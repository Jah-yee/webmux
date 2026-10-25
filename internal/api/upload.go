package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"time"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
)

const (
	maxUploadBytes     int64 = 100 << 20
	maxUploadBodyBytes       = maxUploadBytes + 1<<20
	uploadReadTimeout        = 5 * time.Minute
)

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	item := s.find(w, r)
	if item == nil {
		return
	}
	if item.Info().Status == session.StatusExited {
		failure(w, http.StatusConflict, errors.New("cannot upload to an exited session"))
		return
	}

	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(time.Now().Add(uploadReadTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		failure(w, http.StatusInternalServerError, fmt.Errorf("set upload read deadline: %w", err))
		return
	}
	// Interrupt a blocked network read on cancellation, and restore the deadline
	// before returning so this route's longer deadline cannot affect other routes.
	cancelled := make(chan struct{})
	stop := context.AfterFunc(r.Context(), func() {
		_ = controller.SetReadDeadline(time.Now())
		close(cancelled)
	})
	defer func() {
		if !stop() {
			<-cancelled
		}
		_ = controller.SetReadDeadline(time.Time{})
	}()

	if r.ContentLength > maxUploadBodyBytes {
		failure(w, http.StatusRequestEntityTooLarge, errors.New("upload request exceeds 101 MiB"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBodyBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		failure(w, http.StatusBadRequest, fmt.Errorf("read upload multipart body: %w", err))
		return
	}
	part, err := reader.NextRawPart()
	if err != nil {
		uploadReadFailure(w, "expected exactly one file part named file", err)
		return
	}
	disposition, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	if err != nil || disposition != "form-data" || params["name"] != "file" || params["filename"] == "" {
		failure(w, http.StatusBadRequest, errors.New("expected exactly one file part named file with a filename"))
		return
	}
	upload, err := s.Files.StoreUpload(params["filename"], uploadContextReader{r.Context(), part}, maxUploadBytes)
	if err != nil {
		filesFailure(w, err)
		return
	}
	complete := false
	defer func() {
		if !complete {
			upload.Discard()
		}
	}()
	if _, err := reader.NextRawPart(); err != io.EOF {
		if err == nil {
			failure(w, http.StatusBadRequest, errors.New("expected exactly one file part named file; extra multipart part received"))
		} else {
			uploadReadFailure(w, "finish upload multipart body", err)
		}
		return
	}
	// Multipart permits an epilogue. Drain it through the bounded body reader so
	// even chunked requests cannot bypass the total request size limit.
	if _, err := io.Copy(io.Discard, uploadContextReader{r.Context(), r.Body}); err != nil {
		uploadReadFailure(w, "finish upload request", err)
		return
	}
	if err := r.Context().Err(); err != nil {
		uploadReadFailure(w, "upload cancelled", err)
		return
	}
	if item.Info().Status == session.StatusExited {
		failure(w, http.StatusConflict, errors.New("session exited during upload"))
		return
	}
	complete = true
	reply(w, http.StatusCreated, contract.UploadResult{Path: upload.Path})
}

type uploadContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r uploadContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if cancelled := r.ctx.Err(); cancelled != nil {
		return n, cancelled
	}
	return n, err
}

func uploadReadFailure(w http.ResponseWriter, operation string, err error) {
	failure(w, uploadReadStatus(err), fmt.Errorf("%s: %w", operation, err))
}

// uploadReadStatus classifies a failed request body read: the body limit is
// 413, a read deadline 408, anything else a malformed request.
func uploadReadStatus(err error) int {
	var tooLarge *http.MaxBytesError
	var networkError net.Error
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge
	} else if errors.As(err, &networkError) && networkError.Timeout() {
		return http.StatusRequestTimeout
	}
	return http.StatusBadRequest
}
