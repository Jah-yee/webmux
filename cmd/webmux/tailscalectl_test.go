package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/statefile"
	"github.com/rainchan/webmux/internal/tailscale"
	"github.com/rainchan/webmux/internal/tailscalesettings"
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
"status --json --peers=false") cat "$FAKE_STATUS";;
"serve status --json") cat "$FAKE_SERVE";;
*--bg*) echo "$@" >> "$FAKE_LOG"; if [ -n "$FAKE_AFTER" ]; then cp "$FAKE_AFTER" "$FAKE_SERVE"; fi;;
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
	return newTailscaleController(cfg, nil, service, stateDir, &api.PublicOrigins{}, "token", false,
		io.Discard, slog.New(slog.DiscardHandler), func() bool { return false }, func(context.Context) string { return "unknown" })
}

const tailscaleRunning = `{"BackendState":"Running","Self":{"DNSName":"node.tailnet.ts.net."},"CertDomains":["node.tailnet.ts.net"]}`

func TestPrepareRemovesExplicitPortBeforeAutomaticSelection(t *testing.T) {
	cli, logPath := fakeTailscale(t,
		tailscaleRunning,
		`{"Web":{"node.tailnet.ts.net:9000":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:7681"}}}},"AllowFunnel":{}}`)
	dir := t.TempDir()
	record := tailscaleState{V: 1, Provider: "linux", DNSName: "node.tailnet.ts.net", Mode: "serve", Port: 9000, Target: "http://127.0.0.1:7681", Status: "active"}
	if err := statefile.WriteJSON(filepath.Join(dir, "tailscale-ownership.json"), record, statefile.Options{TempPattern: ".ts-*"}); err != nil {
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
	if _, err := os.Stat(filepath.Join(dir, "tailscale-ownership.json")); !os.IsNotExist(err) {
		t.Fatalf("state record survived removal: %v", err)
	}
}

func TestStartAppliesServeAndCloseRemovesIt(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	target := fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port)
	cli, logPath := fakeTailscale(t, tailscaleRunning, `{"Web":{},"AllowFunnel":{}}`)
	after := filepath.Join(t.TempDir(), "after.json")
	applied := `{"Web":{"node.tailnet.ts.net:8443":{"Handlers":{"/":{"Proxy":"` + target + `"}}}},"AllowFunnel":{}}`
	if err := os.WriteFile(after, []byte(applied), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_AFTER", after)
	dir := t.TempDir()
	ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "serve", Port: 8443, Provider: "auto"})
	if err := ctl.Prepare(context.Background(), ln.Addr().String()); err != nil {
		t.Fatal(err)
	}
	ctl.Start(context.Background(), target)
	<-ctl.done
	if status := ctl.Status(); status.State != "ready" || status.PublicURL != "https://node.tailnet.ts.net:8443" {
		t.Fatalf("status = %+v", status)
	}
	var record tailscaleState
	data, err := os.ReadFile(filepath.Join(dir, "tailscale-ownership.json"))
	if err != nil || json.Unmarshal(data, &record) != nil || record.Status != "active" || record.Port != 8443 || record.Target != target {
		t.Fatalf("record = %s (%v)", data, err)
	}
	ctl.Close()
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "serve --bg --https=8443 " + target + "\nserve --https=8443 off\n"
	if string(calls) != want {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "tailscale-ownership.json")); !os.IsNotExist(err) {
		t.Fatalf("state record survived Close: %v", err)
	}
}

// statefulTailscale is a fake CLI whose serve status follows its own
// serve/funnel/off commands, one entry at a time.
func statefulTailscale(t *testing.T) (string, string) {
	t.Helper()
	cli, logPath := fakeTailscale(t, tailscaleRunning, `{"Web":{},"AllowFunnel":{}}`)
	script := `#!/bin/sh
host=node.tailnet.ts.net
case "$*" in
"status --json --peers=false") cat "$FAKE_STATUS";;
"serve status --json") cat "$FAKE_SERVE";;
*--bg*) echo "$@" >> "$FAKE_LOG"; port=${3#--https=}; public=""
  if [ "$1" = funnel ]; then public="\"$host:$port\":true"; fi
  printf '{"Web":{"%s:%s":{"Handlers":{"/":{"Proxy":"%s"}}}},"AllowFunnel":{%s}}' "$host" "$port" "$4" "$public" > "$FAKE_SERVE";;
*off) echo "$@" >> "$FAKE_LOG"; echo '{"Web":{},"AllowFunnel":{}}' > "$FAKE_SERVE";;
esac
`
	if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return cli, logPath
}

func TestUpdateSettingsReplacesEntryAtRuntime(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	target := fmt.Sprintf("http://127.0.0.1:%d", ln.Addr().(*net.TCPAddr).Port)
	cli, logPath := statefulTailscale(t)
	dir := t.TempDir()
	toml := config.TailscaleConfig{Mode: "serve", Port: 8443, Provider: "auto"}
	store := tailscalesettings.Open(dir, toml)
	defer store.Close()
	service := tailscale.New(tailscale.Options{Command: cli, Provider: "auto", IsWSL: func() bool { return false }})
	ctl := newTailscaleController(toml, store, service, dir, &api.PublicOrigins{}, strings.Repeat("t", minTunnelTokenLength), false,
		io.Discard, slog.New(slog.DiscardHandler), func() bool { return false }, func(context.Context) string { return "unknown" })
	if err := ctl.Prepare(context.Background(), ln.Addr().String()); err != nil {
		t.Fatal(err)
	}
	ctl.Start(context.Background(), target)
	wait := func() contract.TailscaleStatus {
		ctl.mu.Lock()
		done := ctl.done
		ctl.mu.Unlock()
		<-done
		return ctl.Status()
	}
	if status := wait(); status.State != "ready" || status.Source != "config" {
		t.Fatalf("startup = %+v", status)
	}

	if _, err := ctl.UpdateSettings(contract.TailscaleSettingsUpdate{Mode: "funnel", Port: 10000, Provider: "auto"}); !errors.Is(err, tailscalesettings.ErrFunnelUnconfirmed) {
		t.Fatalf("unconfirmed Funnel = %v", err)
	}
	if got, web := store.Effective(); got != toml || web {
		t.Fatalf("rejected update was persisted: %+v %v", got, web)
	}
	view, err := ctl.UpdateSettings(contract.TailscaleSettingsUpdate{Mode: "funnel", Port: 10000, Provider: "auto", ConfirmFunnel: true})
	if err != nil || view.State != "starting" || view.Mode != "funnel" || view.Source != "web" {
		t.Fatalf("confirmed Funnel = %+v %v", view, err)
	}
	if status := wait(); status.State != "ready" || status.PublicURL != "https://node.tailnet.ts.net:10000" {
		t.Fatalf("after switch = %+v", status)
	}

	view, err = ctl.ResetSettings()
	if err != nil || view.Mode != "off" || view.Source != "config" {
		t.Fatalf("reset = %+v %v", view, err)
	}
	if status := wait(); status.State != "off" || status.PublicURL != "" {
		t.Fatalf("after reset = %+v", status)
	}
	ctl.Close()
	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "serve --bg --https=8443 " + target + "\n" +
		"serve --https=8443 off\n" +
		"funnel --bg --https=10000 " + target + "\n" +
		"serve --https=10000 off\n"
	if string(calls) != want {
		t.Fatalf("calls = %q, want %q", calls, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "tailscale-ownership.json")); !os.IsNotExist(err) {
		t.Fatalf("ownership record survived reset: %v", err)
	}
}

func TestUpdateSettingsRequiresStrongToken(t *testing.T) {
	cli, logPath := statefulTailscale(t)
	dir := t.TempDir()
	store := tailscalesettings.Open(dir, config.TailscaleConfig{Mode: "off", Provider: "auto"})
	defer store.Close()
	ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "off", Provider: "auto"})
	ctl.store = store
	if _, err := ctl.UpdateSettings(contract.TailscaleSettingsUpdate{Mode: "serve", Provider: "auto"}); !errors.Is(err, tailscalesettings.ErrEnableBlocked) {
		t.Fatalf("weak token update = %v", err)
	}
	if status := ctl.Status(); status.CanEnable || !strings.Contains(status.EnableBlockedReason, "token") {
		t.Fatalf("status = %+v", status)
	}
	if _, web := store.Effective(); web {
		t.Fatal("blocked update was persisted")
	}
	if data, _ := os.ReadFile(logPath); len(data) != 0 {
		t.Fatalf("blocked update ran commands: %q", data)
	}
}

func TestPrepareDropsStaleRecordKeepingForeignTarget(t *testing.T) {
	cli, logPath := fakeTailscale(t,
		tailscaleRunning,
		`{"Web":{"node.tailnet.ts.net:9000":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:9999"}}}},"AllowFunnel":{}}`)
	dir := t.TempDir()
	record := tailscaleState{V: 1, Provider: "linux", DNSName: "node.tailnet.ts.net", Mode: "serve", Port: 9000, Target: "http://127.0.0.1:7681", Status: "active"}
	if err := statefile.WriteJSON(filepath.Join(dir, "tailscale-ownership.json"), record, statefile.Options{TempPattern: ".ts-*"}); err != nil {
		t.Fatal(err)
	}
	ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "off", Provider: "auto"})
	if err := ctl.Prepare(context.Background(), "127.0.0.1:7681"); err != nil {
		t.Fatalf("a record whose port now proxies elsewhere blocked startup: %v", err)
	}
	ctl.Close()
	if data, err := os.ReadFile(logPath); err == nil && strings.Contains(string(data), "off") {
		t.Fatalf("foreign entry was removed: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "tailscale-ownership.json")); !os.IsNotExist(err) {
		t.Fatalf("stale record survived: %v", err)
	}
}

func TestPrepareFailsClosedWhenServeStatusUnreadable(t *testing.T) {
	cli, logPath := fakeTailscale(t, tailscaleRunning, "not json")
	dir := t.TempDir()
	record := tailscaleState{V: 1, Provider: "linux", DNSName: "node.tailnet.ts.net", Mode: "serve", Port: 9000, Target: "http://127.0.0.1:7681", Status: "active"}
	if err := statefile.WriteJSON(filepath.Join(dir, "tailscale-ownership.json"), record, statefile.Options{TempPattern: ".ts-*"}); err != nil {
		t.Fatal(err)
	}
	ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "off", Provider: "auto"})
	err := ctl.Prepare(context.Background(), "127.0.0.1:7681")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "tailscale-ownership.json")) {
		t.Fatalf("Prepare = %v; want a refusal naming the state file", err)
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
	if err := statefile.WriteJSON(filepath.Join(dir, "tailscale-ownership.json"), record, statefile.Options{TempPattern: ".ts-*"}); err != nil {
		t.Fatal(err)
	}
	service := tailscale.New(tailscale.Options{Command: cli, Provider: "linux", IsWSL: func() bool { return true }})
	ctl := newTailscaleController(config.TailscaleConfig{Mode: "serve", Provider: "linux"}, nil, service, dir,
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
	if _, err := os.Stat(filepath.Join(dir, "tailscale-ownership.json")); !os.IsNotExist(err) {
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
	if err := os.WriteFile(filepath.Join(dir, "tailscale-ownership.json"), []byte("{not json"), 0600); err != nil {
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
	ctl := newTailscaleController(config.TailscaleConfig{Mode: "serve", Provider: "windows"}, nil, service, dir,
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

func TestLegacyOwnershipBlocksBeforeAnyExternalMutation(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(fmt.Sprint(both), func(t *testing.T) {
			cli, logPath := fakeTailscale(t, tailscaleRunning, `{}`)
			dir := t.TempDir()
			legacy := filepath.Join(dir, "tailscale.json")
			if err := os.WriteFile(legacy, []byte("broken legacy record"), 0600); err != nil {
				t.Fatal(err)
			}
			if both {
				record := tailscaleState{V: 1, Provider: "linux", DNSName: "node.tailnet.ts.net", Mode: "serve", Port: 443, Target: "http://127.0.0.1:7681", Status: "active"}
				if err := statefile.WriteJSON(filepath.Join(dir, "tailscale-ownership.json"), record, statefile.Options{}); err != nil {
					t.Fatal(err)
				}
			}
			ctl := testTailscaleController(t, cli, dir, config.TailscaleConfig{Mode: "off", Provider: "auto"})
			defer ctl.Close()
			if err := ctl.Prepare(context.Background(), "127.0.0.1:7681"); !statefile.IsKind(err, statefile.LegacyLayout) {
				t.Fatalf("legacy not blocked: %v", err)
			}
			if calls, _ := os.ReadFile(logPath); len(calls) != 0 {
				t.Fatalf("external mutation before ownership check: %s", calls)
			}
			if data, _ := os.ReadFile(legacy); string(data) != "broken legacy record" {
				t.Fatal("legacy record altered")
			}
		})
	}
}
