//go:build !windows

package tray

import (
	"os/exec"
	"syscall"
)

// configure puts detached processes in their own process group.
func configure(cmd *exec.Cmd, detached bool) {
	if detached {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
}
