package wslclient

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultWebmux is webmux inside WSL when nothing else is configured.
const DefaultWebmux = "~/.local/bin/webmux"

// Target is the distribution and webmux executable a Windows client uses.
// An empty Distro is WSL's default distribution.
type Target struct {
	Distro string
	Webmux string
}

// TraySettingsPath is the tray's settings file under %APPDATA%.
func TraySettingsPath(appData string) string {
	return filepath.Join(appData, "WebMux", "tray.json")
}

// ResolveTarget applies the precedence flag > environment variable >
// tray settings > default, separately for the distribution
// (WEBMUX_DISTRO) and the webmux path (WEBMUX_WSL_PATH). A missing or
// unreadable tray settings file counts as absent.
func ResolveTarget(flagDistro, flagWebmux string, getenv func(string) string, traySettings string) Target {
	var tray struct {
		Distro string `json:"distro"`
		Webmux string `json:"webmux"`
	}
	if data, err := os.ReadFile(traySettings); err == nil {
		if json.Unmarshal(trimBOM(data), &tray) != nil {
			tray.Distro, tray.Webmux = "", ""
		}
	}
	return Target{
		Distro: first(flagDistro, getenv("WEBMUX_DISTRO"), tray.Distro),
		Webmux: first(flagWebmux, getenv("WEBMUX_WSL_PATH"), tray.Webmux, DefaultWebmux),
	}
}

func trimBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xef && data[1] == 0xbb && data[2] == 0xbf {
		return data[3:]
	}
	return data
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (t Target) base() []string {
	argv := []string{WSL}
	if t.Distro != "" {
		argv = append(argv, "-d", t.Distro)
	}
	return argv
}

// Command runs a webmux subcommand from the WSL home directory.
func (t Target) Command(args ...string) []string {
	return append(append(t.base(), "--cd", "~", "--exec", WebmuxPath(t.Webmux)), args...)
}

// Bridge is the argv of one local API connection.
func (t Target) Bridge() []string { return t.Command("bridge") }

// Wslpath converts one absolute Windows drive path with the distribution's
// automount rules; the path is a single argv element.
func (t Target) Wslpath(path string) []string {
	return append(t.base(), "--exec", "wslpath", "-a", "-u", path)
}

// ListVerbose lists installed distributions.
func ListVerbose() []string { return []string{WSL, "--list", "--verbose"} }
