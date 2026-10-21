//go:build !windows

// Command webmux-windows is the Windows console client; it only runs on
// Windows.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "webmux-windows 仅支持 Windows；在 Linux/macOS 上请使用 webmux")
	os.Exit(1)
}
