//go:build !windows

// Command webmux-tray is the Windows tray for WebMux in WSL.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "webmux-tray 仅支持 Windows")
	os.Exit(1)
}
