package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/runtime"
)

// userBinDirs are the per-user install directories that deploy/webmux.service
// puts on PATH; agents installed by curl scripts, pnpm or npm live there.
var userBinDirs = []string{".local/bin", ".local/share/pnpm", ".npm-global/bin"}

// withUserBins prepends the user install directories missing from path, in
// the service template's order.
func withUserBins(path, home string) string {
	have := filepath.SplitList(path)
	var add []string
	for _, dir := range userBinDirs {
		dir = filepath.Join(home, dir)
		if !slices.Contains(have, dir) {
			add = append(add, dir)
		}
	}
	if len(add) == 0 {
		return path
	}
	if path == "" {
		return strings.Join(add, string(filepath.ListSeparator))
	}
	return strings.Join(add, string(filepath.ListSeparator)) + string(filepath.ListSeparator) + path
}

// applyLaunchEnv gives a tray-started server the PATH a service-managed one
// gets: wsl.exe --exec runs no login shell, so ~/.profile never adds
// ~/.local/bin and agents such as claude or omp would not be found.
func applyLaunchEnv() {
	if runtime.Launcher(os.Getenv) != contract.LauncherWindowsTray {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	_ = os.Setenv("PATH", withUserBins(os.Getenv("PATH"), home))
}
