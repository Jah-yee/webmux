package tunnel

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rainchan/webmux/internal/fetch"
)

// Locator resolves the cloudflared executable: explicit path, PATH, verified
// cache under StateDir, then a pinned download with mirror fallback.
type Locator struct {
	Explicit string   // tunnel.cloudflared; never falls back when set
	StateDir string   // cache root: <StateDir>/cloudflared/<Version>/cloudflared
	Mirrors  []string // nil selects fetch.DefaultMirrors; empty means GitHub only
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
	if sum, err := fetch.FileSHA256(target); err == nil {
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

// download fetches the pinned asset from GitHub or a mirror.
func (l Locator) download(ctx context.Context, a asset, target string) error {
	pinned := fetch.Asset{
		Path:   "/cloudflare/cloudflared/releases/download/" + Version + "/" + a.name,
		SHA256: a.sha256,
	}
	if a.archive {
		pinned.Extract, pinned.BinarySHA256 = extract, a.binarySHA256
	}
	d := fetch.Downloader{
		Name:          "cloudflared",
		Mirrors:       l.Mirrors,
		GitHubBase:    l.githubBase,
		Client:        l.client,
		HeaderTimeout: l.headerTimeout,
		IdleTimeout:   l.idleTimeout,
		MaxBytes:      l.maxBytes,
		Logger:        l.Logger,
	}
	source, err := d.Download(ctx, pinned, target)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return fmt.Errorf("download cloudflared %s %w; install cloudflared on PATH or set tunnel.cloudflared", Version, err)
	}
	l.logger().Info("downloaded cloudflared", "version", Version, "source", source)
	return nil
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
