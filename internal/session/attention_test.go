package session

import (
	"context"
	"errors"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/attentionkind"
)

func attentionSession() *Session {
	s := &Session{bellAttention: true}
	s.signals = s.signalHandler()
	return s
}

func feedSignals(s *Session, output string) {
	s.osc.Feed([]byte(output), s.signals)
}

func agentEventOutput(event string) string {
	return "\x1b]777;notify;warp://cli-agent;{\"v\":1,\"agent\":\"omp\",\"event\":\"" + event + "\",\"response\":\"secret\"}\a"
}

func TestAttentionPriority(t *testing.T) {
	s := attentionSession()
	start := time.Now()
	s.raiseAttentionLocked(attentionkind.Done, SourceAgent, start)
	s.raiseAttentionLocked(attentionkind.Bell, SourceTerminal, start.Add(time.Second))
	if a := s.info.Attention; a.Kind != attentionkind.Done || !a.Since.Equal(start) {
		t.Fatalf("weaker bell changed attention: %+v", a)
	}
	s.raiseAttentionLocked(attentionkind.Done, SourceAgent, start.Add(2*time.Second))
	if a := s.info.Attention; !a.Since.Equal(start) {
		t.Fatalf("same kind refreshed since: %+v", a)
	}
	later := start.Add(3 * time.Second)
	s.raiseAttentionLocked(attentionkind.Permission, SourceAgent, later)
	if a := s.info.Attention; a.Kind != attentionkind.Permission || !a.Since.Equal(later) {
		t.Fatalf("stronger signal did not replace: %+v", a)
	}
	s.raiseAttentionLocked(attentionkind.Question, SourceAgent, later.Add(time.Second))
	if a := s.info.Attention; a.Kind != attentionkind.Question {
		t.Fatalf("equal-rank signal did not replace: %+v", a)
	}
	s.raiseAttentionLocked(attentionkind.Error, SourceAgent, later.Add(2*time.Second))
	if a := s.info.Attention; a.Kind != attentionkind.Question {
		t.Fatalf("error downgraded question: %+v", a)
	}
}

func TestAgentEvents(t *testing.T) {
	for _, row := range []struct {
		events []string
		want   attentionkind.Kind
	}{
		{[]string{"question_asked"}, attentionkind.Question},
		{[]string{"permission_request"}, attentionkind.Permission},
		{[]string{"stop"}, attentionkind.Done},
		{[]string{"idle_prompt"}, attentionkind.Done},
		{[]string{"stop_failure"}, attentionkind.Error},
		{[]string{"question_asked", "tool_complete"}, attentionkind.Question},
		{[]string{"permission_request", "something_new"}, attentionkind.Permission},
		{[]string{"question_asked", "prompt_submit"}, ""},
		{[]string{"permission_request", "permission_replied"}, ""},
		{[]string{"stop", "session_start"}, ""},
	} {
		s := attentionSession()
		for _, event := range row.events {
			feedSignals(s, agentEventOutput(event))
		}
		got := attentionkind.Kind("")
		if s.info.Attention != nil {
			got = s.info.Attention.Kind
			if s.info.Attention.Source != SourceAgent {
				t.Fatalf("%v: source %s", row.events, s.info.Attention.Source)
			}
		}
		if got != row.want {
			t.Fatalf("%v: got %q, want %q", row.events, got, row.want)
		}
	}
}

func TestTerminalNotifications(t *testing.T) {
	s := attentionSession()
	feedSignals(s, "\x1b]9;Waiting for input\a")
	if a := s.info.Attention; a == nil || a.Kind != attentionkind.Notify || a.Source != SourceTerminal {
		t.Fatalf("osc 9: %+v", a)
	}
	s.info.Attention = nil
	feedSignals(s, "\x1b]777;notify;omp;Complete\a")
	if a := s.info.Attention; a == nil || a.Kind != attentionkind.Notify {
		t.Fatalf("osc 777: %+v", a)
	}
}

func TestBellQuietPeriodAndSwitch(t *testing.T) {
	s := attentionSession()
	now := time.Now()
	s.userInputLocked([]byte("\t"), now)
	s.bellLocked(now.Add(200 * time.Millisecond))
	if s.info.Attention != nil {
		t.Fatalf("completion bell raised attention: %+v", s.info.Attention)
	}
	s.bellLocked(now.Add(1500 * time.Millisecond))
	if a := s.info.Attention; a == nil || a.Kind != attentionkind.Bell || a.Source != SourceTerminal {
		t.Fatalf("idle bell: %+v", a)
	}
	off := attentionSession()
	off.bellAttention = false
	off.bellLocked(now)
	if off.info.Attention != nil {
		t.Fatal("disabled bell raised attention")
	}
}

func TestTerminalReplyOnly(t *testing.T) {
	for _, row := range []struct {
		input string
		reply bool
	}{
		{"", true},
		{"\x1b[?62;22c", true},
		{"\x1b[>0;276;0c", true},
		{"\x1b[12;40R", true},
		{"\x1b[0n", true},
		{"\x1b[I", true},
		{"\x1b[O", true},
		{"\x1b]11;rgb:0000/0000/0000\x1b\\", true},
		{"\x1b]10;rgb:ffff/ffff/ffff\a", true},
		{"\x1bP1$r0m\x1b\\", true},
		{"\x1b[I\x1b[?62c", true},
		// DECRQM mode reports and XTWINOPS size reports from xterm.js.
		{"\x1b[?2026;2$y", true},
		{"\x1b[4;1;0$y", true},
		{"\x1b[8;24;80t", true},
		{"\x1b[4;384;640t", true},
		{"\x1b[6;16;8t", true},
		{"\x1b[?2026;2$y\x1b[8;24;80t\x1b[?1;2c", true},
		{"a", false},
		{"\r", false},
		{"\x1b", false},
		{"\x1b[A", false},
		{"\x1bb", false},
		{"\x1b[M #!", false},
		{"\x1b[<0;10;5M", false},
		{"\x1b[Ix", false},
		{"\x1b[12;40", false},
		{"\x1b]11;rgb:0000", false},
		{"\x1b[200~text\x1b[201~", false},
		{"\x1b[2;5y", false},
		{"\x1b[?2026;2$yx", false},
	} {
		if got := terminalReplyOnly([]byte(row.input)); got != row.reply {
			t.Fatalf("%q: got %v, want %v", row.input, got, row.reply)
		}
	}
}

func TestUserInputClearsAttention(t *testing.T) {
	s := attentionSession()
	feedSignals(s, agentEventOutput("question_asked"))
	s.userInputLocked([]byte("\x1b[I"), time.Now())
	if s.info.Attention == nil {
		t.Fatal("focus report cleared attention")
	}
	s.userInputLocked([]byte("y"), time.Now())
	if s.info.Attention != nil {
		t.Fatal("user input did not clear attention")
	}
}

func TestCancelledInputKeepsAttention(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	s := attentionSession()
	s.terminal, s.inputGate, s.done = writer, make(chan struct{}, 1), make(chan struct{})
	s.inputGate <- struct{}{}
	feedSignals(s, agentEventOutput("permission_request"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.WriteContext(ctx, []byte("y")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled input returned %v", err)
	}
	if s.info.Attention == nil {
		t.Fatal("cancelled input cleared attention")
	}
	if err := s.WriteContext(context.Background(), []byte("y")); err != nil {
		t.Fatal(err)
	}
	if s.info.Attention != nil {
		t.Fatal("written input did not clear attention")
	}
}

func TestStatusWithAttention(t *testing.T) {
	now := time.Now()
	patterns := []*regexp.Regexp{regexp.MustCompile(`never`)}
	if got := classify(now, now, "", false, true, false, 30*time.Second, patterns); got != StatusWaiting {
		t.Fatalf("recent output with attention: %s", got)
	}
	if got := classify(now, now, "", true, true, false, 30*time.Second, patterns); got != StatusExited {
		t.Fatalf("exited with attention: %s", got)
	}
}

func TestInfoAttentionIsCopied(t *testing.T) {
	s := attentionSession()
	s.raiseAttentionLocked(attentionkind.Done, SourceAgent, time.Now())
	info := s.infoLocked()
	info.Attention.Kind = attentionkind.Bell
	if s.info.Attention.Kind != attentionkind.Done {
		t.Fatal("Info exposed internal attention")
	}
	if sameAttention(info.Attention, s.info.Attention) || !sameAttention(nil, nil) || sameAttention(nil, s.info.Attention) {
		t.Fatal("sameAttention comparison")
	}
}

func TestSessionEnv(t *testing.T) {
	base := []string{
		"HOME=/home/u", "PATH=/bin", "LANG=C.UTF-8", "TERM=screen", "COLORTERM=24bit",
		"TERM_PROGRAM=WarpTerminal", "TERM_PROGRAM_VERSION=1", "WARP_CLIENT_VERSION=v0.2026",
		"WARP_CLI_AGENT_PROTOCOL_VERSION=9", "TMUX=/tmp/tmux-1/default,1,0", "TMUX_PANE=%1",
		"STY=1.pts", "ZELLIJ=0", "ZELLIJ_SESSION_NAME=x", "KITTY_WINDOW_ID=1", "ITERM_SESSION_ID=w0",
		"LC_TERMINAL=iTerm2", "LC_TERMINAL_VERSION=3", "WEZTERM_PANE=0", "GHOSTTY_RESOURCES_DIR=/g",
		"WT_SESSION=x", "WT_PROFILE_ID=y", "VTE_VERSION=7600", "WEBMUX_SESSION_ID=parent", "WEBMUX_LAUNCHER=windows-tray",
		"TERMINFO=/usr/share/terminfo",
	}
	want := []string{"HOME=/home/u", "PATH=/bin", "LANG=C.UTF-8", "TERMINFO=/usr/share/terminfo",
		"TERM=xterm-256color", "COLORTERM=truecolor", "WEBMUX_SESSION_ID=abcdefgh"}
	if got := sessionEnv(base, "abcdefgh", false, nil); !slices.Equal(got, want) {
		t.Fatalf("without protocol:\n%q\nwant\n%q", got, want)
	}
	got := sessionEnv(base, "abcdefgh", true, nil)
	if !slices.Equal(got, append(want, "WARP_CLI_AGENT_PROTOCOL_VERSION=1")) {
		t.Fatalf("with protocol: %q", got)
	}
	for _, entry := range got {
		if strings.HasPrefix(entry, "TERM_PROGRAM") || strings.HasPrefix(entry, "WARP_CLIENT_VERSION") {
			t.Fatalf("leaked Warp identity: %s", entry)
		}
	}
	explicit := sessionEnv(base, "abcdefgh", false, map[string]string{"PATH": "/venv/bin:/bin", "TERM": "dumb", "Z_VAR": "a b"})
	wantExplicit := []string{"HOME=/home/u", "LANG=C.UTF-8", "TERMINFO=/usr/share/terminfo",
		"COLORTERM=truecolor", "WEBMUX_SESSION_ID=abcdefgh", "PATH=/venv/bin:/bin", "TERM=dumb", "Z_VAR=a b"}
	if !slices.Equal(explicit, wantExplicit) {
		t.Fatalf("explicit env:\n%q\nwant\n%q", explicit, wantExplicit)
	}
	for _, bad := range []map[string]string{{"WEBMUX_SESSION_ID": "x"}, {"1BAD": "x"}, {"A=B": "x"}, {"OK": "nul\x00"}} {
		if ValidateEnv(bad) == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestRealSessionAttentionSurvivesSpinner(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-test/default,1,0")
	t.Setenv("TERM_PROGRAM", "tmux")
	script := `env > env.txt; printf '\033]777;notify;warp://cli-agent;{"v":1,"event":"question_asked"}\007'; ` +
		`stty raw -echo; while :; do printf '\r|'; sleep 0.05; printf '\r/'; sleep 0.05; done`
	dir := t.TempDir()
	m := NewManager(Options{DefaultCwd: dir, Presets: []Preset{{Name: "agent", Cmd: []string{"/bin/bash", "-c", script}}}})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	s, err := m.Create("", "agent", nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.Info().Attention == nil {
		if time.Now().After(deadline) {
			t.Fatal("question event never raised attention")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(2500 * time.Millisecond)
	info := s.Info()
	if info.Attention == nil || info.Attention.Kind != attentionkind.Question || info.Status != StatusWaiting {
		t.Fatalf("spinner masked attention: status %s, attention %+v", info.Status, info.Attention)
	}
	env, err := os.ReadFile(dir + "/env.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(env), "\n") {
		if strings.HasPrefix(line, "TMUX") || strings.HasPrefix(line, "TERM_PROGRAM=") {
			t.Fatalf("host terminal variable reached the session: %s", line)
		}
	}
	if !strings.Contains(string(env), "WARP_CLI_AGENT_PROTOCOL_VERSION=1\n") {
		t.Fatal("protocol version was not advertised")
	}
	if err := s.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if s.Info().Attention != nil {
		t.Fatal("user input did not clear attention")
	}
}
