package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// ptyRun starts a client subcommand on a pseudo-terminal, as a user would.
type ptyRun struct {
	t    *testing.T
	tty  *os.File
	mu   sync.Mutex
	out  bytes.Buffer
	done chan int
}

func (f *fixture) ptyStart(t *testing.T, args ...string) *ptyRun {
	t.Helper()
	cmd := f.command(nil, append(args[:1:1], append([]string{"--config", f.config}, args[1:]...)...)...)
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	r := &ptyRun{t: t, tty: tty, done: make(chan int, 1)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			r.mu.Lock()
			r.out.Write(buf[:n])
			r.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	go func() {
		_ = cmd.Wait()
		r.done <- cmd.ProcessState.ExitCode()
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = tty.Close() })
	return r
}

// session runs a session subcommand with --config before its arguments.
func (f *fixture) session(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := f.command(nil, append([]string{args[0], "--config", f.config}, args[1:]...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	_ = cmd.Run()
	return stdout.String(), stderr.String(), cmd.ProcessState.ExitCode()
}

func (r *ptyRun) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.String()
}

func (r *ptyRun) wait() int {
	r.t.Helper()
	select {
	case code := <-r.done:
		return code
	case <-time.After(15 * time.Second):
		r.t.Fatalf("client did not exit; output %q", r.output())
		return -1
	}
}

func TestRunAttachDetachAndExitCode(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	f.start(t, nil, true, "--config", f.config)

	run := f.ptyStart(t, "run", "--env", "GREETING=hi there", "--", "sh", "-c", `read x; stty size; echo "$GREETING:$x"; exit 7`)
	// The session starts before the client attaches; input arrives after
	// the attachment took control and sent its size.
	time.Sleep(time.Second)
	_, _ = io.WriteString(run.tty, "answer\r")
	if code := run.wait(); code != 7 {
		t.Fatalf("exit code %d; output %q", code, run.output())
	}
	if !strings.Contains(run.output(), "30 100") || !strings.Contains(run.output(), "hi there:answer") || !strings.Contains(run.output(), "webmux: session ") || !strings.Contains(run.output(), "Ctrl+] to detach") {
		t.Fatalf("output %q", run.output())
	}

	stdout, _, code := f.session(t, "run", "--detach", "--name", "long", "--", "cat")
	id, link, _ := strings.Cut(strings.TrimSpace(stdout), "\t")
	if code != 0 || !strings.HasPrefix(link, "http://127.0.0.1:") || !strings.HasSuffix(link, "/s/"+id) {
		t.Fatalf("detached run %d %q", code, stdout)
	}
	attached := f.ptyStart(t, "attach", id)
	_, _ = io.WriteString(attached.tty, "echo-me\r")
	eventually(t, "cat output", func() bool { return strings.Count(attached.output(), "echo-me") >= 2 })
	_, _ = attached.tty.Write([]byte{0x1d})
	if code := attached.wait(); code != 0 || !strings.Contains(attached.output(), "detached from "+id) {
		t.Fatalf("detach %d %q", code, attached.output())
	}
	stdout, _, _ = f.session(t, "ls", "--json")
	var list []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.ID == id && s.Status == "exited" {
			t.Fatal("detaching ended the session")
		}
	}
	if stdout, _, code := f.session(t, "open", id); code != 0 || strings.TrimSpace(stdout) != link {
		t.Fatalf("open %d %q", code, stdout)
	}
	if _, stderr, code := f.session(t, "attach", id); code != 2 || !strings.Contains(stderr, "terminal") {
		t.Fatalf("attach without a terminal %d %q", code, stderr)
	}
}

func TestSessionCommandsWithoutServer(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	// Without a terminal only detached runs get as far as the server.
	for _, args := range [][]string{{"ls"}, {"run", "--detach", "--", "true"}, {"open", "abcdefgh"}} {
		if _, stderr, code := f.session(t, args...); code != 3 || !strings.Contains(stderr, "not running") {
			t.Errorf("%v: %d %q", args, code, stderr)
		}
	}
}

func TestAttachedRunWithoutServerOnTerminal(t *testing.T) {
	t.Parallel()
	f := newFixture(t, "")
	run := f.ptyStart(t, "run", "--", "claude")
	if code := run.wait(); code != 3 || !strings.Contains(run.output(), "not running") {
		t.Fatalf("exit %d output %q", code, run.output())
	}
}
