package tunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var payload = []byte("#!/bin/sh\necho fake cloudflared\n")

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func noPath(string) (string, error) { return "", errors.New("not found") }

// source is a fake download server recording request paths.
type source struct {
	*httptest.Server
	hits  atomic.Int32
	paths chan string
}

func newSource(t *testing.T, handler func(http.ResponseWriter, *http.Request)) *source {
	t.Helper()
	s := &source{paths: make(chan string, 8)}
	// A bare handler, not ServeMux, so "/https://..." paths are not cleaned.
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.paths <- r.URL.Path
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func serve(body []byte) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(body) }
}

func status(code int) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }
}

func hang(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }

func stallAfter(prefix []byte) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write(prefix)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
}

func testLocator(t *testing.T, github string, mirrors ...string) Locator {
	t.Helper()
	if mirrors == nil {
		mirrors = []string{}
	}
	return Locator{
		StateDir:      t.TempDir(),
		Mirrors:       mirrors,
		lookPath:      noPath,
		platform:      "test/bare",
		assets:        map[string]asset{"test/bare": {name: "cloudflared-test", sha256: digest(payload), binarySHA256: digest(payload)}},
		githubBase:    github,
		headerTimeout: 200 * time.Millisecond,
		idleTimeout:   200 * time.Millisecond,
	}
}

func cachePath(l Locator) string {
	return filepath.Join(l.StateDir, "cloudflared", Version, "cloudflared")
}

func assertInstalled(t *testing.T, l Locator, path string) {
	t.Helper()
	if path != cachePath(l) {
		t.Fatalf("path %q, want cache %q", path, cachePath(l))
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("installed content %q %v", data, err)
	}
	if info, _ := os.Stat(path); info.Mode()&0o111 == 0 {
		t.Fatalf("installed mode %v", info.Mode())
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestFindPrefersExplicitPathWithoutFallback(t *testing.T) {
	dir := t.TempDir()
	explicit := filepath.Join(dir, "cf")
	if err := os.WriteFile(explicit, payload, 0o755); err != nil {
		t.Fatal(err)
	}
	gh := newSource(t, serve(payload))
	l := testLocator(t, gh.URL)
	l.lookPath = func(string) (string, error) { return "/usr/bin/cloudflared", nil }
	l.Explicit = explicit
	if path, err := l.Find(context.Background()); err != nil || path != explicit {
		t.Fatalf("explicit: %q %v", path, err)
	}
	for _, bad := range []string{filepath.Join(dir, "missing"), dir} {
		l.Explicit = bad
		if _, err := l.Find(context.Background()); err == nil {
			t.Fatalf("invalid explicit path %q accepted", bad)
		}
	}
	plain := filepath.Join(dir, "plain")
	_ = os.WriteFile(plain, payload, 0o644)
	l.Explicit = plain
	if _, err := l.Find(context.Background()); err == nil {
		t.Fatal("non-executable explicit path accepted")
	}
	if gh.hits.Load() != 0 {
		t.Fatal("explicit path fell back to download")
	}
}

func TestFindUsesPathWithoutNetwork(t *testing.T) {
	gh := newSource(t, serve(payload))
	l := testLocator(t, gh.URL)
	l.lookPath = func(name string) (string, error) { return "/opt/bin/" + name, nil }
	if path, err := l.Find(context.Background()); err != nil || path != "/opt/bin/cloudflared" {
		t.Fatalf("PATH: %q %v", path, err)
	}
	if gh.hits.Load() != 0 {
		t.Fatal("PATH hit downloaded")
	}
}

func TestFindDownloadsVerifiesAndReusesCache(t *testing.T) {
	gh := newSource(t, serve(payload))
	l := testLocator(t, gh.URL)
	path, err := l.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertInstalled(t, l, path)
	if got := <-gh.paths; got != "/cloudflare/cloudflared/releases/download/"+Version+"/cloudflared-test" {
		t.Fatalf("GitHub path %q", got)
	}
	if _, err := l.Find(context.Background()); err != nil || gh.hits.Load() != 1 {
		t.Fatalf("cache reuse: hits=%d err=%v", gh.hits.Load(), err)
	}
	// A tampered cache is never executed; it is replaced by a verified download.
	if err := os.WriteFile(path, []byte("tampered"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Find(context.Background()); err != nil || gh.hits.Load() != 2 {
		t.Fatalf("tampered cache: hits=%d err=%v", gh.hits.Load(), err)
	}
	assertInstalled(t, l, path)
}

func TestFindRejectsBadDownloads(t *testing.T) {
	for _, row := range []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		limit   int64
		want    string
	}{
		{"digest mismatch", serve([]byte("evil")), 0, "sha256 mismatch"},
		{"oversized", serve(payload), int64(len(payload) - 1), "exceeds"},
		{"not found", status(http.StatusNotFound), 0, "404"},
	} {
		t.Run(row.name, func(t *testing.T) {
			gh := newSource(t, row.handler)
			l := testLocator(t, gh.URL)
			l.maxBytes = row.limit
			_, err := l.Find(context.Background())
			if err == nil || !strings.Contains(err.Error(), row.want) || !strings.Contains(err.Error(), "tunnel.cloudflared") {
				t.Fatalf("error %v, want %q with install hint", err, row.want)
			}
			entries, _ := os.ReadDir(filepath.Dir(cachePath(l)))
			if len(entries) != 0 {
				t.Fatalf("rejected download left files: %v", entries)
			}
		})
	}
}

func TestFindUnsupportedPlatform(t *testing.T) {
	l := testLocator(t, "http://127.0.0.1:1")
	l.platform = "plan9/mips"
	if _, err := l.Find(context.Background()); err == nil || !strings.Contains(err.Error(), "install cloudflared") {
		t.Fatalf("unsupported platform: %v", err)
	}
}

func TestDownloadFallsBackAcrossSources(t *testing.T) {
	mirrorPath := "/https://github.com/cloudflare/cloudflared/releases/download/" + Version + "/cloudflared-test"
	t.Run("GitHub hangs before headers", func(t *testing.T) {
		gh := newSource(t, hang)
		mirror := newSource(t, serve(payload))
		l := testLocator(t, gh.URL, mirror.URL)
		path, err := l.Find(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		assertInstalled(t, l, path)
		if got := <-mirror.paths; got != mirrorPath {
			t.Fatalf("mirror path %q, want %q", got, mirrorPath)
		}
	})
	t.Run("GitHub stalls mid-body", func(t *testing.T) {
		gh := newSource(t, stallAfter(payload[:4]))
		mirror := newSource(t, serve(payload))
		l := testLocator(t, gh.URL, mirror.URL)
		path, err := l.Find(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		assertInstalled(t, l, path)
	})
	t.Run("mirror 404 then success", func(t *testing.T) {
		gh := newSource(t, status(http.StatusBadGateway))
		first := newSource(t, status(http.StatusNotFound))
		second := newSource(t, serve(payload))
		l := testLocator(t, gh.URL, first.URL, second.URL)
		path, err := l.Find(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		assertInstalled(t, l, path)
		if first.hits.Load() != 1 || second.hits.Load() != 1 {
			t.Fatalf("hits first=%d second=%d", first.hits.Load(), second.hits.Load())
		}
	})
	t.Run("tampered mirror then success", func(t *testing.T) {
		gh := newSource(t, status(http.StatusBadGateway))
		first := newSource(t, serve([]byte("evil")))
		second := newSource(t, serve(payload))
		l := testLocator(t, gh.URL, first.URL, second.URL)
		path, err := l.Find(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		assertInstalled(t, l, path)
	})
	t.Run("GitHub success skips mirrors", func(t *testing.T) {
		gh := newSource(t, serve(payload))
		mirror := newSource(t, serve(payload))
		l := testLocator(t, gh.URL, mirror.URL)
		if _, err := l.Find(context.Background()); err != nil {
			t.Fatal(err)
		}
		if mirror.hits.Load() != 0 {
			t.Fatalf("mirror contacted %d times", mirror.hits.Load())
		}
	})
	t.Run("mirrors disabled", func(t *testing.T) {
		gh := newSource(t, status(http.StatusBadGateway))
		l := testLocator(t, gh.URL)
		if _, err := l.Find(context.Background()); err == nil {
			t.Fatal("GitHub failure without mirrors succeeded")
		}
	})
	t.Run("all fail lists every source", func(t *testing.T) {
		gh := newSource(t, hang)
		first := newSource(t, stallAfter(payload[:4]))
		second := newSource(t, status(http.StatusNotFound))
		l := testLocator(t, gh.URL, first.URL, second.URL)
		_, err := l.Find(context.Background())
		if err == nil {
			t.Fatal("expected failure")
		}
		msg := err.Error()
		for _, want := range []string{
			strings.TrimPrefix(gh.URL, "http://") + ": no response headers",
			strings.TrimPrefix(first.URL, "http://") + ": no data",
			strings.TrimPrefix(second.URL, "http://") + ": HTTP 404",
			"tunnel.cloudflared",
		} {
			if !strings.Contains(msg, want) {
				t.Fatalf("error %q missing %q", msg, want)
			}
		}
	})
	t.Run("cancellation returns promptly", func(t *testing.T) {
		gh := newSource(t, hang)
		mirror := newSource(t, serve(payload))
		l := testLocator(t, gh.URL, mirror.URL)
		l.headerTimeout = time.Minute
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		start := time.Now()
		if _, err := l.Find(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("error %v, want context.Canceled", err)
		}
		if time.Since(start) > 5*time.Second || mirror.hits.Load() != 0 {
			t.Fatalf("cancellation took %s, mirror hits %d", time.Since(start), mirror.hits.Load())
		}
	})
}

func tgz(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "README", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte("hi"))
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write(body)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDownloadExtractsArchive(t *testing.T) {
	archive := tgz(t, "cloudflared", payload)
	gh := newSource(t, serve(archive))
	l := testLocator(t, gh.URL)
	l.assets = map[string]asset{"test/bare": {name: "cloudflared-test.tgz", sha256: digest(archive), binarySHA256: digest(payload), archive: true}}
	path, err := l.Find(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertInstalled(t, l, path)
	// Cache verification uses the executable digest, not the archive digest.
	if _, err := l.Find(context.Background()); err != nil || gh.hits.Load() != 1 {
		t.Fatalf("archive cache reuse: hits=%d err=%v", gh.hits.Load(), err)
	}
}
