package tunnel

import (
	"archive/tar"
	"compress/gzip"
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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

const (
	maxDownloadBytes     = 100 << 20
	defaultHeaderTimeout = 10 * time.Second
	defaultIdleTimeout   = 20 * time.Second
	githubBase           = "https://github.com"
)

// Locator resolves the cloudflared executable: explicit path, PATH, verified
// cache under StateDir, then a pinned download with mirror fallback.
type Locator struct {
	Explicit string   // tunnel.cloudflared; never falls back when set
	StateDir string   // cache root: <StateDir>/cloudflared/<Version>/cloudflared
	Mirrors  []string // nil selects DefaultMirrors; empty means GitHub only
	Logger   *slog.Logger

	// Test seams; zero values select production behavior.
	lookPath      func(string) (string, error)
	platform      string
	assets        map[string]asset
	githubBase    string
	client        *http.Client
	headerTimeout time.Duration
	idleTimeout   time.Duration
	maxBytes      int64
}

// Find returns an executable cloudflared path, downloading it if needed.
func (l Locator) Find(ctx context.Context) (string, error) {
	if l.Explicit != "" {
		if err := executable(l.Explicit); err != nil {
			return "", fmt.Errorf("tunnel.cloudflared: %w", err)
		}
		return l.Explicit, nil
	}
	lookPath := l.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if path, err := lookPath("cloudflared"); err == nil {
		return path, nil
	}
	platform := l.platform
	if platform == "" {
		platform = runtime.GOOS + "/" + runtime.GOARCH
	}
	table := l.assets
	if table == nil {
		table = assets
	}
	a, ok := table[platform]
	if !ok {
		return "", fmt.Errorf("no cloudflared download for %s; install cloudflared on PATH or set tunnel.cloudflared", platform)
	}
	if l.StateDir == "" {
		return "", errors.New("cloudflared download needs state_dir")
	}
	dir := filepath.Join(l.StateDir, "cloudflared", Version)
	target := filepath.Join(dir, "cloudflared")
	if sum, err := fileSHA256(target); err == nil {
		if sum == a.binarySHA256 {
			return target, nil
		}
		l.logger().Warn("cached cloudflared failed verification; downloading again", "path", target)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cloudflared cache: %w", err)
	}
	if err := l.download(ctx, a, target); err != nil {
		return "", err
	}
	return target, nil
}

func (l Locator) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}

func executable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not an executable file", path)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
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

// download tries GitHub, then each mirror, keeping the first artifact whose
// digest matches. Mirrors therefore affect availability, never content.
func (l Locator) download(ctx context.Context, a asset, target string) error {
	path := "/cloudflare/cloudflared/releases/download/" + Version + "/" + a.name
	base := l.githubBase
	if base == "" {
		base = githubBase
	}
	sources := []string{base + path}
	mirrors := l.Mirrors
	if mirrors == nil {
		mirrors = DefaultMirrors
	}
	for _, mirror := range mirrors {
		sources = append(sources, mirror+"/"+githubBase+path)
	}
	var failures []string
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		host := sourceHost(source)
		err := l.fetch(ctx, source, a, target)
		if err == nil {
			l.logger().Info("downloaded cloudflared", "version", Version, "source", host)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		l.logger().Warn("cloudflared download source failed", "source", host, "error", err)
		failures = append(failures, host+": "+err.Error())
	}
	return fmt.Errorf("download cloudflared %s failed from every source (%s); install cloudflared on PATH or set tunnel.cloudflared", Version, strings.Join(failures, "; "))
}

func sourceHost(source string) string {
	if u, err := url.Parse(source); err == nil {
		return u.Host
	}
	return source
}

// fetch downloads one source into a temporary file beside target, verifies
// it, and atomically installs the executable.
func (l Locator) fetch(parent context.Context, source string, a asset, target string) (err error) {
	headerTimeout, idleTimeout, limit := l.headerTimeout, l.idleTimeout, l.maxBytes
	if headerTimeout == 0 {
		headerTimeout = defaultHeaderTimeout
	}
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}
	if limit == 0 {
		limit = maxDownloadBytes
	}
	client := l.client
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
	if got := hex.EncodeToString(h.Sum(nil)); got != a.sha256 {
		return fmt.Errorf("sha256 mismatch: got %s", got)
	}
	binary := tmp.Name()
	if a.archive {
		extracted, err := extract(tmp, dir, limit)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(extracted) }()
		if sum, err := fileSHA256(extracted); err != nil {
			return err
		} else if sum != a.binarySHA256 {
			return fmt.Errorf("extracted cloudflared sha256 mismatch: got %s", sum)
		}
		binary = extracted
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

// extract copies the top-level "cloudflared" entry of a verified .tgz into a
// temporary file in dir.
func extract(archive *os.File, dir string, limit int64) (string, error) {
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return "", err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return "", errors.New("archive has no cloudflared executable")
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag != tar.TypeReg || strings.TrimPrefix(header.Name, "./") != "cloudflared" {
			continue
		}
		out, err := os.CreateTemp(dir, ".extract-*")
		if err != nil {
			return "", err
		}
		n, err := io.Copy(out, io.LimitReader(tr, limit+1))
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err == nil && n > limit {
			err = fmt.Errorf("archive entry exceeds %d bytes", limit)
		}
		if err != nil {
			_ = os.Remove(out.Name())
			return "", err
		}
		return out.Name(), nil
	}
}
