//go:build linux || darwin

package wslclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/runtime"
)

// webmuxBinary is built once per test binary; the bridge under test is the
// real `webmux bridge` standing in for `wsl.exe --exec webmux bridge`.
var webmuxBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("/tmp", "wsb")
	if err != nil {
		panic(err)
	}
	webmuxBinary = filepath.Join(dir, "webmux")
	build := exec.Command("go", "build", "-o", webmuxBinary, "github.com/rainchan/webmux/cmd/webmux")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type instance struct {
	dir, config, state string
}

func newInstance(t *testing.T) *instance {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "wsi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	in := &instance{dir: dir, config: filepath.Join(dir, "config.toml"), state: filepath.Join(dir, "state")}
	content := fmt.Sprintf("listen = '127.0.0.1:0'\ndefault_cwd = %q\nstate_dir = %q\nshell = '/bin/sh'\n", dir, in.state)
	if err := os.WriteFile(in.config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return in
}

func (in *instance) command(args ...string) *exec.Cmd {
	cmd := exec.Command(webmuxBinary, append(args, "--config", in.config)...)
	// CI runners are systemd services: keep their launcher markers out so
	// the server records itself as manually started.
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name != "INVOCATION_ID" && name != "XPC_SERVICE_NAME" && name != "WEBMUX_LAUNCHER" && name != "HOME" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "HOME="+in.dir)
	return cmd
}

func (in *instance) start(t *testing.T) {
	t.Helper()
	cmd := in.command("serve")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	deadline := time.Now().Add(15 * time.Second)
	for {
		if record, err := runtime.Read(runtime.Dir(in.state)); err == nil && record.PID == cmd.Process.Pid {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (in *instance) dialer() *BridgeDialer {
	return &BridgeDialer{Command: func() *exec.Cmd { return in.command("bridge") }}
}

func TestBridgeDialerReportsNotRunning(t *testing.T) {
	in := newInstance(t)
	client := localclient.New(in.dialer())
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.Status(ctx); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("status without server: %v", err)
	}
	if _, err := client.DialWebSocket(ctx, "/ws/sessions"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("websocket without server: %v", err)
	}
	failing := &BridgeDialer{Command: func() *exec.Cmd { return exec.Command("/bin/sh", "-c", "echo broken >&2; exit 1") }}
	_, err := localclient.New(failing).Status(ctx)
	if err == nil || errors.Is(err, ErrNotRunning) || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("other bridge failure: %v", err)
	}
}

func TestBridgeDialerRequestsSubscriptionAndStop(t *testing.T) {
	in := newInstance(t)
	in.start(t)
	client := localclient.New(in.dialer())
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	subscription, err := client.DialWebSocket(ctx, "/ws/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = subscription.CloseNow() }()
	var event contract.SessionEvent
	if _, data, err := subscription.Read(ctx); err != nil || json.Unmarshal(data, &event) != nil || event.Type != "snapshot" {
		t.Fatalf("snapshot %s %v", data, err)
	}
	status, err := client.Status(ctx)
	if err != nil || status.State != contract.StateRunning || !status.LocalAPI {
		t.Fatalf("status %+v %v", status, err)
	}
	if err := client.Do(ctx, http.MethodPost, "/api/sessions", map[string]any{"preset": "custom", "cmd": []string{"cat"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, data, err := subscription.Read(ctx); err != nil || !strings.Contains(string(data), `"created"`) {
		t.Fatalf("created event %s %v", data, err)
	}
	if err := client.Shutdown(ctx, false); err != nil {
		t.Fatal(err)
	}
	// The subscription ends with the server.
	for {
		if _, _, err := subscription.Read(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("subscription outlived the server")
			}
			break
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := client.Status(ctx)
		if errors.Is(err, ErrNotRunning) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server still answers after shutdown: %v", err)
		}
		client.Close()
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(runtime.Dir(in.state), "server.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record after shutdown: %v", err)
	}
}
