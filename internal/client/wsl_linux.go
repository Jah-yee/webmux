//go:build linux

package client

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// wslVirtualInterfaces lists the Hyper-V synthetic NICs when running in
// WSL's NAT networking mode; their addresses sit behind Windows' NAT. In
// mirrored mode those NICs carry the host's real addresses and are kept.
// The networking mode cannot change without restarting WSL, so it is
// detected once per process.
var wslVirtualInterfaces = sync.OnceValue(func() map[string]bool {
	if _, err := os.Stat("/proc/sys/fs/binfmt_misc/WSLInterop"); err != nil {
		if release, err := os.ReadFile("/proc/sys/kernel/osrelease"); err != nil || !strings.Contains(strings.ToLower(string(release)), "microsoft") {
			return nil
		}
	}
	if wslNetworkingMode() != "nat" {
		return nil
	}
	virtual := make(map[string]bool)
	entries, _ := os.ReadDir("/sys/class/net")
	for _, entry := range entries {
		driver, err := os.Readlink(filepath.Join("/sys/class/net", entry.Name(), "device", "driver"))
		if err == nil && filepath.Base(driver) == "hv_netvsc" {
			virtual[entry.Name()] = true
		}
	}
	return virtual
})

// wslNetworkingMode asks wslinfo (WSL 2.0+); an unknown mode counts as NAT.
func wslNetworkingMode() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "wslinfo", "--networking-mode").Output()
	if err != nil {
		return "nat"
	}
	return strings.ToLower(strings.TrimSpace(string(out)))
}
