//go:build windows

package winui

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey       = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "WebMuxTray"
)

// AutostartEnabled reports whether the login item exists and points to exe.
func AutostartEnabled(exe string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = k.Close() }()
	value, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return false
	}
	return samePath(commandPath(value), exe)
}

// SetAutostart registers exe as a login item, or removes the item.
func SetAutostart(on bool, exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	if on {
		return k.SetStringValue(runValueName, `"`+exe+`"`)
	}
	if err := k.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

// commandPath extracts the executable from a Run command line.
func commandPath(command string) string {
	command = strings.TrimSpace(command)
	if rest, ok := strings.CutPrefix(command, `"`); ok {
		if end := strings.IndexByte(rest, '"'); end >= 0 {
			return rest[:end]
		}
		return rest
	}
	if end := strings.IndexByte(command, ' '); end >= 0 && strings.HasSuffix(strings.ToLower(command[:end]), ".exe") {
		return command[:end]
	}
	return command
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
