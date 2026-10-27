//go:build linux || darwin

package session

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLeaderExitWithInheritedPTY(t *testing.T) {
	t.Parallel()
	m := NewManager(Options{DefaultCwd: t.TempDir()})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	s, err := m.Create(CreateInput{Cwd: "", Cmd: []string{"/bin/bash", "-c", "trap '' HUP; sleep 30 & printf 'CHILD_STARTED\\n'; exit 4"}, Name: "", Env: nil, ConfigID: ""})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(7 * time.Second):
		t.Fatal("leader exited but retained slave PTY prevented final exit")
	}
	if info := s.Info(); info.ExitCode == nil || *info.ExitCode != 4 {
		t.Fatalf("exit info: %+v", info)
	}
	assertNoSessionProcesses(t, s)
}

func assertNoSessionProcesses(t *testing.T, s *Session) {
	t.Helper()
	for pid, state := range processes() {
		if state.sid == s.cmd.Process.Pid {
			t.Fatalf("session descendant still exists: pid %d state %c", pid, state.state)
		}
	}
}

func TestCloseReapsSeparateJobGroups(t *testing.T) {
	t.Parallel()
	m := NewManager(Options{DefaultCwd: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancel()
	defer m.Shutdown(ctx)
	s, err := m.Create(CreateInput{Cwd: "", Cmd: []string{"/bin/bash", "-c", `set -m; (trap '' HUP; printf 'CHILD_READY\n'; exec sleep 30) & wait`}, Name: "", Env: nil, ConfigID: ""})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(string(s.Buffer()), "CHILD_READY") {
		if time.Now().After(deadline) {
			t.Fatal("child never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Close()
	select {
	case <-s.Done():
	case <-ctx.Done():
		t.Fatal("close failed to terminate child job")
	}
	assertNoSessionProcesses(t, s)
}

func TestCancelledPTYWriteDoesNotHoldInputGate(t *testing.T) {
	t.Parallel()
	m := NewManager(Options{DefaultCwd: t.TempDir()})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	s, err := m.Create(CreateInput{Cwd: "", Cmd: []string{"/bin/bash", "-c", `stty raw -echo; printf READY; sleep 30`}, Name: "", Env: nil, ConfigID: ""})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(string(s.Buffer()), "READY") {
		if time.Now().After(deadline) {
			t.Fatal("terminal never entered raw mode")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = s.Resize(100, 40); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	start := time.Now()
	err = s.WriteContext(ctx, bytes.Repeat([]byte("x"), 512*1024))
	if !errors.Is(err, context.Canceled) || time.Since(start) > 2*time.Second {
		t.Fatalf("uncancelled write: %v", err)
	}
	next, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	start = time.Now()
	_ = s.WriteContext(next, []byte("y"))
	if time.Since(start) > time.Second {
		t.Fatal("cancelled writer retained serialization gate")
	}
}

func waitForOutput(t *testing.T, s *Session, text string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(string(s.Buffer()), text) {
		if time.Now().After(deadline) {
			t.Fatalf("output %q never appeared: %q", text, s.Buffer())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestUnattendedSleepIsNotUserWait(t *testing.T) {
	m := NewManager(Options{DefaultCwd: t.TempDir()})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	sleeper, err := m.Create(CreateInput{Cwd: "", Cmd: []string{"/bin/bash", "--noprofile", "--norc", "-c", "printf 'background timer\\n'; sleep 60"}, Name: "", Env: nil, ConfigID: ""})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := m.Create(CreateInput{Cwd: "", Cmd: []string{"/bin/bash", "--noprofile", "--norc", "-c", "printf READY; read -r value"}, Name: "", Env: nil, ConfigID: ""})
	if err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, sleeper, "background timer")
	waitForOutput(t, reader, "READY")
	deadline := time.Now().Add(3 * time.Second)
	var table processTable
	var snapshot processSnapshot
	for {
		table = processes()
		snapshot = newProcessSnapshot(table)
		sleepQuiet := snapshot.foregroundBlocked(sleeper.cmd.Process.Pid) && !snapshot.awaitingInput(sleeper.cmd.Process.Pid)
		if sleepQuiet && snapshot.awaitingInput(reader.cmd.Process.Pid) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sleep/read wait not distinguished: sleep pid %d blocked=%t awaiting=%t %+v children=%v, read pid %d awaiting=%t %+v", sleeper.cmd.Process.Pid, snapshot.foregroundBlocked(sleeper.cmd.Process.Pid), snapshot.awaitingInput(sleeper.cmd.Process.Pid), table[sleeper.cmd.Process.Pid], directChildren(sleeper.cmd.Process.Pid, table), reader.cmd.Process.Pid, snapshot.awaitingInput(reader.cmd.Process.Pid), table[reader.cmd.Process.Pid])
		}
		time.Sleep(20 * time.Millisecond)
	}
	now := time.Now()
	quiet := &Session{info: Info{LastActiveAt: now.Add(-4 * time.Second)}, cmd: sleeper.cmd}
	quiet.evaluate(now, snapshot, 30*time.Second, nil)
	if quiet.info.Status != StatusRunning {
		t.Fatalf("unattended sleep status = %s, want running", quiet.info.Status)
	}
	blocked := &Session{info: Info{LastActiveAt: now.Add(-4 * time.Second)}, cmd: reader.cmd}
	blocked.evaluate(now, snapshot, 30*time.Second, nil)
	if blocked.info.Status != StatusWaiting {
		t.Fatalf("terminal read status = %s, want waiting", blocked.info.Status)
	}
}

func directChildren(pid int, table processTable) map[int]processState {
	kids := map[int]processState{}
	for child, state := range table {
		if state.ppid == pid {
			kids[child] = state
		}
	}
	return kids
}

func TestRealProcessTreeWaiting(t *testing.T) {
	t.Parallel()
	m := NewManager(Options{DefaultCwd: t.TempDir()})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	reader, err := m.Create(CreateInput{Cwd: "", Cmd: []string{"/bin/bash", "-c", `printf READY; read -r value`}, Name: "", Env: nil, ConfigID: ""})
	if err != nil {
		t.Fatal(err)
	}
	busy, err := m.Create(CreateInput{Cwd: "", Cmd: []string{"/bin/bash", "-c", `printf READY; while :; do :; done`}, Name: "", Env: nil, ConfigID: ""})
	if err != nil {
		t.Fatal(err)
	}
	waitForOutput(t, reader, "READY")
	waitForOutput(t, busy, "READY")
	deadline := time.Now().Add(2 * time.Second)
	for !newProcessSnapshot(processes()).foregroundBlocked(reader.cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatal("shell blocked on terminal input was not detected as waiting")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for range 5 {
		if newProcessSnapshot(processes()).foregroundBlocked(busy.cmd.Process.Pid) {
			t.Fatal("busy loop was classified as waiting")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
