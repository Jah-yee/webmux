package session

import (
	"bytes"
	"testing"

	"github.com/0magnet/xterm-go/vt"
)

func TestTruncatedResetDiscardsRawSuffix(t *testing.T) {
	t.Parallel()
	output := []byte("lost history\x1b[31mdo-not-replay\x1b]0;partial")
	for _, test := range []struct {
		name   string
		resume *ResumePoint
	}{
		{name: "fresh"},
		{name: "expired", resume: &ResumePoint{Generation: "generation", Offset: 0}},
		{name: "future", resume: &ResumePoint{Generation: "generation", Offset: uint64(len(output) + 1)}},
		{name: "other generation", resume: &ResumePoint{Generation: "old", Offset: uint64(len(output))}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Session{ring: NewRingBuffer(16), info: Info{Generation: "generation"}, subs: make(map[*Subscriber]struct{})}
			s.ring.Write(output)
			sub := s.AttachClient(AttachOptions{Client: "observer", Resume: test.resume})
			if control := <-sub.Out; control.Control == nil {
				t.Fatalf("control before reset: %+v", control)
			}
			position := (<-sub.Out).Position
			if position == nil || position.Generation != "generation" || position.Mode != "reset" || !position.Truncated || position.Start != uint64(len(output)) || position.End != position.Start {
				t.Fatalf("truncated reset position: %+v", position)
			}
			if frame := (<-sub.Out).Frame; !bytes.Equal(frame, []byte{1}) {
				t.Fatalf("unsafe raw suffix replayed: %q", frame)
			}
			if state := <-sub.Out; state.Info == nil || len(sub.Out) != 0 {
				t.Fatalf("state after empty reset: %+v, %d extra frames", state, len(sub.Out))
			}
			if got := s.Buffer(); !bytes.Equal(got, output[len(output)-16:]) {
				t.Fatalf("diagnostic buffer changed: %q", got)
			}
		})
	}
}

func TestResumeAfterRingWrapKeepsExactTail(t *testing.T) {
	t.Parallel()
	output := []byte("discarded\x1b[31mexact tail\x1b[0m")
	for _, offset := range []uint64{uint64(len(output) - 16), uint64(len(output) - 5), uint64(len(output))} {
		s := &Session{ring: NewRingBuffer(16), info: Info{Generation: "generation"}, subs: make(map[*Subscriber]struct{})}
		s.ring.Write(output)
		sub := s.AttachClient(AttachOptions{Client: "returning-client", Resume: &ResumePoint{Generation: "generation", Offset: offset}})
		if control := <-sub.Out; control.Control == nil {
			t.Fatalf("control before append: %+v", control)
		}
		position := (<-sub.Out).Position
		if position == nil || position.Mode != "append" || position.Start != offset || position.End != uint64(len(output)) || !position.Truncated {
			t.Fatalf("resume at %d: %+v", offset, position)
		}
		if offset < uint64(len(output)) {
			want := append([]byte{0}, output[offset:]...)
			if frame := (<-sub.Out).Frame; !bytes.Equal(frame, want) {
				t.Fatalf("resume at %d: %q, want %q", offset, frame, want)
			}
		}
		if state := <-sub.Out; state.Info == nil || len(sub.Out) != 0 {
			t.Fatalf("state after append at %d: %+v, %d extra frames", offset, state, len(sub.Out))
		}
	}
}

func TestUntruncatedResetKeepsCompleteHistory(t *testing.T) {
	t.Parallel()
	output := []byte("\x1b[31mcomplete\x1b[0m")
	s := &Session{ring: NewRingBuffer(64), info: Info{Generation: "generation"}, subs: make(map[*Subscriber]struct{})}
	s.ring.Write(output)
	sub := s.Attach()
	<-sub.Out // Control precedes position.
	position := (<-sub.Out).Position
	if position == nil || position.Mode != "reset" || position.Truncated || position.Start != 0 || position.End != uint64(len(output)) {
		t.Fatalf("complete reset position: %+v", position)
	}
	if frame := (<-sub.Out).Frame; !bytes.Equal(frame, append([]byte{1}, output...)) {
		t.Fatalf("complete history changed: %q", frame)
	}
}

func TestTruncatedExitedReplayKeepsStatusBeforeExit(t *testing.T) {
	t.Parallel()
	code := 7
	s := &Session{ring: NewRingBuffer(4), info: Info{Generation: "generation", Status: StatusExited, ExitCode: &code}, subs: make(map[*Subscriber]struct{})}
	s.ring.Write([]byte("old output"))
	sub := s.Attach()
	s.SendSubmissionStatus(sub, SubmissionStatus{ID: "submission-0001", State: SubmissionWritten})
	s.Replay(sub)
	<-sub.Out // Control precedes position.
	position := (<-sub.Out).Position
	if position == nil || position.Mode != "reset" || !position.Truncated || position.Start != 10 || position.End != 10 {
		t.Fatalf("exited reset position: %+v", position)
	}
	if frame := (<-sub.Out).Frame; !bytes.Equal(frame, []byte{1}) {
		t.Fatalf("exited replay exposed old output: %q", frame)
	}
	if kinds := queuedKinds(sub); !bytes.Equal(kinds, []byte{2, 5, 3}) {
		t.Fatalf("state/status/exit ordering: %v", kinds)
	}
}

func TestTruncatedResetPreservesModesWithoutScreenText(t *testing.T) {
	t.Parallel()
	output := []byte("normal history\x1b[?1049h\x1b[?1;66;2004;1004;6;45;1003;1006h\x1b[?7;25l\x1b[4hdo-not-replay")
	s := &Session{ring: NewRingBuffer(16), info: Info{Generation: "generation"}, subs: make(map[*Subscriber]struct{})}
	s.ring.Write(output)
	s.preview.Feed(output)
	sub := s.Attach()
	<-sub.Out // Control precedes position.
	position := (<-sub.Out).Position
	if position == nil || position.Start != uint64(len(output)) || position.End != position.Start {
		t.Fatalf("mode-only snapshot position: %+v", position)
	}
	frame := (<-sub.Out).Frame
	if len(frame) < 2 || frame[0] != 1 || bytes.Contains(frame, []byte("do-not-replay")) {
		t.Fatalf("mode-only snapshot: %q", frame)
	}
	restored := vt.NewTerminal(vt.NewOptions())
	restored.Write(frame[1:])
	core := restored.CoreService()
	modes := core.DecPrivateModes
	if restored.Buffer() != restored.Buffers().Alt() || !core.Modes.InsertMode || !modes.ApplicationCursorKeys || !modes.ApplicationKeypad || !modes.BracketedPasteMode || !modes.SendFocus || !modes.Origin || !modes.ReverseWraparound || modes.Wraparound || !core.IsCursorHidden {
		t.Fatalf("persistent modes lost: core %+v, modes %+v", core, modes)
	}
	if mouse := restored.MouseService(); mouse.ActiveProtocol() != "ANY" || mouse.ActiveEncoding() != "SGR" {
		t.Fatalf("mouse modes lost: %s/%s", mouse.ActiveProtocol(), mouse.ActiveEncoding())
	}
	preview := Preview{screen: restored}
	if text := preview.snapshot(); text != "" {
		t.Fatalf("retained screen text replayed: %q", text)
	}
	restored.Write([]byte("\x1b[?1049l"))
	if restored.Buffer() != restored.Buffers().Normal() || preview.snapshot() != "" {
		t.Fatal("alternate-buffer exit did not return to an empty normal buffer")
	}
}
