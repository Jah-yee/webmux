package session

import "os"

// ContainsProcess reports whether pid runs inside a WebMux session: on Linux
// it carries a WEBMUX_SESSION_ID marker, or one of its ancestors is a session
// leader, a member of a session's terminal session, or this server itself
// (the Linux subreaper adopts orphaned session descendants). macOS cannot
// read other processes' environments, so only the process tree counts there.
func (m *Manager) ContainsProcess(pid int) bool {
	if hasSessionMarker(pid) {
		return true
	}
	leaders := make(map[int]bool)
	m.mu.Lock()
	for _, s := range m.sessions {
		if s.cmd != nil && s.cmd.Process != nil {
			leaders[s.cmd.Process.Pid] = true
		}
	}
	m.mu.Unlock()
	return insideTree(processes(), pid, os.Getpid(), leaders)
}
