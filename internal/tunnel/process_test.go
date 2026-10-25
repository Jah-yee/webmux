package tunnel

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCloudflared records argv and TUNNEL_TOKEN, prints the stderr lines
// selected by FAKE_MODE, then runs until SIGTERM.
const fakeCloudflared = `#!/bin/sh
printf '%s\n' "$@" > "$FAKE_DIR/argv"
printf '%s' "${TUNNEL_TOKEN-unset}" > "$FAKE_DIR/token"
case "$FAKE_MODE" in
quick)
	echo 'ERR failed to request quick Tunnel: Post "https://api.trycloudflare.com/tunnel": EOF' >&2
	echo 'INF |  https://abc-def.trycloudflare.com                    |' >&2
	echo 'INF Registered tunnel connection connIndex=0 location=lax' >&2 ;;
named) echo "credential: $TUNNEL_TOKEN" >&2
	echo 'INF Registered tunnel connection connIndex=0' >&2 ;;
url-only) echo 'INF |  https://abc-def.trycloudflare.com  |' >&2 ;;
late) sleep 0.5
	echo 'INF |  https://late-reg.trycloudflare.com  |' >&2
	echo 'INF Registered tunnel connection connIndex=0' >&2 ;;
fail) echo 'ERR invalid tunnel token' >&2; exit 3 ;;
esac
trap 'echo term > "$FAKE_DIR/term"; exit 0' TERM
while :; do sleep 0.05; done
`

func startFake(t *testing.T, mode string, o Options) (*Tunnel, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "cloudflared")
	if err := os.WriteFile(bin, []byte(fakeCloudflared), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("FAKE_MODE", mode)
	t.Setenv("TUNNEL_TOKEN", "inherited")
	o.Binary = bin
	if o.Local == "" {
		o.Local = "http://127.0.0.1:7681"
	}
	tun, err := Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tun.Close)
	return tun, dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return string(data)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s not written: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestQuickTunnelAnnouncesPublicOrigin(t *testing.T) {
	tun, dir := startFake(t, "quick", Options{})
	select {
	case origin := <-tun.Ready():
		if origin != "https://abc-def.trycloudflare.com" {
			t.Fatalf("origin %q", origin)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not ready")
	}
	argv := readFile(t, filepath.Join(dir, "argv"))
	if argv != "tunnel\n--no-autoupdate\n--config\n/dev/null\n--url\nhttp://127.0.0.1:7681\n" {
		t.Fatalf("argv %q", argv)
	}
	if token := readFile(t, filepath.Join(dir, "token")); token != "unset" {
		t.Fatalf("Quick Tunnel inherited TUNNEL_TOKEN %q", token)
	}
}

func TestNamedTunnelKeepsCredentialOutOfArgv(t *testing.T) {
	tun, dir := startFake(t, "named", Options{Token: "named-secret", Hostname: "term.example.com"})
	select {
	case origin := <-tun.Ready():
		if origin != "https://term.example.com" {
			t.Fatalf("origin %q", origin)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("not ready")
	}
	argv := readFile(t, filepath.Join(dir, "argv"))
	if strings.Contains(argv, "named-secret") || argv != "tunnel\n--no-autoupdate\n--config\n/dev/null\nrun\n" {
		t.Fatalf("argv %q", argv)
	}
	if token := readFile(t, filepath.Join(dir, "token")); token != "named-secret" {
		t.Fatalf("TUNNEL_TOKEN %q", token)
	}
}

func TestReadyTimesOutWithoutRegistration(t *testing.T) {
	tun, _ := startFake(t, "url-only", Options{ReadyTimeout: 200 * time.Millisecond})
	select {
	case origin := <-tun.Ready():
		t.Fatalf("announced unregistered tunnel %q", origin)
	case <-time.After(600 * time.Millisecond):
	}
	select {
	case <-tun.Done():
		t.Fatal("timeout stopped cloudflared")
	default:
	}
}

func TestReadyAcceptsLateRegistration(t *testing.T) {
	tun, _ := startFake(t, "late", Options{ReadyTimeout: 100 * time.Millisecond})
	select {
	case origin := <-tun.Ready():
		if origin != "https://late-reg.trycloudflare.com" {
			t.Fatalf("origin %q", origin)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late registration was not announced")
	}
}

func TestStartupFailureReportsExitStatus(t *testing.T) {
	tun, _ := startFake(t, "fail", Options{})
	select {
	case <-tun.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	if err := tun.Err(); err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("exit error %v", err)
	}
	select {
	case origin := <-tun.Ready():
		t.Fatalf("failed process announced %q", origin)
	default:
	}
}

func TestCloseTerminatesProcess(t *testing.T) {
	tun, dir := startFake(t, "quick", Options{})
	<-tun.Ready()
	start := time.Now()
	tun.Close()
	if time.Since(start) > 4*time.Second {
		t.Fatalf("Close took %s", time.Since(start))
	}
	if readFile(t, filepath.Join(dir, "term")) != "term\n" {
		t.Fatal("cloudflared did not receive SIGTERM")
	}
	select {
	case <-tun.Done():
	default:
		t.Fatal("Done not closed after Close")
	}
	if err := tun.Err(); err != nil {
		t.Fatalf("Close reported %v", err)
	}
}

func TestNamedCredentialNeverReachesDebugLogs(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tun, _ := startFake(t, "named", Options{Token: "do-not-log-this-credential", Hostname: "term.example.com", Logger: logger})
	select {
	case <-tun.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("fake named tunnel never registered")
	}
	tun.Close()
	if strings.Contains(logs.String(), "do-not-log-this-credential") {
		t.Fatal("cloudflared output leaked credential")
	}
}
