//go:build !windows

package wslclient

import "os/exec"

func hideWindow(*exec.Cmd) {}
