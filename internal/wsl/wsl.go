// Package wsl detects whether the process runs inside WSL and reports its
// networking mode. It is shared so the server and out-of-server clients agree
// on the same checks.
package wsl

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	interopMarker = "/proc/sys/fs/binfmt_misc/WSLInterop"
	releasePath   = "/proc/sys/kernel/osrelease"
)

// Detected reports whether this process runs inside WSL.
func Detected() bool {
	if os.Getenv("WSL_INTEROP") != "" || os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	if _, err := os.Stat(interopMarker); err == nil {
		return true
	}
	release, err := os.ReadFile(releasePath)
	return err == nil && strings.Contains(strings.ToLower(string(release)), "microsoft")
}

// NetworkingMode asks wslinfo; it returns mirrored, nat or unknown.
func NetworkingMode(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "wslinfo", "--networking-mode").Output()
	if err != nil {
		return "unknown"
	}
	switch mode := strings.ToLower(strings.TrimSpace(string(out))); mode {
	case "mirrored", "nat":
		return mode
	default:
		return "unknown"
	}
}
