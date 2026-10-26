//go:build linux

package client

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/rainchan/webmux/internal/wsl"
)

// wslVirtualInterfaces lists the Hyper-V synthetic NICs when running in
// WSL's NAT networking mode; their addresses sit behind Windows' NAT. In
// mirrored mode those NICs carry the host's real addresses and are kept.
// The networking mode cannot change without restarting WSL, so it is
// detected once per process.
var wslVirtualInterfaces = sync.OnceValue(func() map[string]bool {
	if !wsl.Detected() {
		return nil
	}
	mode := wsl.NetworkingMode(context.Background())
	if mode != "nat" && mode != "unknown" {
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
