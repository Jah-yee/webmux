package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/config"
)

func TestTunnelFlagAndGeneratedToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	enabled := filepath.Join(dir, "tunnel.toml")
	if err := os.WriteFile(enabled, []byte("[tunnel]\nenabled = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name      string
		args      []string
		tunnel    bool
		token     string // "generated" means a fresh 43-character token
		generated bool
	}{
		{"default", nil, false, "", false},
		{"flag enables", []string{"--tunnel"}, true, "", true},
		{"flag disables file", []string{"--config", enabled, "--tunnel=false"}, false, "", false},
		{"file enables", []string{"--config", enabled}, true, "", true},
		{"configured token kept", []string{"--tunnel", "--token", tunnelTestToken}, true, tunnelTestToken, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			c, err := loadConfig(row.args)
			if err != nil {
				t.Fatal(err)
			}
			if c.Tunnel.Enabled != row.tunnel {
				t.Fatalf("tunnel enabled %v", c.Tunnel.Enabled)
			}
			if row.generated {
				again, _ := loadConfig(row.args)
				if len(c.Token) != 43 || c.Token == again.Token {
					t.Fatalf("generated tokens %q %q", c.Token, again.Token)
				}
			} else if c.Token != row.token {
				t.Fatalf("token %q, want %q", c.Token, row.token)
			}
		})
	}
}

// tunnelTestToken matches the 43-character width of a generated token.
const tunnelTestToken = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"

func TestTunnelRejectsShortConfiguredToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := loadConfig([]string{"--tunnel", "--token", "mine"})
	if err == nil || !strings.Contains(err.Error(), "at least 43 characters") {
		t.Fatalf("weak tunnel token error %v", err)
	}
}

// fakeTunnel records its PID in $FAKE_DIR/pids, announces a Quick Tunnel,
// then exits with status 1 once $FAKE_DIR/exit appears, or records SIGTERM.
const fakeTunnel = `#!/bin/sh
trap 'echo term > "$FAKE_DIR/term"; exit 0' TERM
echo $$ >> "$FAKE_DIR/pids"
echo 'INF |  https://abc-def.trycloudflare.com  |' >&2
echo 'INF Registered tunnel connection connIndex=0' >&2
while [ ! -e "$FAKE_DIR/exit" ]; do sleep 0.05; done
exit 1
`

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type tunnelRun struct {
	dir, state, local string
	out               *syncBuffer
	cancel            context.CancelFunc
	result            chan error
}

// startTunnelServe starts with --tunnel and waits for the public URL.
func startTunnelServe(t *testing.T) *tunnelRun {
	t.Helper()
	return startServe(t, true, tunnelTestToken)
}

// startServe runs serve in-process with the fake cloudflared configured.
// With startupTunnel it takes the --tunnel path and waits for the public URL.
// The state directory sits under /tmp so the local API socket fits sun_path.
func startServe(t *testing.T, startupTunnel bool, token string) *tunnelRun {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("FAKE_DIR", dir)
	bin := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(bin, []byte(fakeTunnel), 0o755); err != nil {
		t.Fatal(err)
	}
	state, err := os.MkdirTemp("/tmp", "wmx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(state) })
	c, err := config.Load(filepath.Join(dir, "absent.toml"), false)
	if err != nil {
		t.Fatal(err)
	}
	c.Listen, c.Token, c.StateDir = "127.0.0.1:0", token, state
	c.Tunnel = config.TunnelConfig{Enabled: startupTunnel, Cloudflared: bin}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &tunnelRun{dir: dir, state: state, out: &syncBuffer{}, cancel: cancel, result: make(chan error, 1)}
	go func() { r.result <- serve(ctx, serverRun{Config: c}, r.out) }()
	t.Cleanup(func() {
		cancel()
		<-r.result
	})
	pattern := `WebMux local:  (http://127\.0\.0\.1:\d+)\n`
	if startupTunnel {
		eventually(t, "public URL", func() bool { return strings.Contains(r.out.String(), "WebMux public:") })
		pattern = `WebMux local:  (http://127\.0\.0\.1:\d+)/#token=` + regexp.QuoteMeta(token) + `\n`
	} else {
		eventually(t, "local URL", func() bool { return strings.Contains(r.out.String(), "WebMux local:") })
	}
	m := regexp.MustCompile(pattern).FindStringSubmatch(r.out.String())
	if m == nil {
		t.Fatalf("local URL missing: %q", r.out.String())
	}
	r.local = m[1]
	return r
}

const publicHost = "abc-def.trycloudflare.com"

func (r *tunnelRun) status(t *testing.T, host string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, r.local+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	req.Header.Set("Authorization", "Bearer "+tunnelTestToken)
	if host == publicHost {
		req.Header.Set("Origin", "https://"+publicHost)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}

func TestServeAnnouncesTunnelAndRevokesOnExit(t *testing.T) {
	r := startTunnelServe(t)
	out := r.out.String()
	localAt := strings.Index(out, "WebMux local:")
	publicAt := strings.Index(out, "WebMux public: https://"+publicHost+"/#token="+tunnelTestToken+"\n")
	if localAt < 0 || publicAt < localAt {
		t.Fatalf("output order: %q", out)
	}
	if qr := out[publicAt:]; !strings.Contains(qr, "█") || strings.Count(qr, "\n") < 10 {
		t.Fatalf("QR code missing: %q", qr)
	}
	if got := r.status(t, publicHost); got != http.StatusOK {
		t.Fatalf("public request: %d", got)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "exit"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	eventually(t, "public origin revoked", func() bool { return r.status(t, publicHost) == http.StatusForbidden })
	if got := r.status(t, strings.TrimPrefix(r.local, "http://")); got != http.StatusOK {
		t.Fatalf("local request after tunnel exit: %d", got)
	}
}

func TestServeShutdownStopsTunnel(t *testing.T) {
	r := startTunnelServe(t)
	r.cancel()
	select {
	case err := <-r.result:
		if err != nil {
			t.Fatal(err)
		}
		r.result <- nil // for Cleanup
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not return")
	}
	data, err := os.ReadFile(filepath.Join(r.dir, "term"))
	if err != nil || string(data) != "term\n" {
		t.Fatalf("cloudflared not terminated before serve returned: %q %v", data, err)
	}
}

func TestServePrintsTokenFragmentOnlyWithTunnel(t *testing.T) {
	dir := t.TempDir()
	c, err := config.Load(filepath.Join(dir, "absent.toml"), false)
	if err != nil {
		t.Fatal(err)
	}
	c.Listen, c.Token, c.StateDir = "127.0.0.1:0", tunnelTestToken, dir
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- serve(ctx, serverRun{Config: c}, out) }()
	eventually(t, "local URL", func() bool {
		return strings.Contains(out.String(), "WebMux local:  http://127.0.0.1:")
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "#token=") {
		t.Fatalf("non-tunnel start printed a token fragment: %q", out.String())
	}
}

func TestServeFailsBeforeListeningWithoutCloudflared(t *testing.T) {
	dir := t.TempDir()
	c, err := config.Load(filepath.Join(dir, "absent.toml"), false)
	if err != nil {
		t.Fatal(err)
	}
	c.Listen, c.Token, c.StateDir = "127.0.0.1:0", tunnelTestToken, dir
	c.Tunnel = config.TunnelConfig{Enabled: true, Cloudflared: filepath.Join(dir, "missing")}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	out := &syncBuffer{}
	if err := serve(context.Background(), serverRun{Config: c}, out); err == nil || !strings.Contains(err.Error(), "tunnel.cloudflared") {
		t.Fatalf("serve error %v", err)
	}
	if out.String() != "" {
		t.Fatalf("printed before failing: %q", out.String())
	}
}
