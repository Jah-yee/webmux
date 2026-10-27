package session

import (
	"regexp"
	"strings"
	"time"
)

// statusInput is everything status composition reads, in priority order:
// exit, the OSC 7501 root record, latched attention, structured agent
// signals, then the heuristics (output window, prompt patterns, a process
// blocked on terminal input, idle threshold).
type statusInput struct {
	now, last  time.Time
	line       string
	exited     bool
	program    *programRecord
	attention  bool
	structured bool
	blocked    bool
	idleAfter  time.Duration
	patterns   []*regexp.Regexp
}

func classify(in statusInput) Status {
	if in.exited {
		return StatusExited
	}
	age := in.now.Sub(in.last)
	if in.program != nil {
		switch in.program.state {
		case "blocked":
			return StatusWaiting
		case "done", "error":
			if in.attention {
				return StatusWaiting
			}
			return StatusIdle
		case "idle":
			return StatusIdle
		}
		// working: a program that stopped drawing for this long has likely
		// died without clearing its record.
		if age > in.idleAfter {
			return StatusIdle
		}
		return StatusRunning
	}
	if in.attention {
		return StatusWaiting
	}
	if age < 2*time.Second {
		return StatusRunning
	}
	// An agent that reports its own waiting is always reading the terminal
	// and may print prompt-like text, so the waiting heuristics would lie.
	if !in.structured {
		for _, pattern := range in.patterns {
			if pattern.MatchString(strings.TrimSpace(in.line)) {
				return StatusWaiting
			}
		}
		if in.blocked {
			return StatusWaiting
		}
	}
	if age > in.idleAfter {
		return StatusIdle
	}
	return StatusRunning
}

func (s *Session) statusInputLocked(now time.Time, idleAfter time.Duration, patterns []*regexp.Regexp) statusInput {
	return statusInput{
		now: now, last: s.info.LastActiveAt, line: s.preview.last,
		exited: s.info.ExitCode != nil, program: s.program, attention: s.info.Attention != nil,
		structured: s.structured, idleAfter: idleAfter, patterns: patterns,
	}
}

func (s *Session) evaluate(now time.Time, snapshot processSnapshot, idleAfter time.Duration, patterns []*regexp.Regexp) {
	s.mu.Lock()
	// Flush a throttled preview so the final chunk of a burst is not lost.
	if s.preview.dirty {
		s.info.Preview = s.preview.refresh(true)
	}
	in := s.statusInputLocked(now, idleAfter, patterns)
	pid := s.cmd.Process.Pid
	s.mu.Unlock()
	// Reading /proc must not hold the session lock while output is flowing.
	if !in.exited && in.program == nil && !in.attention && !in.structured && now.Sub(in.last) >= 2*time.Second {
		in.blocked = snapshot.awaitingInput(pid)
	}
	s.mu.Lock()
	switch {
	case s.info.ExitCode != nil:
		s.info.Status = StatusExited
	case !s.info.LastActiveAt.After(in.last):
		// Output arrived while /proc was read; keep its running status.
		s.info.Status = classify(in)
	}
	s.mu.Unlock()
}

// outputStatusLocked is the status right after PTY output: the output window
// makes every heuristic report running, so only the record and attention
// matter.
func (s *Session) outputStatusLocked() Status {
	now := time.Now()
	return classify(statusInput{
		now: now, last: now, program: s.program,
		attention: s.info.Attention != nil, idleAfter: time.Hour,
	})
}

func (s *Session) publishState() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.subs) > 0 {
		s.broadcastLocked(s.stateLocked())
	}
}
func sameDisplay(a, b Info) bool {
	return a.Title == b.Title && a.Status == b.Status && a.Preview == b.Preview &&
		a.LastActiveAt.Equal(b.LastActiveAt) && a.Cols == b.Cols && a.Rows == b.Rows &&
		a.Clients == b.Clients && sameExit(a.ExitCode, b.ExitCode) &&
		sameWorkspace(a.Workspace, b.Workspace) && sameAttention(a.Attention, b.Attention) &&
		sameReportedCwd(a.ReportedCwd, b.ReportedCwd)
}
func sameExit(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameWorkspace(a, b *Workspace) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameAttention(a, b *Attention) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Kind == b.Kind && a.Source == b.Source && a.Since.Equal(b.Since)
}

func sameReportedCwd(a, b *ReportedCwd) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Path == b.Path && a.Host == b.Host && a.Remote == b.Remote && a.Since.Equal(b.Since)
}
