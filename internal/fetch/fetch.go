// Package fetch downloads pinned GitHub release assets with mirror fallback.
// Every source must produce the pinned SHA-256, so mirrors affect only
// availability, never content. It depends only on the standard library and
// builds on every client platform.
package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// GitHub is the canonical source; mirrors receive "<prefix>/" + GitHub + path.
	GitHub = "https://github.com"

	defaultMaxBytes      = 100 << 20
	defaultHeaderTimeout = 10 * time.Second
	defaultIdleTimeout   = 20 * time.Second
)

// DefaultMirrors are gh-proxy style prefixes tried, in order, after a direct
// GitHub download fails. Each receives "<prefix>/https://github.com/...".
// Both served a pinned cloudflared release intact from mainland China at
// 1.8-2.5 MB/s on 2026-10-20; ghproxy.net managed only ~10 KB/s and was dropped.
var DefaultMirrors = []string{"https://ghfast.top", "https://gh-proxy.com"}

// Asset is one pinned release download.
type Asset struct {
	// Path is the GitHub path, e.g. "/owner/repo/releases/download/v1/name".
	Path string
	// SHA256 is the hex digest of the downloaded file.
	SHA256 string
	// Extract optionally turns the verified download into the installed file,
	// returning a temporary path in dir; BinarySHA256 then verifies it.
	Extract      func(download *os.File, dir string, limit int64) (string, error)
	BinarySHA256 string
}

// Downloader holds the sources and limits; zero values select defaults.
type Downloader struct {
	// Name labels log lines, e.g. "cloudflared".
	Name string
	// Mirrors nil selects DefaultMirrors; empty means GitHub only.
	Mirrors       []string
	GitHubBase    string
	Client        *http.Client
	HeaderTimeout time.Duration
	IdleTimeout   time.Duration
	MaxBytes      int64
	Logger        *slog.Logger
}

// SourcesError reports that every source failed.
type SourcesError struct {
	Failures []string // "<host>: <reason>"
}

func (e *SourcesError) Error() string {
	return "failed from every source (" + strings.Join(e.Failures, "; ") + ")"
}

// Download installs a verified executable (0755) at target, trying GitHub,
// then each mirror. It returns the host that served it. A context error is
// returned unwrapped; target is untouched unless a source succeeds.
func (d Downloader) Download(ctx context.Context, a Asset, target string) (string, error) {
	base := d.GitHubBase
	if base == "" {
		base = GitHub
	}
	sources := []string{base + a.Path}
	mirrors := d.Mirrors
	if mirrors == nil {
		mirrors = DefaultMirrors
	}
	for _, mirror := range mirrors {
		sources = append(sources, mirror+"/"+GitHub+a.Path)
	}
	var failures []string
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		host := sourceHost(source)
		err := d.fetch(ctx, source, a, target)
		if err == nil {
			return host, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		d.logger().Warn(d.Name+" download source failed", "source", host, "error", err)
		failures = append(failures, host+": "+err.Error())
	}
	return "", &SourcesError{Failures: failures}
}

func (d Downloader) logger() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}
	return slog.Default()
}

// FileSHA256 returns the hex SHA-256 of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sourceHost(source string) string {
	if u, err := url.Parse(source); err == nil {
		return u.Host
	}
	return source
}

// fetch downloads one source into a temporary file beside target, verifies
// it, and atomically installs the executable.
func (d Downloader) fetch(parent context.Context, source string, a Asset, target string) (err error) {
	headerTimeout, idleTimeout, limit := d.HeaderTimeout, d.IdleTimeout, d.MaxBytes
	if headerTimeout == 0 {
		headerTimeout = defaultHeaderTimeout
	}
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}
	if limit == 0 {
		limit = defaultMaxBytes
	}
	client := d.Client
	if client == nil {
		client = http.DefaultClient
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	// One watchdog covers connect+headers, then each idle gap in the body.
	// phase names the current wait; timedOut records it only if the watchdog
	// actually fired, so unrelated failures keep their own error.
	var phase, timedOut atomic.Pointer[string]
	setPhase := func(s string) { phase.Store(&s) }
	setPhase(fmt.Sprintf("no response headers within %s", headerTimeout))
	watchdog := time.AfterFunc(headerTimeout, func() {
		timedOut.Store(phase.Load())
		cancel()
	})
	defer watchdog.Stop()
	defer func() {
		if reason := timedOut.Load(); err != nil && reason != nil && parent.Err() == nil {
			err = errors.New(*reason)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return fmt.Errorf("HTTP %s", res.Status)
	}
	if !watchdog.Stop() {
		return ctx.Err()
	}
	setPhase(fmt.Sprintf("no data for %s", idleTimeout))
	watchdog.Reset(idleTimeout)

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return err
	}
	// After a successful rename these are no-ops; otherwise they discard partial data.
	defer func() { _ = os.Remove(tmp.Name()) }()
	defer func() { _ = tmp.Close() }()
	h := sha256.New()
	body := &idleReader{r: io.LimitReader(res.Body, limit+1), watchdog: watchdog, idle: idleTimeout}
	n, err := io.Copy(io.MultiWriter(tmp, h), body)
	if err != nil {
		return err
	}
	watchdog.Stop()
	if n > limit {
		return fmt.Errorf("response exceeds %d bytes", limit)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return fmt.Errorf("sha256 mismatch: got %s", got)
	}
	binary := tmp.Name()
	if a.Extract != nil {
		extracted, err := a.Extract(tmp, dir, limit)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(extracted) }()
		if sum, err := FileSHA256(extracted); err != nil {
			return err
		} else if sum != a.BinarySHA256 {
			return fmt.Errorf("extracted %s sha256 mismatch: got %s", d.Name, sum)
		}
		binary = extracted
	}
	// Windows cannot rename an open file over another.
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(binary, 0o755); err != nil {
		return err
	}
	return os.Rename(binary, target)
}

// idleReader resets the watchdog whenever data arrives.
type idleReader struct {
	r        io.Reader
	watchdog *time.Timer
	idle     time.Duration
}

func (r *idleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.watchdog.Reset(r.idle)
	}
	return n, err
}
