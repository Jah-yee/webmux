package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/runtime"
)

const fixtureToken = "fixture-token-0123456789"

// fixture is an isolated HOME, configuration and state directory. Paths sit
// under /tmp because macOS temporary directories overflow sun_path.
type fixture struct {
	dir, config, state string
	env                []string
}

func newFixture(t *testing.T, extra string) *fixture {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "wmx")
	if err != nil {
		t.Fatal(err)
	}
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	f := &fixture{dir: dir, config: filepath.Join(dir, "config.toml"), state: filepath.Join(dir, "state")}
	content := fmt.Sprintf("listen = '127.0.0.1:0'\ntoken = %q\ndefault_cwd = %q\nstate_dir = %q\nshell = '/bin/sh'\n%s", fixtureToken, dir, f.state, extra)
	if err := os.WriteFile(f.config, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "HOME", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", "INVOCATION_ID", "XPC_SERVICE_NAME", "WEBMUX_LAUNCHER", "WEBMUX_SESSION_ID":
			continue
		}
		f.env = append(f.env, entry)
	}
	f.env = append(f.env, "HOME="+dir)
	return f
}

func (f *fixture) command(env []string, args ...string) *exec.Cmd {
	encoded, _ := json.Marshal(args)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(append(slices.Clone(f.env), env...), "WEBMUX_TEST_EXEC="+string(encoded))
	return cmd
}

// run executes a client subcommand with --config appended.
func (f *fixture) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := f.command(nil, append(args, "--config", f.config)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	return stdout.String(), stderr.String(), cmd.ProcessState.ExitCode()
}

type server struct {
	cmd    *exec.Cmd
	stderr *syncBuffer
	exited chan struct{}
}

// start runs a server and waits until its record names it, unless
// waitRecord is false (an instance that cannot own the record).
func (f *fixture) start(t *testing.T, env []string, waitRecord bool, args ...string) *server {
	t.Helper()
	cmd := f.command(env, args...)
	s := &server{cmd: cmd, stderr: &syncBuffer{}, exited: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = s.stderr, s.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait(); close(s.exited) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-s.exited
	})
	eventually(t, "server start", func() bool {
		if !waitRecord {
			return strings.Contains(s.stderr.String(), "WebMux local:")
		}
		record, err := runtime.Read(runtime.Dir(f.state))
		return err == nil && record.PID == cmd.Process.Pid
	})
	return s
}

func (s *server) waitExit(t *testing.T) {
	t.Helper()
	select {
	case <-s.exited:
	case <-time.After(20 * time.Second):
		t.Fatal("server did not exit")
	}
}

func (f *fixture) record(t *testing.T) runtime.Record {
	t.Helper()
	record, err := runtime.Read(runtime.Dir(f.state))
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func (f *fixture) local(t *testing.T) *localclient.Client {
	client := localclient.New(localclient.UnixDialer{Path: f.record(t).Socket})
	t.Cleanup(client.Close)
	return client
}

func decodeStatus(t *testing.T, stdout string) contract.StatusV1 {
	t.Helper()
	var status contract.StatusV1
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("status JSON %q: %v", stdout, err)
	}
	var fields map[string]any
	_ = json.Unmarshal([]byte(stdout), &fields)
	for _, key := range []string{"v", "state", "record", "localApi", "pid", "version", "launcher", "listen", "sessions", "health", "urls", "tokenConfigured", "drift", "warnings", "config", "logFile", "socket"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("status JSON lacks %q: %s", key, stdout)
		}
	}
	return status
}

func TestDispatchRejectsUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := dispatch([]string{"statsu"}, &stdout, &stderr); code != contract.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), `unknown subcommand "statsu"`) || !strings.Contains(stderr.String(), subcommands) {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := dispatch([]string{"version"}, &stdout, &stderr); code != 0 || stdout.String() != "webmux "+version+"\n" {
		t.Fatalf("version %d %q", code, stdout.String())
	}
}

// Covers the legacy `webmux --config` invocation, the local API, status
// over the socket and fallback, and stop through the local API.
func TestServerLocalAPIStatusAndStop(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	stdout, _, code := f.run(t, "status", "--json")
	if status := decodeStatus(t, stdout); code != contract.ExitNotRunning || status.State != contract.StateStopped {
		t.Fatalf("before start: %d %+v", code, status)
	}
	srv := f.start(t, nil, true, "--config", f.config)
	record := f.record(t)
	runDir := runtime.Dir(f.state)
	for path, mode := range map[string]os.FileMode{runDir: 0700, runtime.RecordPath(runDir): 0600, record.Socket: 0600} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: %v %v", path, info, err)
		}
	}
	data, _ := os.ReadFile(runtime.RecordPath(runDir))
	if strings.Contains(string(data), fixtureToken) || strings.HasSuffix(record.Listen, ":0") || record.Launcher != "manual" {
		t.Fatalf("record %s", data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := f.local(t)
	var info contract.SessionInfo
	if err := client.Do(ctx, http.MethodPost, "/api/sessions", map[string]any{"preset": "custom", "cmd": []string{"cat"}}, &info); err != nil {
		t.Fatalf("create over socket without token: %v", err)
	}
	terminal(t, ctx, client, info.ID)

	stdout, stderr, code := f.run(t, "status", "--json")
	status := decodeStatus(t, stdout)
	if code != 0 || !status.LocalAPI || !status.Record || status.State != contract.StateRunning || status.Sessions == nil || *status.Sessions != 1 || status.Listen != record.Listen || status.Token != "" {
		t.Fatalf("status %d %+v", code, status)
	}
	if strings.Contains(stdout+stderr, fixtureToken) {
		t.Fatal("status printed the token without --reveal-token")
	}
	human, _, _ := f.run(t, "status")
	if !strings.Contains(human, "WebMux is running") || strings.Contains(human, fixtureToken) {
		t.Fatalf("human status %q", human)
	}
	stdout, _, _ = f.run(t, "status", "--json", "--reveal-token")
	if decodeStatus(t, stdout).Token != fixtureToken {
		t.Fatalf("revealed token missing: %s", stdout)
	}

	tcp, err := http.NewRequest(http.MethodGet, "http://"+record.Listen+contract.PathToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	tcp.Header.Set("Authorization", "Bearer "+fixtureToken)
	resp, err := http.DefaultClient.Do(tcp)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("TCP %s: %d", contract.PathToken, resp.StatusCode)
	}

	stdout, _, code = f.run(t, "stop", "--json")
	var stop contract.StopV1
	if err := json.Unmarshal([]byte(stdout), &stop); err != nil || code != 0 || stop.Result != contract.StopStopped {
		t.Fatalf("stop %d %s", code, stdout)
	}
	srv.waitExit(t)
	if _, err := os.Stat(runtime.RecordPath(runDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record left behind: %v", err)
	}
	if _, err := os.Stat(record.Socket); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket left behind: %v", err)
	}
	if _, _, code := f.run(t, "stop"); code != 0 {
		t.Fatalf("idempotent stop exit %d", code)
	}
}

// terminal writes through /ws/s/{id} and waits for cat's echo.
func terminal(t *testing.T, ctx context.Context, client *localclient.Client, id string) {
	t.Helper()
	conn, err := client.DialWebSocket(ctx, "/ws/s/"+id+"?client=test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := conn.Write(ctx, websocket.MessageBinary, append([]byte{0x00}, "hello-local\n"...)); err != nil {
		t.Fatal(err)
	}
	var seen []byte
	for !bytes.Contains(seen, []byte("hello-local")) {
		_, frame, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("terminal output %q: %v", seen, err)
		}
		if len(frame) > 0 && (frame[0] == 0x00 || frame[0] == 0x01) {
			seen = append(seen, frame[1:]...)
		}
	}
}

func TestServiceManagedInstanceRefusesStop(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	srv := f.start(t, []string{"INVOCATION_ID=test-unit"}, true, "serve", "--config", f.config)
	if launcher := f.record(t).Launcher; launcher != "systemd" {
		t.Fatalf("launcher %q", launcher)
	}
	_, stderr, code := f.run(t, "stop")
	if code != 1 || !strings.Contains(stderr, "systemctl --user stop webmux") {
		t.Fatalf("stop %d %q", code, stderr)
	}
	stdout, _, code := f.run(t, "status", "--json")
	if code != 0 || decodeStatus(t, stdout).State != contract.StateRunning {
		t.Fatalf("refused stop still stopped the server: %d %s", code, stdout)
	}
	stdout, _, code = f.run(t, "stop", "--force", "--json")
	if code != 0 || !strings.Contains(stdout, `"stopped"`) {
		t.Fatalf("forced stop %d %s", code, stdout)
	}
	srv.waitExit(t)
}

// A program inside a session may create sibling sessions, but may not read
// the token or stop the server.
func TestSessionProcessesCannotStopOrReadToken(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is required to act as an in-session client")
	}
	f := newFixture(t, "")
	f.start(t, nil, true, "--config", f.config)
	record := f.record(t)
	out := filepath.Join(f.dir, "inside.txt")
	stop, _ := json.Marshal([]string{"stop", "--config", f.config})
	script := `curl -s -o /dev/null -w "token=%{http_code}\n" --unix-socket "$1" http://webmux/local/v1/token
curl -s -o /dev/null -w "create=%{http_code}\n" --unix-socket "$1" -H 'Content-Type: application/json' -d '{"preset":"custom","cmd":["true"]}' http://webmux/api/sessions
WEBMUX_TEST_EXEC="$3" "$2" 2>&1; echo "stop=$?"`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := f.local(t)
	cmd := []string{"/bin/sh", "-c", "{\n" + script + "\n} > \"$4.tmp\"; mv \"$4.tmp\" \"$4\"", "sh", record.Socket, os.Args[0], string(stop), out}
	if err := client.Do(ctx, http.MethodPost, "/api/sessions", map[string]any{"preset": "custom", "cmd": cmd}, nil); err != nil {
		t.Fatal(err)
	}
	var result []byte
	eventually(t, "in-session client output", func() bool {
		var err error
		result, err = os.ReadFile(out)
		return err == nil
	})
	text := string(result)
	if !strings.Contains(text, "token=403") || !strings.Contains(text, "create=201") || !strings.Contains(text, "stop=1") || !strings.Contains(text, "cannot stop") {
		t.Fatalf("in-session results:\n%s", text)
	}
	stdout, _, code := f.run(t, "status", "--json")
	if code != 0 || decodeStatus(t, stdout).State != contract.StateRunning {
		t.Fatalf("server stopped by a session process: %s", stdout)
	}
}

func TestSecondInstanceKeepsFirstRecord(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	first := f.start(t, nil, true, "--config", f.config)
	second := f.start(t, nil, false, "serve", "--config", f.config)
	eventually(t, "lock warning", func() bool { return strings.Contains(second.stderr.String(), "状态目录已被另一实例使用") })
	if record := f.record(t); record.PID != first.cmd.Process.Pid {
		t.Fatalf("record overwritten by PID %d", record.PID)
	}
	link := strings.TrimSpace(strings.SplitN(strings.SplitN(second.stderr.String(), "WebMux local:", 2)[1], "\n", 2)[0])
	req, err := http.NewRequest(http.MethodGet, link+"/api/health", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+fixtureToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("second instance TCP health %d", resp.StatusCode)
	}
	status, err := f.local(t).Status(context.Background())
	if err != nil || status.PID == nil || *status.PID != first.cmd.Process.Pid {
		t.Fatalf("socket not owned by the first instance: %+v %v", status, err)
	}
	_ = second.cmd.Process.Signal(syscall.SIGTERM)
	second.waitExit(t)
	if record := f.record(t); record.PID != first.cmd.Process.Pid {
		t.Fatal("second instance removed the first instance's record")
	}
}

// standIn writes a record for a non-webmux process whose socket accepts
// connections but never answers, as a hung server would.
func standIn(t *testing.T, f *fixture, script string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-exited })
	var identity runtime.Identity
	eventually(t, "stand-in identity", func() bool {
		var err error
		identity, err = runtime.ProcessIdentity(cmd.Process.Pid)
		// macOS identities carry no executable path.
		return err == nil && (identity.Exe == "" || strings.HasSuffix(identity.Exe, "sh"))
	})
	dir := runtime.Dir(f.state)
	if err := runtime.EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "webmux.sock")
	hung, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = hung.Close() })
	go func() {
		for {
			conn, err := hung.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()
	idle, _ := net.Listen("tcp", "127.0.0.1:0")
	t.Cleanup(func() { _ = idle.Close() })
	record := runtime.Record{PID: cmd.Process.Pid, Identity: identity, Listen: idle.Addr().String(), ConfiguredListen: "127.0.0.1:0", Launcher: "manual", Socket: socket, StartedAt: time.Now()}
	if err := runtime.Write(dir, record); err != nil {
		t.Fatal(err)
	}
	return cmd
}

func TestStopFallsBackToSignals(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	// A loop keeps the shell itself as the recorded process (no exec).
	standIn(t, f, "while :; do sleep 0.1; done")
	stdout, _, code := f.run(t, "status", "--json")
	if status := decodeStatus(t, stdout); code != 1 || status.State != contract.StateUnresponsive {
		t.Fatalf("hung status %d %+v", code, status)
	}
	stdout, _, code = f.run(t, "stop", "--json")
	if code != 0 || !strings.Contains(stdout, `"stopped"`) {
		t.Fatalf("SIGTERM fallback %d %s", code, stdout)
	}

	f = newFixture(t, "")
	standIn(t, f, `trap "" TERM; while :; do sleep 0.1; done`)
	stdout, _, code = f.run(t, "stop", "--json", "--timeout", "500ms")
	if code != 0 || !strings.Contains(stdout, `"killed"`) {
		t.Fatalf("SIGKILL fallback %d %s", code, stdout)
	}
}

func TestStopWithoutRecordSignalsNothing(t *testing.T) {
	t.Parallel()
	health := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"ok":true,"sessions":0}`)
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = health.Serve(listener) }()
	defer func() { _ = health.Close() }()
	f := newFixture(t, "")
	content, _ := os.ReadFile(f.config)
	content = bytes.Replace(content, []byte("127.0.0.1:0"), []byte(listener.Addr().String()), 1)
	if err := os.WriteFile(f.config, content, 0600); err != nil {
		t.Fatal(err)
	}
	stdout, _, code := f.run(t, "status", "--json")
	if status := decodeStatus(t, stdout); code != 0 || status.State != contract.StateRunning || status.Record {
		t.Fatalf("recordless status %d %+v", code, status)
	}
	stdout, _, code = f.run(t, "stop", "--json")
	if code != 1 || !strings.Contains(stdout, `"error"`) || !strings.Contains(stdout, "no stoppable instance") {
		t.Fatalf("recordless stop %d %s", code, stdout)
	}
}

// bridgeConn presents a `webmux bridge` subprocess as one connection.
type bridgeConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	once   sync.Once
}

func (c *bridgeConn) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *bridgeConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }
func (c *bridgeConn) Close() error {
	c.once.Do(func() { _ = c.stdin.Close(); _ = c.cmd.Wait() })
	return nil
}
func (c *bridgeConn) LocalAddr() net.Addr              { return &net.UnixAddr{Name: "bridge", Net: "unix"} }
func (c *bridgeConn) RemoteAddr() net.Addr             { return &net.UnixAddr{Name: "bridge", Net: "unix"} }
func (c *bridgeConn) SetDeadline(time.Time) error      { return nil }
func (c *bridgeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *bridgeConn) SetWriteDeadline(time.Time) error { return nil }

type bridgeDialer struct {
	f     *fixture
	mu    sync.Mutex
	conns []*bridgeConn
}

func (d *bridgeDialer) DialContext(context.Context) (net.Conn, error) {
	cmd := d.f.command(nil, "bridge", "--config", d.f.config)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	conn := &bridgeConn{cmd: cmd, stdin: stdin, stdout: stdout}
	d.mu.Lock()
	d.conns = append(d.conns, conn)
	d.mu.Unlock()
	return conn, nil
}

func TestBridgeCarriesHTTPAndWebSocket(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	cmd := f.command(nil, "bridge", "--config", f.config)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if cmd.ProcessState.ExitCode() != contract.ExitNotRunning || stdout.Len() != 0 || !strings.Contains(stderr.String(), "not running") {
		t.Fatalf("bridge without server: %v stdout %q stderr %q", err, stdout.String(), stderr.String())
	}

	f.start(t, nil, true, "--config", f.config)
	dialer := &bridgeDialer{f: f}
	client := localclient.New(dialer)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for range 3 {
		if status, err := client.Status(ctx); err != nil || status.State != contract.StateRunning {
			t.Fatalf("status over bridge: %+v %v", status, err)
		}
	}
	var info contract.SessionInfo
	if err := client.Do(ctx, http.MethodPost, "/api/sessions", map[string]any{"preset": "custom", "cmd": []string{"cat"}}, &info); err != nil {
		t.Fatal(err)
	}
	dialer.mu.Lock()
	requests := len(dialer.conns)
	dialer.mu.Unlock()
	if requests != 1 {
		t.Fatalf("keep-alive: %d bridge processes for 4 requests", requests)
	}
	terminal(t, ctx, client, info.ID)
}

func TestServeLogFileSurvivesClosedPipes(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	logFile := filepath.Join(f.dir, "logs", "webmux.log")
	if err := os.MkdirAll(filepath.Dir(logFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logFile, []byte("previous run\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := f.command(nil, "serve", "--config", f.config, "--log-file", "~/logs/webmux.log")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	// The launcher goes away: nothing reads its pipes any more.
	_ = stdout.Close()
	_ = stderr.Close()
	eventually(t, "record", func() bool {
		record, err := runtime.Read(runtime.Dir(f.state))
		return err == nil && record.PID == cmd.Process.Pid
	})
	if record := f.record(t); record.LogFile != logFile {
		t.Fatalf("record log file %q", record.LogFile)
	}
	// Session creation and exit produce output after the pipes closed.
	client := f.local(t)
	for range 3 {
		if err := client.Do(context.Background(), http.MethodPost, "/api/sessions", map[string]any{"preset": "custom", "cmd": []string{"true"}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, code := f.run(t, "status"); code != 0 {
		t.Fatalf("server died after its pipes closed: %d", code)
	}
	if old, _ := os.ReadFile(logFile + ".1"); string(old) != "previous run\n" {
		t.Fatalf("rotated log %q", old)
	}
	info, err := os.Stat(logFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("log file %v %v", info, err)
	}
	if _, _, code := f.run(t, "stop"); code != 0 {
		t.Fatal("stop failed")
	}
	select {
	case <-exited:
	case <-time.After(20 * time.Second):
		t.Fatal("server did not exit")
	}
	if data, _ := os.ReadFile(logFile); !strings.Contains(string(data), "webmux listening") || !strings.Contains(string(data), "WebMux local:") || strings.Contains(string(data), "previous run") || strings.Contains(string(data), fixtureToken) {
		t.Fatalf("log content %q", data)
	}
}
