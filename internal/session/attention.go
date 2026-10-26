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
// clears it; only user input, an agent's own status or resume event, or exit
// does.
type Attention struct {
	Kind   attentionkind.Kind
	Source AttentionSource
	Since  time.Time
	// layer records which signal source raised it; it is merge policy only
	// and never leaves the session package.
	layer signalLayer
}

// signalLayer orders signal sources: a weaker layer never replaces or clears
// attention raised by a stronger one, and an OSC 7501 root record silences
// every other layer while it exists.
type signalLayer int

const (
	layerBell    signalLayer = iota + 1 // bare BEL
	layerNotify                         // OSC 9 / OSC 777 notifications
	layerAgent                          // Warp cli-agent events
	layerProgram                        // OSC 7501 root record
)

// programRecord is the OSC 7501 root record: only state and kind are kept.
type programRecord struct {
	state, kind string
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

// raiseAttentionLocked merges a signal below the program layer: it is ignored
// while a root record exists or when the current attention comes from a
// stronger layer, and otherwise keeps a stronger existing kind. Since changes
// only when the kind does.
func (s *Session) raiseAttentionLocked(kind attentionkind.Kind, source AttentionSource, layer signalLayer, now time.Time) {
	if s.program != nil {
		return
	}
	current := s.info.Attention
	if current != nil {
		if current.layer > layer || attentionRank(kind) < attentionRank(current.Kind) {
			return
		}
		if current.Kind == kind {
			current.Source, current.layer = source, layer
			return
		}
	}
	s.info.Attention = &Attention{Kind: kind, Source: source, Since: now, layer: layer}
}

// programStatusLocked applies an OSC 7501 root record report. Only a change
// of the record moves attention, so a repeated report never revives attention
// the user already cleared; a change replaces attention whatever its rank.
func (s *Session) programStatusLocked(state, kind string, now time.Time) {
	s.structured = true
	if state == "clear" {
		if s.program != nil {
			s.program = nil
			s.info.Attention = nil
		}
		return
	}
	next := programRecord{state: state, kind: kind}
	if s.program != nil && *s.program == next {
		return
	}
	s.program = &next
	switch state {
	case "blocked":
		if kind == "permission" {
			s.setProgramAttentionLocked(attentionkind.Permission, now)
		} else {
			s.setProgramAttentionLocked(attentionkind.Question, now)
		}
	case "done":
		s.setProgramAttentionLocked(attentionkind.Done, now)
	case "error":
		s.setProgramAttentionLocked(attentionkind.Error, now)
	default: // working, idle
		s.info.Attention = nil
	}
}

func (s *Session) setProgramAttentionLocked(kind attentionkind.Kind, now time.Time) {
	if current := s.info.Attention; current != nil && current.Kind == kind {
		current.Source, current.layer = SourceAgent, layerProgram
		return
	}
	s.info.Attention = &Attention{Kind: kind, Source: SourceAgent, Since: now, layer: layerProgram}
}

// dropProgramLocked forgets the root record and the attention it raised.
func (s *Session) dropProgramLocked() {
	s.program = nil
	if s.info.Attention != nil && s.info.Attention.layer == layerProgram {
		s.info.Attention = nil
	}
}

// promptLocked handles OSC 133;A: a new shell prompt ends a working or
// blocked program, while done and error outlive it.
func (s *Session) promptLocked() {
	if s.program != nil && (s.program.state == "working" || s.program.state == "blocked") {
		s.dropProgramLocked()
	}
}

func (s *Session) agentEventLocked(event string, now time.Time) {
	s.structured = true
	switch event {
	case "question_asked":
		s.raiseAttentionLocked(attentionkind.Question, SourceAgent, layerAgent, now)
	case "permission_request":
		s.raiseAttentionLocked(attentionkind.Permission, SourceAgent, layerAgent, now)
	case "stop", "idle_prompt":
		s.raiseAttentionLocked(attentionkind.Done, SourceAgent, layerAgent, now)
	case "stop_failure":
		s.raiseAttentionLocked(attentionkind.Error, SourceAgent, layerAgent, now)
	case "prompt_submit", "permission_replied", "session_start":
		if s.program == nil {
			s.info.Attention = nil
		}
	}
}

func (s *Session) bellLocked(now time.Time) {
	if s.bellAttention && now.Sub(s.lastInputAt) >= bellQuietPeriod {
		s.raiseAttentionLocked(attentionkind.Bell, SourceTerminal, layerBell, now)
	}
}

func (s *Session) signalHandler() *SignalHandler {
	hostname, _ := os.Hostname()
	h := &SignalHandler{
		Title: func(title string) {
			if s.userTitle == "" {
				if title == "" {
					title = filepath.Base(s.info.Cwd)
				}
				s.info.Title = title
			}
		},
		Bell:   func() { s.bellLocked(time.Now()) },
		Notify: func() { s.raiseAttentionLocked(attentionkind.Notify, SourceTerminal, layerNotify, time.Now()) },
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
	if s.programStatus {
		h.ProgramStatus = func(state, kind string) { s.programStatusLocked(state, kind, time.Now()) }
		h.Probe = s.queueProbeReplyLocked
		h.Prompt = s.promptLocked
		h.Reset = s.dropProgramLocked
	}
	return h
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
