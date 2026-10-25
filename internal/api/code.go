package api

import (
	"context"
	"errors"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/wire"
	"mime"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"
)

// serveCode answers one code view JSON route within the 10 s bound. Each
// route returns its concrete contract type; archtest rule R8 checks T at
// every call.
func serveCode[T any](mux *http.ServeMux, pattern string, fn func(context.Context, *http.Request) (T, error)) {
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		value, err := fn(ctx, r)
		if err != nil {
			failure(w, codeStatus(err), err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		reply(w, 200, value)
	})
}

func (s *Server) codeRoutes(mux *http.ServeMux) {
	base := " /api/sessions/{id}/code"
	serveCode(mux, "GET"+base+"/context", func(ctx context.Context, r *http.Request) (contract.CodeContext, error) {
		v, err := s.Code.Context(ctx, r.PathValue("id"))
		return wire.CodeContext(v), err
	})
	serveCode(mux, "GET"+base+"/tree", func(ctx context.Context, r *http.Request) (contract.CodeListing, error) {
		q := r.URL.Query()
		v, err := s.Code.Tree(ctx, r.PathValue("id"), q.Get("contextId"), q.Get("nodeId"), q.Get("includeIgnored") == "true", q.Get("cursor"))
		return wire.CodeListing(v), err
	})
	serveCode(mux, "GET"+base+"/files", func(ctx context.Context, r *http.Request) (contract.CodeListing, error) {
		q := r.URL.Query()
		v, err := s.Code.Search(ctx, r.PathValue("id"), q.Get("contextId"), q.Get("q"), q.Get("includeIgnored") == "true")
		return wire.CodeListing(v), err
	})
	serveCode(mux, "GET"+base+"/locate", func(ctx context.Context, r *http.Request) (contract.CodeLocation, error) {
		q := r.URL.Query()
		v, err := s.Code.Locate(ctx, r.PathValue("id"), q.Get("contextId"), q.Get("path"))
		return wire.CodeLocation(v), err
	})
	serveCode(mux, "GET"+base+"/file", func(ctx context.Context, r *http.Request) (contract.CodeFile, error) {
		q := r.URL.Query()
		source := q.Get("source")
		if source == "" {
			source = "worktree"
		}
		v, err := s.Code.FileSource(ctx, r.PathValue("id"), q.Get("contextId"), q.Get("nodeId"), source)
		return wire.CodeFile(v), err
	})
	serveCode(mux, "GET"+base+"/changes", func(ctx context.Context, r *http.Request) (contract.CodeChanges, error) {
		q := r.URL.Query()
		v, err := s.Code.Changes(ctx, r.PathValue("id"), q.Get("contextId"), codeview.ChangeQuery{Cursor: q.Get("cursor"), Q: q.Get("q"), NodeID: q.Get("nodeId"), Group: q.Get("group")})
		return wire.CodeChanges(v), err
	})
	serveCode(mux, "GET"+base+"/diff", func(ctx context.Context, r *http.Request) (contract.CodeDiff, error) {
		v, err := s.Code.Diff(ctx, r.PathValue("id"), r.URL.Query().Get("contextId"), r.URL.Query().Get("changeId"))
		return wire.CodeDiff(v), err
	})
	serveCode(mux, "GET"+base+"/snapshots/{snapshotId}/file", func(ctx context.Context, r *http.Request) (contract.CodeFile, error) {
		v, err := s.Code.SnapshotFile(ctx, r.PathValue("id"), r.PathValue("snapshotId"), r.URL.Query().Get("side"))
		return wire.CodeFile(v), err
	})
	// Raw bytes stream outside the 10 s JSON wrapper; GET also serves HEAD.
	mux.HandleFunc("GET"+base+"/snapshots/{snapshotId}/raw", s.codeRaw)
	mux.HandleFunc("POST"+base+"/check", func(w http.ResponseWriter, r *http.Request) {
		var req contract.CodeCheckRequest
		if !decode(w, r, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		v, e := s.Code.Check(ctx, r.PathValue("id"), wire.CodeCheckRequest(req))
		if e != nil {
			failure(w, codeStatus(e), e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		reply(w, 200, wire.CodeCheck(v))
	})
}

// rawChunkDeadline bounds how long one streamed chunk may stall.
const rawChunkDeadline = 30 * time.Second

func (s *Server) codeRaw(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	t, err := s.Code.Raw(ctx, r.PathValue("id"), r.PathValue("snapshotId"), q.Get("side"))
	cancel()
	if err != nil {
		failure(w, codeStatus(err), err)
		return
	}
	defer t.Close()
	h := w.Header()
	contentType, disposition := "application/octet-stream", "attachment"
	if t.MediaType != "" {
		contentType = t.MediaType
		if q.Get("download") != "1" {
			disposition = "inline"
		}
	}
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.FormatInt(t.Size, 10))
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Disposition", contentDisposition(disposition, t.Name))
	if t.MediaType == "image/svg+xml" {
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	out := &deadlineWriter{w: w, rc: http.NewResponseController(w)}
	if err := t.WriteTo(r.Context(), out); err != nil {
		// Headers promised Content-Length; abort so clients see a failure.
		panic(http.ErrAbortHandler)
	}
}

// contentDisposition encodes the source basename (RFC 2231 for non-ASCII) and
// falls back to "download" for control characters or unencodable names.
func contentDisposition(disposition, name string) string {
	safe := utf8.ValidString(name) && name != ""
	for _, c := range name {
		if c < 0x20 || c == 0x7f {
			safe = false
		}
	}
	if safe {
		if v := mime.FormatMediaType(disposition, map[string]string{"filename": name}); v != "" {
			return v
		}
	}
	return mime.FormatMediaType(disposition, map[string]string{"filename": "download"})
}

// deadlineWriter extends the connection write deadline before every chunk.
type deadlineWriter struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func (d *deadlineWriter) Write(p []byte) (int, error) {
	if err := d.rc.SetWriteDeadline(time.Now().Add(rawChunkDeadline)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return 0, err
	}
	return d.w.Write(p)
}
