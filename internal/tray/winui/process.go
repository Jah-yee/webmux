//go:build windows

package winui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/rainchan/webmux/internal/tray"
	"github.com/rainchan/webmux/internal/wslclient"
)

const stillActive = 259

// Identify returns the creation time of a running process, which changes
// when Windows reuses its PID. It implements tray.Keepalive.Identify.
func Identify(pid int) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != stillActive {
		return "", false
	}
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return "", false
	}
	return strconv.FormatInt(created.Nanoseconds(), 10), true
}

// Kill terminates a process. It implements tray.Keepalive.Kill.
func Kill(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return windows.TerminateProcess(h, 1)
}

func shellExecute(file, args string) error {
	var params *uint16
	if args != "" {
		params = utf16Ptr(args)
	}
	return windows.ShellExecute(0, utf16Ptr("open"), utf16Ptr(file), params, nil, windows.SW_SHOWNORMAL)
}

// OpenURL opens an http(s) link in the default browser.
func OpenURL(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to open %q", url)
	}
	return shellExecute(url, "")
}

// OpenInNotepad opens a Windows path in Notepad. Windows paths cannot
// contain quotes, so quoting keeps the path one argument.
func OpenInNotepad(path string) error {
	if strings.ContainsRune(path, '"') {
		return fmt.Errorf("invalid path %q", path)
	}
	return shellExecute("notepad.exe", `"`+path+`"`)
}

// WSLPath maps an absolute path inside distro to its \\wsl.localhost path.
func WSLPath(distro, path string) (string, error) {
	if distro == "" {
		return "", errors.New("未知的发行版")
	}
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("不是 WSL 绝对路径：%s", path)
	}
	return `\\wsl.localhost\` + distro + strings.ReplaceAll(path, "/", `\`), nil
}

// Distros lists installed distributions: names from --list --quiet (exact,
// including spaces) and the default from --list --verbose.
func Distros(ctx context.Context, r tray.Runner) ([]wslclient.Distro, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	verboseInv := tray.ListVerbose()
	res, err := r.Run(ctx, verboseInv.Argv, verboseInv.Env)
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("wsl.exe --list --verbose: exit %d: %s", res.Code, strings.TrimSpace(wslclient.DecodeOutput(res.Stdout)))
	}
	verbose := wslclient.ParseListVerbose(res.Stdout)
	quietInv := tray.ListQuiet()
	quiet, err := r.Run(ctx, quietInv.Argv, quietInv.Env)
	if err != nil || quiet.Code != 0 {
		return verbose, nil
	}
	defaults := map[string]bool{}
	versions := map[string]int{}
	for _, d := range verbose {
		defaults[d.Name] = d.Default
		versions[d.Name] = d.Version
	}
	var out []wslclient.Distro
	for _, name := range wslclient.ParseListQuiet(quiet.Stdout) {
		out = append(out, wslclient.Distro{Name: name, Default: defaults[name], Version: versions[name]})
	}
	return out, nil
}

// CheckWSL runs `wsl.exe --version`, which only the Microsoft Store WSL
// supports, then lists the distributions.
func CheckWSL(ctx context.Context, r tray.Runner) ([]wslclient.Distro, error) {
	versionCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	inv := tray.WSLVersion()
	res, err := r.Run(versionCtx, inv.Argv, inv.Env)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("%w：%v", tray.ErrNoWSL, err)
	}
	if res.Code != 0 {
		return nil, fmt.Errorf("%w（wsl.exe --version 失败，需要 Microsoft Store 版 WSL）", tray.ErrNoWSL)
	}
	distros, err := Distros(ctx, r)
	if err != nil {
		return nil, fmt.Errorf("%w：%v", tray.ErrNoWSL, err)
	}
	if len(distros) == 0 {
		return nil, tray.ErrNoWSL
	}
	return distros, nil
}

// NetworkingMode reports `wslinfo --networking-mode`; failures count as NAT.
func NetworkingMode(ctx context.Context, r tray.Runner, t tray.Target) string {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	inv := t.NetworkingMode()
	res, err := r.Run(ctx, inv.Argv, inv.Env)
	if err != nil || res.Code != 0 {
		return "nat"
	}
	mode := strings.ToLower(strings.TrimSpace(wslclient.DecodeOutput(res.Stdout)))
	if mode == "" {
		return "nat"
	}
	return mode
}
