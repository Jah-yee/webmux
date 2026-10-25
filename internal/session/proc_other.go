//go:build !linux && !darwin

package session

import "github.com/creack/pty"

type processTable struct{}
type processSnapshot struct{}

func processes() processTable                         { return processTable{} }
func newProcessSnapshot(processTable) processSnapshot { return processSnapshot{} }
func (processSnapshot) foregroundBlocked(int) bool    { return false }
func (processSnapshot) awaitingInput(int) bool        { return false }

func (s *Session) resizePTYLocked(cols, rows uint16) error {
	return pty.Setsize(s.terminal, &pty.Winsize{Cols: cols, Rows: rows})
}
