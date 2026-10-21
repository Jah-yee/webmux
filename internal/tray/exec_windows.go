//go:build windows

package tray

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// configure hides console windows; detached processes also get their own
// process group so console control events of the tray never reach them.
func configure(cmd *exec.Cmd, detached bool) {
	flags := uint32(windows.CREATE_NO_WINDOW)
	if detached {
		flags |= windows.CREATE_NEW_PROCESS_GROUP
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
}
