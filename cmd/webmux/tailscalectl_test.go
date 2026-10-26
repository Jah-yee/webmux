package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/statefile"
	"github.com/rainchan/webmux/internal/tailscale"
)

func fakeTailscale(t *testing.T, status, serve string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	statusPath := filepath.Join(dir, "status.json")
	servePath := filepath.Join(dir, "serve.json")
	logPath := filepath.Join(dir, "calls.log")
	if err := os.WriteFile(statusPath, []byte(status), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(servePath, []byte(serve), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$*" in
"status --json") cat "$FAKE_STATUS";;
"serve status --json") cat "$FAKE_SERVE";;
*) echo "$@" >> "$FAKE_LOG";;
esac
`
	cli := filepath.Join(dir, "tailscale")
	if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_STATUS", statusPath)
	t.Setenv("FAKE_SERVE", servePath)
	t.Setenv("FAKE_LOG", logPath)
	return cli, logPath
}

func testTailscaleController(t *testing.T, cli, stateDir string, cfg config.TailscaleConfig) *tailscaleController {
	t.Helper()
	service := tailscale.New(tailscale.Options{Command: cli, Provider: cfg.Provider, IsWSL: func() bool { return false }})
	return newTailscaleController(cfg, service, stateDir, &api.PublicOrigins{}, "token", false,
		io.Discard, slog.New(slog.DiscardHandler), func() bool { return false }, func(context.Context) string { return "unknown" })
}

const tailscaleRunning = `{"BackendState":"Running","Self":{"DNSName":"node.tailnet.ts.net."},"CertDomains":["node.tailnet.ts.net"]}`

func TestPrepareRemovesExplicitPortBeforeAutomaticSelection(t *testing.T) {
	cli, logPath := fakeTailscale(t,
		tailscaleRunning,
		`{"Web":{"node.tailnet.ts.net:9000":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:7681"}}}},"AllowFunnel":{}}`)
	dir := t.TempDir()
	record := tailscaleState{V: 1, Provider: "linux", DNSName: "node.tailnet.ts.net", Mode: "serve", Port: 9000, Target: "http://127.0.0.1:7681", Status: "active"}
	if err := statefile.WriteJSON(filepath.Join(dir, "tailscale.json"), record, statefile.Options{TempPattern: ".ts-*"}); err != nil {
		t.Fatal(err)
	}
	ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "serve", Port: 0, Provider: "auto"})
	if err := ctl.Prepare(context.Background(), "127.0.0.1:7681"); err != nil {
		t.Fatal(err)
	}
	defer ctl.Close()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "serve --https=9000 off") {
		t.Fatalf("old explicit entry was not removed: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "tailscale.json")); !os.IsNotExist(err) {
		t.Fatalf("state record survived removal: %v", err)
	}
}

func TestPrepareAndCloseKeepForeignTarget(t *testing.T) {
	cli, logPath := fakeTailscale(t,
		tailscaleRunning,
		`{"Web":{"node.tailnet.ts.net:9000":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:9999"}}}},"AllowFunnel":{}}`)
	dir := t.TempDir()
	record := tailscaleState{V: 1, Provider: "linux", DNSName: "node.tailnet.ts.net", Mode: "serve", Port: 9000, Target: "http://127.0.0.1:7681", Status: "active"}
	if err := statefile.WriteJSON(filepath.Join(dir, "tailscale.json"), record, statefile.Options{TempPattern: ".ts-*"}); err != nil {
		t.Fatal(err)
	}
	ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "off", Provider: "auto"})
	if err := ctl.Prepare(context.Background(), "127.0.0.1:7681"); err == nil {
		t.Fatal("Prepare accepted an entry with a different target")
	}
	ctl.Close()
	if data, err := os.ReadFile(logPath); err == nil && strings.Contains(string(data), "off") {
		t.Fatalf("foreign entry was removed: %q", data)
	}
}

func TestPrepareFailsClosedWhenServeStatusUnreadable(t *testing.T) {
	cli, logPath := fakeTailscale(t, tailscaleRunning, "not json")
	dir := t.TempDir()
	record := tailscaleState{V: 1, Provider: "linux", DNSName: "node.tailnet.ts.net", Mode: "serve", Port: 9000, Target: "http://127.0.0.1:7681", Status: "active"}
	if err := statefile.WriteJSON(filepath.Join(dir, "tailscale.json"), record, statefile.Options{TempPattern: ".ts-*"}); err != nil {
		t.Fatal(err)
	}
	ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "off", Provider: "auto"})
	if err := ctl.Prepare(context.Background(), "127.0.0.1:7681"); err == nil {
		t.Fatal("Prepare removed an entry it could not verify")
	}
	ctl.Close()
	if data, err := os.ReadFile(logPath); err == nil && strings.Contains(string(data), "off") {
		t.Fatalf("unverified entry was removed: %q", data)
	}
}

func TestPrepareRemovesEntryBeforeProviderChange(t *testing.T) {
	cli, logPath := fakeTailscale(t,
		tailscaleRunning,
		`{"Web":{"node.tailnet.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:7681"}}}},"AllowFunnel":{}}`)
	dir := t.TempDir()
	record := tailscaleState{V: 1, Provider: "windows", DNSName: "node.tailnet.ts.net", Mode: "serve", Port: 443, Target: "http://127.0.0.1:7681", Status: "active"}
	if err := statefile.WriteJSON(filepath.Join(dir, "tailscale.json"), record, statefile.Options{TempPattern: ".ts-*"}); err != nil {
		t.Fatal(err)
	}
	service := tailscale.New(tailscale.Options{Command: cli, Provider: "linux", IsWSL: func() bool { return true }})
	ctl := newTailscaleController(config.TailscaleConfig{Mode: "serve", Provider: "linux"}, service, dir,
		&api.PublicOrigins{}, "token", false, io.Discard, slog.New(slog.DiscardHandler),
		func() bool { return true }, func(context.Context) string { return "mirrored" })
	if err := ctl.Prepare(context.Background(), "127.0.0.1:7681"); err != nil {
		t.Fatal(err)
	}
	defer ctl.Close()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "serve --https=443 off") {
		t.Fatalf("old provider entry was not removed: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "tailscale.json")); !os.IsNotExist(err) {
		t.Fatalf("state record survived provider change: %v", err)
	}
}

func TestFailKeepsAuthoredReason(t *testing.T) {
	cli, _ := fakeTailscale(t, tailscaleRunning, `{"Web":{},"AllowFunnel":{}}`)
	ctl := testTailscaleController(t, cli, t.TempDir(), config.TailscaleConfig{Mode: "off", Provider: "auto"})
	ctl.fail(errors.New("端口 443 已被其他 Tailscale 入口占用，WebMux 不会覆盖"), false)
	if got := ctl.Status().Error; !strings.Contains(got, "端口 443") {
		t.Fatalf("authored reason = %q", got)
	}
	ctl.fail(&tailscale.CLIError{Action: "tailscale funnel", Detail: "access denied"}, false)
	if got := ctl.Status().Error; !strings.Contains(got, "set --operator") {
		t.Fatalf("CLI reason = %q", got)
	}
}

func TestPrepareRefusesCorruptStatePointingAtTarget(t *testing.T) {
	cli, _ := fakeTailscale(t,
		tailscaleRunning,
		`{"Web":{"node.tailnet.ts.net:443":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:7681"}}}},"AllowFunnel":{}}`)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tailscale.json"), []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "off", Provider: "auto"})
	if err := ctl.Prepare(context.Background(), "127.0.0.1:7681"); err == nil {
		t.Fatal("corrupt state with a matching entry was accepted")
	}
}

func TestWindowsProviderNATFailsClosed(t *testing.T) {
	cli, logPath := fakeTailscale(t, tailscaleRunning, `{"Web":{},"AllowFunnel":{}}`)
	dir := t.TempDir()
	service := tailscale.New(tailscale.Options{Command: cli, Provider: "windows", IsWSL: func() bool { return true }})
	ctl := newTailscaleController(config.TailscaleConfig{Mode: "serve", Provider: "windows"}, service, dir,
		&api.PublicOrigins{}, "token", false, io.Discard, slog.New(slog.DiscardHandler),
		func() bool { return true }, func(context.Context) string { return "nat" })
	ctl.localPort = 7681
	ctl.apply(context.Background())
	status := ctl.Status()
	if status.State != "failed" || !strings.Contains(status.BlockedReason, "mirrored") {
		t.Fatalf("NAT status = %+v", status)
	}
	if data, _ := os.ReadFile(logPath); len(data) != 0 {
		t.Fatalf("NAT provider ran write commands: %q", data)
	}
}

func TestTailscaleListenTargets(t *testing.T) {
	for _, row := range []struct {
		listen string
		target string
		ok     bool
	}{
		{"127.0.0.1:7681", "http://127.0.0.1:7681", true},
		{"0.0.0.0:7681", "http://127.0.0.1:7681", true},
		{"localhost:7681", "http://127.0.0.1:7681", true},
		{"[::1]:7681", "", false},
		{"100.64.0.5:7681", "", false},
		{"127.0.0.1:0", "", false},
	} {
		target, ok := targetFromListen(row.listen)
		if target != row.target || ok != row.ok {
			t.Errorf("targetFromListen(%q) = %q, %v; want %q, %v", row.listen, target, ok, row.target, row.ok)
		}
	}
	for _, row := range []struct {
		local string
		port  int
		ok    bool
	}{
		{"http://127.0.0.1:7681", 7681, true},
		{"http://[::1]:7681", 0, false},
		{"http://100.64.0.5:7681", 0, false},
	} {
		port, ok := localPort(row.local)
		if port != row.port || ok != row.ok {
			t.Errorf("localPort(%q) = %d, %v; want %d, %v", row.local, port, ok, row.port, row.ok)
		}
	}
}

func TestClassifyTailscaleError(t *testing.T) {
	if got := classifyTailscaleError(errText("access denied"), "windows"); !strings.Contains(got, "set --operator") {
		t.Fatalf("windows permission = %q", got)
	}
	if got := classifyTailscaleError(errText("funnel not available"), "linux"); !strings.Contains(got, "funnel 属性") {
		t.Fatalf("funnel attribute = %q", got)
	}
	if got := classifyTailscaleError(errText("certificate not ready"), "linux"); !strings.Contains(got, "HTTPS") {
		t.Fatalf("certificate = %q", got)
	}
}

type errText string

func (e errText) Error() string { return string(e) }
