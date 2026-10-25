package session

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestPersistentPTY(t *testing.T) {
	t.Parallel()
	m := NewManager(Options{DefaultCwd: t.TempDir(), Presets: []Preset{{Name: "shell", Cmd: []string{"/bin/bash", "--noprofile", "--norc", "-i"}}}})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	s, err := m.Create("", "shell", nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	sub := s.Attach()
	moved := false
	deadline := time.After(5 * time.Second)
	if err = s.Write([]byte("printf '\\033]2;agent title\\007'; printf 'consumer-marker\\n'; exit 7\n")); err != nil {
		t.Fatal(err)
	}
	for !moved {
		select {
		case frame := <-sub.Out:
			if frame.Exit != nil {
				moved = true
			}
		case <-deadline:
			t.Fatal("PTY failed to exit")
		}
	}
	s.Detach(sub)
	if got := s.Info(); got.Title != "agent title" || got.ExitCode == nil || *got.ExitCode != 7 {
		t.Fatalf("info: %+v", got)
	}
	again := s.Attach()
	defer s.Detach(again)
	if control := <-again.Out; control.Control == nil {
		t.Fatalf("control before replay: %+v", control)
	}
	if position := <-again.Out; position.Position == nil {
		t.Fatalf("position: %+v", position)
	}
	snapshot := (<-again.Out).Frame
	if len(snapshot) == 0 || snapshot[0] != 1 || !strings.Contains(string(snapshot[1:]), "consumer-marker") {
		t.Fatalf("replay: %q", snapshot)
	}
	if err = m.Remove(s.Info().ID); err != nil {
		t.Fatal(err)
	}
}

func TestReportedCwdTracksShellSignals(t *testing.T) {
	s := &Session{info: Info{Cwd: "/work"}}
	handler := s.signalHandler()
	handler.Cwd("/work/sub", "")
	first := s.info.ReportedCwd
	if first == nil || first.Path != "/work/sub" || first.Remote || first.Since.IsZero() {
		t.Fatalf("local report: %+v", first)
	}
	handler.Cwd("/work/sub", "")
	if s.info.ReportedCwd != first {
		t.Fatal("a repeated report replaced the existing value")
	}
	handler.Cwd("/srv/remote", "remote.example")
	if got := s.info.ReportedCwd; got == nil || got.Path != "/srv/remote" || !got.Remote {
		t.Fatalf("remote report: %+v", got)
	}
}

func TestReportedCwdClearsWhenSessionExits(t *testing.T) {
	t.Parallel()
	s := newIdleSession(t, "/bin/sh", "-c", "printf '\\033]7;file:///tmp\\007'; read _")
	until := time.Now().Add(5 * time.Second)
	for s.Info().ReportedCwd == nil {
		if time.Now().After(until) {
			t.Fatal("session did not report its directory")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := s.Write([]byte("leave\n")); err != nil {
		t.Fatal(err)
	}
	until = time.Now().Add(5 * time.Second)
	for s.Info().ExitCode == nil {
		if time.Now().After(until) {
			t.Fatal("session did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := s.Info().ReportedCwd; got != nil {
		t.Fatalf("reported directory survived exit: %+v", got)
	}
}

func TestResizeFromRequiresTheControllingConnection(t *testing.T) {
	t.Parallel()
	m := NewManager(Options{DefaultCwd: t.TempDir(), Presets: []Preset{{Name: "idle", Cmd: []string{"/bin/sleep", "30"}}}})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	s, err := m.Create("", "idle", nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	first := s.AttachClient(AttachOptions{Client: "first-client"})
	defer s.Detach(first)
	second := s.AttachClient(AttachOptions{Client: "second-client"})
	defer s.Detach(second)
	if err = s.ResizeFrom(second, 100, 40); err != ErrNotController {
		t.Fatalf("observer resize: %v", err)
	}
	// Claims and resizes race; every accepted resize must come from the
	// connection that controlled at that moment, never a stale one.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			s.Claim(second)
			s.Claim(first)
		}
	}()
	for i := 0; i < 200; i++ {
		_ = s.ResizeFrom(first, 90, 30)
		_ = s.ResizeFrom(second, 60, 20)
	}
	<-done
	s.Claim(first)
	if err = s.ResizeFrom(first, 100, 40); err != nil {
		t.Fatal(err)
	}
	if info := s.Info(); info.Cols != 100 || info.Rows != 40 {
		t.Fatalf("controller resize: %dx%d", info.Cols, info.Rows)
	}
}

func newIdleSession(t *testing.T, cmd ...string) *Session {
	t.Helper()
	m := NewManager(Options{DefaultCwd: t.TempDir(), Presets: []Preset{{Name: "idle", Cmd: cmd}}})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	s, err := m.Create("", "idle", nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// kindOf is the protocol type byte the hub writes for m.
func kindOf(m Message) byte {
	switch {
	case m.Frame != nil:
		return m.Frame[0]
	case m.Info != nil:
		return 2
	case m.Exit != nil:
		return 3
	case m.Position != nil:
		return 4
	case m.Submission != nil:
		return 5
	case m.Control != nil:
		return 6
	}
	panic("empty message")
}

func queuedKinds(sub *Subscriber) []byte {
	var kinds []byte
	for len(sub.Out) > 0 {
		kinds = append(kinds, kindOf(<-sub.Out))
	}
	return kinds
}

func TestSameClientConnectionGainingAuthorityResendsItsGrid(t *testing.T) {
	t.Parallel()
	s := newIdleSession(t, "/bin/sleep", "30")
	older := s.AttachClient(AttachOptions{Client: "shared-client"})
	defer s.Detach(older)
	newer := s.AttachClient(AttachOptions{Client: "shared-client"})
	queuedKinds(older)
	// Its resizes were refused while newer was authoritative, so gaining
	// authority must prompt it with a control frame to send its grid again.
	s.Claim(older)
	if kinds := queuedKinds(older); !bytes.Contains(kinds, []byte{6}) {
		t.Fatalf("claim by same client sent %v", kinds)
	}
	s.Claim(newer)
	queuedKinds(older)
	s.Detach(newer)
	if kinds := queuedKinds(older); !bytes.Contains(kinds, []byte{6}) {
		t.Fatalf("promotion after detach sent %v", kinds)
	}
	if err := s.ResizeFrom(older, 70, 20); err != nil {
		t.Fatal(err)
	}
}

func TestReplayOfExitedSessionDeliversStatusBeforeExit(t *testing.T) {
	t.Parallel()
	s := newIdleSession(t, "/usr/bin/true")
	until := time.Now().Add(5 * time.Second)
	for s.Info().ExitCode == nil {
		if time.Now().After(until) {
			t.Fatal("session did not exit")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sub := s.AttachClient(AttachOptions{Client: "replay-client"})
	defer s.Detach(sub)
	s.SendSubmissionStatus(sub, SubmissionStatus{ID: "submission-0001", State: SubmissionWritten})
	s.Replay(sub)
	// The hub closes the socket after the exit frame; nothing may follow it.
	kinds := queuedKinds(sub)
	if len(kinds) < 2 || kinds[len(kinds)-1] != 3 || kinds[len(kinds)-2] != 5 {
		t.Fatalf("frames after replay: %v", kinds)
	}
}

func TestPendingSubmissionForgetsDisconnectedWatchers(t *testing.T) {
	t.Parallel()
	s := newIdleSession(t, "/bin/sleep", "30")
	s.mu.Lock()
	record := s.submissions.add("submission-0002", "original-tab", time.Now())
	s.mu.Unlock()
	first := s.AttachClient(AttachOptions{Client: "restored-tab"})
	second := s.AttachClient(AttachOptions{Client: "restored-tab"})
	s.QuerySubmissions(first, []string{"submission-0002"})
	s.Detach(first)
	s.mu.Lock()
	_, kept := record.watchers["restored-tab"]
	s.mu.Unlock()
	if !kept {
		t.Fatal("watcher dropped while the client is still connected")
	}
	s.Detach(second)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(record.watchers) != 0 {
		t.Fatalf("watchers of disconnected clients: %v", record.watchers)
	}
}
