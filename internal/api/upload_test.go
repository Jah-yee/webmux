package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
)

func uploadAPI(t *testing.T) (http.Handler, *session.Manager, *session.Session, string) {
	t.Helper()
	root := t.TempDir()
	cwd := t.TempDir()
	m := session.NewManager(session.Options{DefaultCwd: cwd})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	item, err := m.Create(cwd, "custom", []string{"/bin/sh"}, nil, "upload", nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(testDeps(t, Deps{Config: config.Config{Token: "secret"}, Manager: m, Workspace: testWorkspace(t, m, cwd)}))
	t.Setenv("TMPDIR", root)
	return handler, m, item, root
}

func uploadBody(t *testing.T, name string, data []byte, extra string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name}))
	header.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if extra != "" {
		if err := writer.WriteField(extra, "extra"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func uploadRequest(item *session.Session, body io.Reader, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://webmux.example/api/sessions/"+item.Info().ID+"/upload", body)
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("Content-Type", contentType)
	return r
}

func requireUploadError(t *testing.T, w *httptest.ResponseRecorder, status int, root string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("status %d, want %d: %s", w.Code, status, w.Body.String())
	}
	var result struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Error == "" {
		t.Fatalf("missing contextual JSON error: %s (%v)", w.Body.String(), err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed upload left temporary files: %v (%v)", entries, err)
	}
}

func TestUploadPersistsBinaryInPrivateUniqueTemporaryDirectories(t *testing.T) {
	handler, _, item, root := uploadAPI(t)
	first := []byte{0, 1, 2, 255, '\n', '\r', 128}
	second := []byte("second upload")
	var paths []string
	for _, data := range [][]byte{first, second} {
		body, contentType := uploadBody(t, "same name.bin", data, "")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, uploadRequest(item, bytes.NewReader(body), contentType))
		if w.Code != http.StatusCreated {
			t.Fatalf("upload: %d %s", w.Code, w.Body.String())
		}
		var result struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		path := result.Path
		if !filepath.IsAbs(path) || filepath.Base(path) != "same name.bin" || filepath.Dir(filepath.Dir(path)) != root || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "webmux-upload-") {
			t.Fatalf("unexpected temporary path %q", path)
		}
		stored, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(stored, data) {
			t.Fatalf("persisted bytes %v, want %v (%v)", stored, data, err)
		}
		for target, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700} {
			info, err := os.Stat(target)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatalf("permissions for %s: %v (%v), want %o", target, info, err, mode)
			}
		}
		paths = append(paths, path)
	}
	if paths[0] == paths[1] || filepath.Dir(paths[0]) == filepath.Dir(paths[1]) {
		t.Fatal("same filenames reused an upload directory")
	}
	stored, err := os.ReadFile(paths[0])
	if err != nil || !bytes.Equal(stored, first) {
		t.Fatalf("second upload overwrote first: %v (%v)", stored, err)
	}
}

func TestUploadNormalizesTraversalFilenames(t *testing.T) {
	for _, name := range []string{"../../escape.bin", "/absolute/path/escape.bin", `..\..\escape.bin`, `C:\folder\escape.bin`} {
		t.Run(name, func(t *testing.T) {
			handler, _, item, root := uploadAPI(t)
			body, contentType := uploadBody(t, name, []byte("safe"), "")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, uploadRequest(item, bytes.NewReader(body), contentType))
			if w.Code != http.StatusCreated {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			var result map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if filepath.Base(result["path"]) != "escape.bin" || filepath.Dir(filepath.Dir(result["path"])) != root {
				t.Fatalf("filename escaped temporary directory: %q", result["path"])
			}
		})
	}
}

func TestUploadRejectsUnsafeFilenames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "/", `..\`, "bad\x00name", "bad\rname", "bad\nname", "bad\tname", "bad\x1bname", "bad\x7fname", "bad\u0085name", "unsafe\nparent/safe.bin"} {
		t.Run(fmt.Sprintf("%q", name), func(t *testing.T) {
			handler, _, item, root := uploadAPI(t)
			body, contentType := uploadBody(t, name, []byte("data"), "")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, uploadRequest(item, bytes.NewReader(body), contentType))
			requireUploadError(t, w, http.StatusBadRequest, root)
		})
	}
}

func TestUploadRejectsMalformedAndExtraMultipartParts(t *testing.T) {
	for _, name := range []string{"not multipart", "missing boundary", "empty multipart", "field instead of file", "wrong field", "extra field", "extra file", "truncated file", "truncated final boundary", "malformed extra part"} {
		t.Run(name, func(t *testing.T) {
			handler, _, item, root := uploadAPI(t)
			body, contentType := uploadBody(t, "file.bin", []byte("data"), "")
			switch name {
			case "not multipart":
				contentType = "application/octet-stream"
			case "missing boundary":
				contentType = "multipart/form-data"
			case "empty multipart":
				body = []byte("--empty--\r\n")
				contentType = "multipart/form-data; boundary=empty"
			case "field instead of file":
				body = []byte("--field\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\ndata\r\n--field--\r\n")
				contentType = "multipart/form-data; boundary=field"
			case "wrong field":
				body = bytes.Replace(body, []byte(`; name=file`), []byte(`; name=other`), 1)
			case "extra field":
				body, contentType = uploadBody(t, "file.bin", []byte("data"), "extra")
			case "extra file":
				body, contentType = uploadBody(t, "file.bin", []byte("data"), "extra")
				body = bytes.Replace(body, []byte(`name="extra"`), []byte(`name="file"; filename="second.bin"`), 1)
			case "truncated file":
				body = body[:bytes.Index(body, []byte("\r\n\r\ndata"))+len("\r\n\r\nda")]
			case "truncated final boundary":
				body = body[:len(body)-8]
			case "malformed extra part":
				body, contentType = uploadBody(t, "file.bin", []byte("data"), "extra")
				body = bytes.Replace(body, []byte("Content-Disposition: form-data; name=\"extra\""), []byte("not-a-valid-header"), 1)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, uploadRequest(item, bytes.NewReader(body), contentType))
			requireUploadError(t, w, http.StatusBadRequest, root)
		})
	}
}

// Generate arbitrarily large request bodies without allocating their contents.
// A nonzero byte also ensures no multipart boundary is accidentally generated.
type uploadGeneratedReader struct{}

func (uploadGeneratedReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestUploadSizeLimitsRemovePartialFiles(t *testing.T) {
	for _, name := range []string{"oversized file", "oversized declared body", "oversized chunked epilogue"} {
		t.Run(name, func(t *testing.T) {
			handler, _, item, root := uploadAPI(t)
			const boundary = "upload-size-boundary"
			header := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"; filename=\"large.bin\"\r\n\r\n"
			ending := "\r\n--" + boundary + "--\r\n"
			var body io.Reader
			if name == "oversized chunked epilogue" {
				body = io.MultiReader(strings.NewReader(header+"small"+ending), io.LimitReader(uploadGeneratedReader{}, maxUploadBodyBytes))
			} else {
				body = io.MultiReader(strings.NewReader(header), io.LimitReader(uploadGeneratedReader{}, maxUploadBytes+1), strings.NewReader(ending))
			}
			r := uploadRequest(item, body, "multipart/form-data; boundary="+boundary)
			if name == "oversized declared body" {
				r.ContentLength = maxUploadBodyBytes + 1
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			requireUploadError(t, w, http.StatusRequestEntityTooLarge, root)
		})
	}
}

func TestUploadRequiresLiveSessionAndExistingSecurityMiddleware(t *testing.T) {
	for _, row := range []struct {
		name   string
		status int
	}{
		{"missing session", http.StatusNotFound},
		{"exited session", http.StatusConflict},
		{"missing authentication", http.StatusUnauthorized},
		{"wrong authentication", http.StatusUnauthorized},
		{"query authentication", http.StatusUnauthorized},
		{"untrusted origin", http.StatusForbidden},
	} {
		t.Run(row.name, func(t *testing.T) {
			handler, manager, item, root := uploadAPI(t)
			body, contentType := uploadBody(t, "file.bin", []byte("data"), "")
			r := uploadRequest(item, bytes.NewReader(body), contentType)
			switch row.name {
			case "missing session":
				r.URL.Path = "/api/sessions/missing/upload"
			case "exited session":
				if err := manager.Close(item.Info().ID); err != nil {
					t.Fatal(err)
				}
				select {
				case <-item.Done():
				case <-time.After(7 * time.Second):
					t.Fatal("session did not exit")
				}
			case "missing authentication":
				r.Header.Del("Authorization")
			case "wrong authentication":
				r.Header.Set("Authorization", "Bearer wrong")
			case "query authentication":
				r.Header.Del("Authorization")
				r.URL.RawQuery = "token=secret"
			case "untrusted origin":
				r.Header.Set("Origin", "https://evil.example")
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			requireUploadError(t, w, row.status, root)
		})
	}
}

type uploadCancellingReader struct {
	reader io.Reader
	cancel context.CancelFunc
	read   int
}

func (r *uploadCancellingReader) Read(p []byte) (int, error) {
	if len(p) > 1024 {
		p = p[:1024]
	}
	n, err := r.reader.Read(p)
	r.read += n
	if r.read >= 8192 {
		r.cancel()
	}
	return n, err
}

func TestUploadCancellationRemovesPartialFile(t *testing.T) {
	handler, _, item, root := uploadAPI(t)
	body, contentType := uploadBody(t, "cancelled.bin", bytes.Repeat([]byte("x"), 32<<10), "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &uploadCancellingReader{reader: bytes.NewReader(body), cancel: cancel}
	r := uploadRequest(item, reader, contentType).WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	requireUploadError(t, w, http.StatusBadRequest, root)
	if !strings.Contains(w.Body.String(), "context canceled") {
		t.Fatalf("missing cancellation context: %s", w.Body.String())
	}
}
