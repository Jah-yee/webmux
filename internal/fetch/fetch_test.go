package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func serve(t *testing.T, body []byte, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDownloadVerifiesAndInstalls(t *testing.T) {
	body := []byte("verified release asset")
	server := serve(t, body, http.StatusOK)
	target := filepath.Join(t.TempDir(), "webmux")
	d := Downloader{Name: "test", GitHubBase: server.URL, Mirrors: []string{}, Client: server.Client()}
	host, err := d.Download(context.Background(), Asset{Path: "/asset", SHA256: digest(body)}, target)
	if err != nil {
		t.Fatal(err)
	}
	if host != strings.TrimPrefix(server.URL, "http://") {
		t.Fatalf("host = %q", host)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("content = %q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
}

func TestDownloadFallsBackToMirror(t *testing.T) {
	body := []byte("mirror content")
	primary := serve(t, nil, http.StatusInternalServerError)
	mirror := serve(t, body, http.StatusOK)
	target := filepath.Join(t.TempDir(), "webmux")
	d := Downloader{
		Name: "test", GitHubBase: primary.URL, Mirrors: []string{mirror.URL},
		Client: primary.Client(),
	}
	host, err := d.Download(context.Background(), Asset{Path: "/asset", SHA256: digest(body)}, target)
	if err != nil {
		t.Fatal(err)
	}
	if host != strings.TrimPrefix(mirror.URL, "http://") {
		t.Fatalf("host = %q", host)
	}
}

func TestDownloadRejectsChecksumMismatch(t *testing.T) {
	server := serve(t, []byte("tampered"), http.StatusOK)
	target := filepath.Join(t.TempDir(), "webmux")
	d := Downloader{Name: "test", GitHubBase: server.URL, Mirrors: []string{}, Client: server.Client()}
	_, err := d.Download(context.Background(), Asset{Path: "/asset", SHA256: digest([]byte("expected"))}, target)
	var sources *SourcesError
	if !errors.As(err, &sources) {
		t.Fatalf("err = %v", err)
	}
	if len(sources.Failures) != 1 || !strings.Contains(sources.Failures[0], "sha256 mismatch") {
		t.Fatalf("failures = %v", sources.Failures)
	}
	if _, statErr := os.Stat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("target was installed after a failed verification: %v", statErr)
	}
}

func TestDownloadRejectsOversizedResponse(t *testing.T) {
	server := serve(t, []byte("too large"), http.StatusOK)
	target := filepath.Join(t.TempDir(), "webmux")
	d := Downloader{
		Name: "test", GitHubBase: server.URL, Mirrors: []string{}, Client: server.Client(),
		MaxBytes: 3,
	}
	_, err := d.Download(context.Background(), Asset{Path: "/asset", SHA256: digest([]byte("too large"))}, target)
	if err == nil || !strings.Contains(err.Error(), "exceeds 3 bytes") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadExtractsAndVerifiesBinary(t *testing.T) {
	body := []byte("archive")
	binary := []byte("executable")
	var dir string
	d := Downloader{Name: "test", Mirrors: []string{}}
	extract := func(download *os.File, targetDir string, limit int64) (string, error) {
		dir = targetDir
		path := filepath.Join(dir, "extracted")
		return path, os.WriteFile(path, binary, 0o600)
	}
	server := serve(t, body, http.StatusOK)
	d.GitHubBase, d.Client = server.URL, server.Client()
	target := filepath.Join(t.TempDir(), "webmux")
	_, err := d.Download(context.Background(), Asset{
		Path: "/asset", SHA256: digest(body),
		Extract: extract, BinarySHA256: digest(binary),
	}, target)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Fatalf("installed %q", got)
	}
	if dir == "" {
		t.Fatal("extract did not receive the target directory")
	}
}

func TestDownloadStopsOnCanceledContext(t *testing.T) {
	server := serve(t, []byte("body"), http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := Downloader{Name: "test", GitHubBase: server.URL, Mirrors: []string{}, Client: server.Client()}
	_, err := d.Download(ctx, Asset{Path: "/asset", SHA256: digest([]byte("body"))}, filepath.Join(t.TempDir(), "webmux"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	data := []byte("hash me")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != digest(data) {
		t.Fatalf("hash = %q", got)
	}
}
