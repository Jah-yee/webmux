//go:build linux || darwin

package session

import (
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// state uses ps letters: R running, S sleeping, Z zombie. Other values, including
// '?' for a process whose run state is unreadable, never count as waiting.
// timer is a positive nanosleep or sleep(1) wait, not an unread terminal.
type processState struct {
	ppid  int
	state byte
	sid   int
	start uint64
	timer bool
}
type processTable map[int]processState

// A probe's immutable index is shared by every session until the next probe.
type processSnapshot struct {
	table    processTable
	children map[int][]int
}

func newProcessSnapshot(table processTable) processSnapshot {
	children := make(map[int][]int, len(table))
	for child, state := range table {
		children[state.ppid] = append(children[state.ppid], child)
	}
	return processSnapshot{table: table, children: children}
}

func (p processSnapshot) foregroundBlocked(pid int) bool {
	root, ok := p.table[pid]
	if !ok {
		return false
	}
	if root.state == 'R' {
		return false
	}
	seen := map[int]bool{pid: true}
	pending := []int{pid}
	leaves := 0
	for len(pending) > 0 {
		member := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		state := p.table[member]
		if state.state == 'R' {
			return false
		}
		if len(p.children[member]) == 0 {
			leaves++
			if state.state != 'S' {
				return false
			}
		}
		for _, child := range p.children[member] {
			// Process snapshots can contain cycles; visit each PID only once.
			if !seen[child] {
				seen[child] = true
				pending = append(pending, child)
			}
		}
	}
	return leaves > 0
}

// awaitingInput is the user-attention half of a quiescent tree. Confirmed
// timer leaves (sleep, nanosleep) are waiting on the clock, not the keyboard.
func (p processSnapshot) awaitingInput(pid int) bool {
	if !p.foregroundBlocked(pid) {
		return false
	}
	return !p.timerLeaves(pid)
}

func (p processSnapshot) timerLeaves(pid int) bool {
	seen := map[int]bool{pid: true}
	pending := []int{pid}
	leaves := 0
	for len(pending) > 0 {
		member := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if len(p.children[member]) == 0 {
			leaves++
			if !enrichTimer(member, p.table[member]).timer {
				return false
			}
		}
		for _, child := range p.children[member] {
			if !seen[child] {
				seen[child] = true
				pending = append(pending, child)
			}
		}
	}
	return leaves > 0
}

func cString(b []byte) string {
	return cutCString(string(b))
}

func cutCString(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, " ")
}

func sleepCommand(name string) bool {
	return path.Base(cutCString(name)) == "sleep"
}

func timerWait(name string) bool {
	name = cutCString(name)
	if name == "" || name == "0" {
		return false
	}
	// Darwin e_wmesg is 8 bytes with WMESGLEN 7, so nanosleep is stored as "nanosle".
	return strings.Contains(name, "nanosleep") || strings.HasPrefix(name, "nanoslee") || name == "nanosle"
}

// resizePTYLocked pins the PTY descriptor against concurrent close/reuse.
// TIOCSWINSZ lets the kernel notify only this terminal's foreground job.
func (s *Session) resizePTYLocked(cols, rows uint16) error {
	if s.terminal == nil {
		return os.ErrInvalid
	}
	raw, err := s.terminal.SyscallConn()
	if err != nil {
		return err
	}
	var resizeErr error
	err = raw.Control(func(fd uintptr) {
		resizeErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Col: cols, Row: rows})
	})
	if err != nil {
		return err
	}
	return resizeErr
}
