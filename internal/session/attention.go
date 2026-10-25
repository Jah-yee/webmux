package session

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rainchan/webmux/internal/attentionkind"
)

type AttentionSource string

const (
	SourceAgent    AttentionSource = "agent"
	SourceTerminal AttentionSource = "terminal"
)

// bellQuietPeriod ignores bells that answer a keystroke, such as shell completion.
const bellQuietPeriod = time.Second

// Attention latches a signal that the session needs the user. Output never
// clears it; only user input, an agent's resume event, or exit does.
type Attention struct {
	Kind   attentionkind.Kind
	Source AttentionSource
	Since  time.Time
}

func attentionRank(kind attentionkind.Kind) int {
	switch kind {
	case attentionkind.Question, attentionkind.Permission:
		return 4
	case attentionkind.Error:
		return 3
	case attentionkind.Done:
		return 2
	case attentionkind.Notify:
		return 1
	}
	return 0
}

// raiseAttentionLocked keeps a stronger existing signal and refreshes Since
// only when the kind changes.
func (s *Session) raiseAttentionLocked(kind attentionkind.Kind, source AttentionSource, now time.Time) {
	current := s.info.Attention
	if current != nil {
		if attentionRank(kind) < attentionRank(current.Kind) {
			return
		}
		if current.Kind == kind {
			current.Source = source
			return
		}
	}
	s.info.Attention = &Attention{Kind: kind, Source: source, Since: now}
}

func (s *Session) agentEventLocked(event string, now time.Time) {
	switch event {
	case "question_asked":
		s.raiseAttentionLocked(attentionkind.Question, SourceAgent, now)
	case "permission_request":
		s.raiseAttentionLocked(attentionkind.Permission, SourceAgent, now)
	case "stop", "idle_prompt":
		s.raiseAttentionLocked(attentionkind.Done, SourceAgent, now)
	case "stop_failure":
		s.raiseAttentionLocked(attentionkind.Error, SourceAgent, now)
	case "prompt_submit", "permission_replied", "session_start":
		s.info.Attention = nil
	}
}

func (s *Session) bellLocked(now time.Time) {
	if s.bellAttention && now.Sub(s.lastInputAt) >= bellQuietPeriod {
		s.raiseAttentionLocked(attentionkind.Bell, SourceTerminal, now)
	}
}

func (s *Session) signalHandler() *SignalHandler {
	hostname, _ := os.Hostname()
	return &SignalHandler{
		Title: func(title string) {
			if s.userTitle == "" {
				if title == "" {
					title = filepath.Base(s.info.Cwd)
				}
				s.info.Title = title
			}
		},
		Bell:   func() { s.bellLocked(time.Now()) },
		Notify: func() { s.raiseAttentionLocked(attentionkind.Notify, SourceTerminal, time.Now()) },
		Event:  func(event string) { s.agentEventLocked(event, time.Now()) },
		Cwd: func(path, host string) {
			remote := !reportedHostIsLocal(host, hostname)
			current := s.info.ReportedCwd
			if current != nil && current.Path == path && current.Host == host && current.Remote == remote {
				// A prompt in the same directory is not a state change.
				return
			}
			s.info.ReportedCwd = &ReportedCwd{Path: path, Host: host, Remote: remote, Since: time.Now()}
		},
	}
}

// userInputLocked records accepted input; terminal replies leave state alone.
func (s *Session) userInputLocked(p []byte, now time.Time) {
	if terminalReplyOnly(p) {
		return
	}
	s.recordInputLocked(now)
}

// recordInputLocked records input known to come from the user.
func (s *Session) recordInputLocked(now time.Time) {
	s.lastInputAt = now
	s.info.Attention = nil
}

// terminalReplyOnly reports input made solely of sequences a terminal emits on
// its own: OSC and DCS strings, and CSI reports ending in c, R, n, I, O, t or
// $y (device attributes, cursor position, status, focus, window size, mode
// state). Incomplete sequences and anything else, including mouse reports,
// count as user input.
func terminalReplyOnly(p []byte) bool {
	for i := 0; i < len(p); {
		if p[i] != 27 || i+1 >= len(p) {
			return false
		}
		switch p[i+1] {
		case '[':
			j := i + 2
			for j < len(p) && p[j] >= 0x20 && p[j] <= 0x3f {
				j++
			}
			if j >= len(p) {
				return false
			}
			// DECRQM replies end in "$y"; replayed history makes xterm.js
			// answer every mode query again whenever a browser attaches.
			report := strings.ContainsRune("cRnIOt", rune(p[j])) || p[j] == 'y' && p[j-1] == '$'
			if !report {
				return false
			}
			i = j + 1
		case ']', 'P':
			end := stringEnd(p, i+2, p[i+1] == ']')
			if end < 0 {
				return false
			}
			i = end
		default:
			return false
		}
	}
	return true
}

// stringEnd returns the index after an ST (or BEL for OSC) terminator, or -1.
func stringEnd(p []byte, from int, bel bool) int {
	for j := from; j < len(p); j++ {
		if bel && p[j] == 7 {
			return j + 1
		}
		if p[j] == 27 && j+1 < len(p) && p[j+1] == '\\' {
			return j + 2
		}
	}
	return -1
}
