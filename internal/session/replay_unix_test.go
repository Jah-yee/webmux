//go:build linux || darwin

package session

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func TestTruncatedObserverReplayRepaintsForegroundAtOriginalSize(t *testing.T) {
	if os.Getenv("WEBMUX_TEST_REPAINT_HELPER") == "1" {
		runForegroundRepaintHelper(t)
		return
	}
	t.Parallel()
	// Job control puts the helper in a foreground group distinct from the
	// session leader. A Go signal handler reacts while terminal input blocks;
	// Bash can defer its WINCH trap indefinitely inside a blocked read.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := `set -m; trap 'printf "WRONG_LEADER\n"' WINCH; "$@"`
	m := NewManager(Options{ScrollbackBytes: 128, DefaultCwd: t.TempDir()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	s, err := m.Create("", []string{"/bin/bash", "--noprofile", "--norc", "-c", script, "repaint", executable, "-test.run=^TestTruncatedObserverReplayRepaintsForegroundAtOriginalSize$"}, "", map[string]string{"WEBMUX_TEST_REPAINT_HELPER": "1"})
	if err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, s, "READY")
	group, err := unix.IoctlGetInt(int(s.terminal.Fd()), unix.TIOCGPGRP)
	if err != nil || group <= 0 || group == s.cmd.Process.Pid {
		t.Fatalf("foreground job group: %d, leader %d, error %v", group, s.cmd.Process.Pid, err)
	}
	initial, err := pty.GetsizeFull(s.terminal)
	if err != nil {
		t.Fatal(err)
	}
	info := s.Info()
	_, end := s.ring.Range()
	controller := s.AttachClient(AttachOptions{Client: "controller", Resume: &ResumePoint{Generation: info.Generation, Offset: end}})
	defer s.Detach(controller)
	queuedKinds(controller)
	s.mu.Lock()
	controlBefore := *s.control
	s.mu.Unlock()

	observer := s.AttachClient(AttachOptions{Client: "observer"})
	defer s.Detach(observer)
	assertEmptyReplay := func() {
		t.Helper()
		if control := <-observer.Out; control.Control == nil || control.Control.Controller == nil || control.Control.Controller.ClientID != "controller" {
			t.Fatalf("observer changed control: %+v", control)
		}
		position := (<-observer.Out).Position
		if position == nil || position.Mode != "reset" || !position.Truncated || position.Start != position.End {
			t.Fatalf("observer reset position: %+v", position)
		}
		if snapshot := (<-observer.Out).Frame; !bytes.Equal(snapshot, []byte{1}) {
			t.Fatalf("observer received raw history: %q", snapshot)
		}
		if state := <-observer.Out; state.Info == nil {
			t.Fatalf("state did not precede redraw: %+v", state)
		}
	}
	var pendingOutput string
	waitForRedraw := func(cols uint16) {
		t.Helper()
		want := fmt.Sprintf("REDRAW:%d %d", initial.Rows, cols)
		deadline := time.After(5 * time.Second)
		for !strings.Contains(pendingOutput, want) {
			select {
			case message := <-observer.Out:
				if message.Exit != nil {
					t.Fatalf("foreground program exited during redraw: %d", *message.Exit)
				}
				if message.Control != nil {
					t.Fatalf("transient recovery grid broadcast: %+v", message.Control)
				}
				if len(message.Frame) > 0 && message.Frame[0] == 0 {
					pendingOutput += string(message.Frame[1:])
				}
			case <-deadline:
				t.Fatalf("foreground redraw %q missing; received %q", want, pendingOutput)
			}
		}
		if strings.Contains(pendingOutput, "WRONG_LEADER") || strings.Contains(pendingOutput, "UNEXPECTED_INPUT") {
			t.Fatalf("redraw touched leader or injected input: %q", pendingOutput)
		}
		_, pendingOutput, _ = strings.Cut(pendingOutput, want)
	}
	assertEmptyReplay()
	// A controller's redundant attach resize must not immediately undo the
	// pulse: real applications can coalesce that into no size change at all.
	if err := s.ResizeFrom(controller, info.Cols, info.Rows); err != nil {
		t.Fatal(err)
	}
	waitForRedraw(initial.Cols - 1)
	waitForRedraw(initial.Cols)
	// Explicit recovery by the observer must also repaint at the original
	// size without requiring a control claim or changing the logical grid.
	s.Replay(observer)
	assertEmptyReplay()
	waitForRedraw(initial.Cols - 1)
	waitForRedraw(initial.Cols)
	final, err := pty.GetsizeFull(s.terminal)
	if err != nil || *final != *initial {
		t.Fatalf("PTY size changed: before %+v, after %+v, error %v", initial, final, err)
	}
	if got := s.Info(); got.Cols != info.Cols || got.Rows != info.Rows {
		t.Fatalf("session grid changed: %dx%d to %dx%d", info.Cols, info.Rows, got.Cols, got.Rows)
	}
	s.mu.Lock()
	unchanged := s.controlSub == controller && *s.control == controlBefore
	s.mu.Unlock()
	if !unchanged {
		t.Fatal("observer recovery transferred control")
	}
}

func runForegroundRepaintHelper(t *testing.T) {
	t.Helper()
	winch := make(chan os.Signal, 8)
	signal.Notify(winch, unix.SIGWINCH)
	defer signal.Stop(winch)
	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Restore(int(os.Stdin.Fd()), state)
	input := make(chan struct{}, 1)
	go func() {
		var b [1]byte
		if n, _ := os.Stdin.Read(b[:]); n > 0 {
			input <- struct{}{}
		}
	}()
	fmt.Printf("%0512dREADY\n", 0)
	for {
		select {
		case <-winch:
			size, err := pty.GetsizeFull(os.Stdin)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Printf("\x1b[2J\x1b[HREDRAW:%d %d\n", size.Rows, size.Cols)
		case <-input:
			fmt.Print("UNEXPECTED_INPUT\n")
		}
	}
}

func TestRepaintCoalescesAndRealResizeCancelsRestore(t *testing.T) {
	t.Parallel()
	s := newIdleSession(t, "/bin/sleep", "30")
	s.mu.Lock()
	s.repaintLocked()
	timer := s.repaintTimer
	s.repaintLocked()
	coalesced := timer != nil && s.repaintTimer == timer && s.preview.screen.Cols() == 79
	s.mu.Unlock()
	if !coalesced {
		t.Fatal("pending redraw request was not coalesced on the temporary grid")
	}
	if err := s.Resize(91, 31); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	size, err := pty.GetsizeFull(s.terminal)
	if err != nil || size.Cols != 91 || size.Rows != 31 {
		t.Fatalf("recovery overwrote controller resize: %+v, error %v", size, err)
	}
	s.mu.Lock()
	consistent := s.repaintTimer == nil && s.info.Cols == 91 && s.info.Rows == 31 && s.preview.screen.Cols() == 91 && s.preview.screen.Rows() == 31
	s.mu.Unlock()
	if !consistent {
		t.Fatal("logical grid and preview did not follow the controller resize")
	}
}

func TestRepaintSkipsUnavailablePTY(t *testing.T) {
	t.Parallel()
	terminal, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()
	code := 0
	process := &exec.Cmd{Process: &os.Process{Pid: os.Getpid()}}
	for _, s := range []*Session{
		{},
		{terminal: terminal},
		{terminal: terminal, cmd: &exec.Cmd{}},
		{cmd: process, info: Info{Cols: 80, Rows: 24}},
		{terminal: terminal, cmd: process, info: Info{Cols: 80, Rows: 24}},
		{terminal: terminal, cmd: process, info: Info{Cols: 80, Rows: 24, ExitCode: &code}},
	} {
		s.repaintLocked()
		if s.repaintTimer != nil {
			s.repaintTimer.Stop()
			t.Fatal("unavailable or exited PTY scheduled a redraw")
		}
	}
}
