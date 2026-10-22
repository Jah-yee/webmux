package session

import (
	"regexp"
	"strings"
	"time"
)

func classify(now, last time.Time, line string, exited, attention, blocked bool, idleAfter time.Duration, patterns []*regexp.Regexp) Status {
	if exited {
		return StatusExited
	}
	if attention {
		return StatusWaiting
	}
	age := now.Sub(last)
	if age < 2*time.Second {
		return StatusRunning
	}
	for _, pattern := range patterns {
		if pattern.MatchString(strings.TrimSpace(line)) {
			return StatusWaiting
		}
	}
	if blocked {
		return StatusWaiting
	}
	if age > idleAfter {
		return StatusIdle
	}
	return StatusRunning
}
func (s *Session) evaluate(now time.Time, snapshot processSnapshot, idleAfter time.Duration, patterns []*regexp.Regexp) {
	s.mu.Lock()
	defer s.mu.Unlock()
	blocked := false
	if s.info.ExitCode == nil && s.info.Attention == nil && now.Sub(s.info.LastActiveAt) >= 2*time.Second {
		blocked = snapshot.awaitingInput(s.cmd.Process.Pid)
	}
	s.info.Status = classify(now, s.info.LastActiveAt, s.preview.last, s.info.ExitCode != nil, s.info.Attention != nil, blocked, idleAfter, patterns)
}
func (s *Session) publishState() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.subs) > 0 {
		s.broadcastLocked(s.stateLocked())
	}
}
func sameDisplay(a, b Info) bool {
	return a.Title == b.Title && a.Status == b.Status && a.Preview == b.Preview && a.LastActiveAt.Equal(b.LastActiveAt) && a.Cols == b.Cols && a.Rows == b.Rows && a.Clients == b.Clients && sameExit(a.ExitCode, b.ExitCode) && sameWorkspace(a.Workspace, b.Workspace) && sameAttention(a.Attention, b.Attention) && sameReportedCwd(a.ReportedCwd, b.ReportedCwd)
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
