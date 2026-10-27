//go:build linux || darwin

package runtime

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordRoundTripPermissionsAndAtomicReplace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	if err := EnsureDir(dir); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0700 {
		t.Fatalf("dir mode %v", info.Mode().Perm())
	}
	first := Record{PID: 10, Identity: Identity{Start: "1"}, Listen: "127.0.0.1:7681", TokenConfigured: true, TokenFingerprint: TokenFingerprint("secret-token")}
	if err := Write(dir, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.PID = 11
	if err := Write(dir, second); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(RecordPath(dir))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("record mode %v %v", info, err)
	}
	data, _ := os.ReadFile(RecordPath(dir))
	if strings.Contains(string(data), "secret-token") {
		t.Fatal("record contains the token")
	}
	got, err := Read(dir)
	if err != nil || got.PID != 11 || got.V != RecordVersion {
		t.Fatalf("read %+v %v", got, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
	Remove(dir, 10) // Not ours: kept.
	if _, err := Read(dir); err != nil {
		t.Fatal("removed another instance's record")
	}
	Remove(dir, 11)
	if _, err := Read(dir); !errors.Is(err, ErrNoRecord) {
		t.Fatalf("after remove: %v", err)
	}
}

func TestReadRejectsUnknownVersionAndCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	for content, want := range map[string]error{
		`{"v":2,"pid":1}`: ErrUnsupportedVersion,
		`{"v":1,"pid":`:   ErrCorrupt,
		`not json`:        ErrCorrupt,
		`{"v":1,"pid":0,"identity":{"start":"1"}}`: ErrCorrupt,
	} {
		if err := os.WriteFile(RecordPath(dir), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(dir); !errors.Is(err, want) {
			t.Errorf("%s: got %v, want %v", content, err, want)
		}
	}
}

func TestLockIsExclusiveWithinAndAcrossProcesses(t *testing.T) {
	if dir := os.Getenv("WEBMUX_TEST_HOLD_LOCK"); dir != "" {
		lock, err := AcquireLock(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Release()
		_, _ = os.Stdout.WriteString("locked\n")
		time.Sleep(time.Minute)
		return
	}
	dir := t.TempDir()
	lock, err := AcquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire in process: %v", err)
	}
	lock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockIsExclusiveWithinAndAcrossProcesses$")
	cmd.Env = append(os.Environ(), "WEBMUX_TEST_HOLD_LOCK="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	buf := make([]byte, 7)
	if _, err := out.Read(buf); err != nil || string(buf) != "locked\n" {
		t.Fatalf("child: %q %v", buf, err)
	}
	if _, err := AcquireLock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("acquire while child holds lock: %v", err)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	lock, err = AcquireLock(dir)
	if err != nil {
		t.Fatalf("lock not released by exited child: %v", err)
	}
	lock.Release()
}

func TestProcessIdentity(t *testing.T) {
	self, err := ProcessIdentity(os.Getpid())
	if err != nil || self.Start == "" {
		t.Fatalf("self: %+v %v", self, err)
	}
	again, _ := ProcessIdentity(os.Getpid())
	if again != self {
		t.Fatalf("identity unstable: %+v %+v", self, again)
	}
	record := Record{PID: os.Getpid(), Identity: self}
	if err := record.Alive(); err != nil {
		t.Fatal(err)
	}
	record.Identity.Start += "0"
	if err := record.Alive(); !errors.Is(err, ErrNoProcess) {
		t.Fatalf("reused PID accepted: %v", err)
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessIdentity(cmd.Process.Pid); !errors.Is(err, ErrNoProcess) {
		t.Fatalf("exited child: %v", err)
	}
}

func TestPeerCredAndListener(t *testing.T) {
	path, err := SocketPath(shortTempDir(t), os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := ListenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("socket mode %v %v", info, err)
	}
	peers := PeerListener(listener)
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := peers.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	select {
	case conn := <-accepted:
		peer := conn.(*PeerConn)
		if peer.PID != os.Getpid() {
			t.Fatalf("peer PID %d, want %d", peer.PID, os.Getpid())
		}
		_ = conn.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("connection not accepted")
	}
}

func TestListenSocketReplacesLeftover(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "run", "webmux.sock")
	first, err := ListenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate SIGKILL: the file stays behind without a listener.
	first.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = first.Close()
	second, err := ListenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

func TestSocketPathRules(t *testing.T) {
	long := "/" + strings.Repeat("d", maxSocketPath)
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	tests := []struct {
		name, state string
		env         map[string]string
		want        string
		err         bool
	}{
		{"default", "/home/u/.local/state/webmux", nil, "/home/u/.local/state/webmux/run/webmux.sock", false},
		{"fallback", long, map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"}, "/run/user/1000/webmux/", false},
		{"no runtime dir", long, nil, "", true},
		{"fallback too long", long, map[string]string{"XDG_RUNTIME_DIR": long}, "", true},
	}
	for _, tt := range tests {
		got, err := SocketPath(tt.state, env(tt.env))
		if tt.err != (err != nil) || !strings.HasPrefix(got, tt.want) {
			t.Errorf("%s: got %q %v", tt.name, got, err)
		}
		if tt.name == "fallback" && (len(got) != len("/run/user/1000/webmux/")+12+len(".sock")) {
			t.Errorf("fallback name %q", got)
		}
	}
	a, _ := SocketPath(long, env(map[string]string{"XDG_RUNTIME_DIR": "/r"}))
	b, _ := SocketPath(long+"x", env(map[string]string{"XDG_RUNTIME_DIR": "/r"}))
	if a == b {
		t.Fatal("fallback does not depend on the state directory")
	}
}

func TestTokenFingerprintAndLauncher(t *testing.T) {
	if TokenFingerprint("") != "" || len(TokenFingerprint("a")) != 16 || TokenFingerprint("a") == TokenFingerprint("b") {
		t.Fatal("fingerprint")
	}
	tests := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"WEBMUX_LAUNCHER": "windows-tray", "INVOCATION_ID": "x"}, "windows-tray"},
		{map[string]string{"WEBMUX_LAUNCHER": "other", "INVOCATION_ID": "x"}, "systemd"},
		{map[string]string{"XPC_SERVICE_NAME": "com.webmux"}, "launchd"},
		{map[string]string{"XPC_SERVICE_NAME": "0"}, "manual"},
		{nil, "manual"},
	}
	for _, tt := range tests {
		if got := Launcher(func(k string) string { return tt.env[k] }); got != tt.want {
			t.Errorf("%v: %s, want %s", tt.env, got, tt.want)
		}
	}
}

// macOS temporary directories exceed the 104-byte socket limit when nested.
func shortTempDir(t *testing.T) string {
	dir, err := os.MkdirTemp("/tmp", "wm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}
