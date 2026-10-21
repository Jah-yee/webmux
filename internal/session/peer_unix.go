//go:build linux || darwin

package session

// insideTree walks pid's ancestors. A process whose terminal session is a
// leader's session also counts, which covers job-control groups that
// re-parented away from the leader.
func insideTree(table processTable, pid, server int, leaders map[int]bool) bool {
	seen := make(map[int]bool)
	for current := pid; current > 1 && !seen[current]; {
		seen[current] = true
		state, ok := table[current]
		if !ok {
			return false
		}
		if leaders[current] || leaders[state.sid] || (current != pid && current == server) {
			return true
		}
		current = state.ppid
	}
	return false
}
