package wslclient

import (
	"os"
	"os/exec"
)

// NewCommand builds a wsl.exe invocation from argv with WSL_UTF8=1 and no
// console window on Windows. Nothing passes through a shell.
func NewCommand(argv []string, env ...string) *exec.Cmd {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(append(os.Environ(), "WSL_UTF8=1"), env...)
	hideWindow(cmd)
	return cmd
}
