// Package tray is the platform-independent core of the Windows tray: its
// settings, wsl.exe invocations, launch-mode selection, service supervisor,
// forwarding decisions and log writer. Windows APIs live in tray/winui.
package tray

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/rainchan/webmux/internal/statefile"
)

// Mode is how WebMux runs inside WSL.
type Mode string

const (
	ModeAuto    Mode = "auto"
	ModeTray    Mode = "tray"
	ModeSystemd Mode = "systemd"
)

// Settings never holds the WebMux token.
type Settings struct {
	// Distro is the target WSL distribution; empty means the default one.
	Distro string `json:"distro"`
	Mode   Mode   `json:"mode"`
	// Webmux is the executable inside WSL; "~/" is the WSL home directory.
	Webmux string `json:"webmux"`
	// Config is the WebMux configuration inside WSL; empty means default.
	Config string `json:"config"`
	// LogFile is the server log in tray-managed mode.
	LogFile string `json:"logFile"`
	// StartWebMux starts WebMux when the tray starts.
	StartWebMux bool `json:"startWebMux"`
	// AutoRestart restarts a tray-managed WebMux that exited unexpectedly.
	AutoRestart bool `json:"autoRestart"`
	// Forward lists the Windows addresses the user enabled forwarding on.
	Forward []string `json:"forward"`
}

func DefaultSettings() Settings {
	return Settings{
		Mode:        ModeAuto,
		Webmux:      "~/.local/bin/webmux",
		LogFile:     "~/.local/state/webmux/webmux.log",
		StartWebMux: true,
		AutoRestart: true,
		Forward:     []string{},
	}
}

// Validate checks values the tray passes to WSL as argv elements.
func (s Settings) Validate() error {
	switch s.Mode {
	case ModeAuto, ModeTray, ModeSystemd:
	default:
		return fmt.Errorf("mode must be auto, tray or systemd, not %q", s.Mode)
	}
	if strings.TrimSpace(s.Webmux) == "" {
		return errors.New("webmux must name the executable inside WSL")
	}
	for name, value := range map[string]string{"distro": s.Distro, "webmux": s.Webmux, "config": s.Config, "logFile": s.LogFile} {
		if strings.ContainsRune(value, 0) {
			return fmt.Errorf("%s contains a NUL byte", name)
		}
	}
	for name, value := range map[string]string{"config": s.Config, "logFile": s.LogFile} {
		if value != "" && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "~/") {
			return fmt.Errorf("%s must be an absolute WSL path or start with ~/", name)
		}
	}
	if s.LogFile == "" {
		return errors.New("logFile must not be empty")
	}
	for _, address := range s.Forward {
		ip := net.ParseIP(address)
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			return fmt.Errorf("forward address %q must be a specific non-loopback IP", address)
		}
	}
	return nil
}

// LoadSettings returns defaults for a missing file. For an unreadable,
// malformed or invalid file it returns defaults and the error; the file is
// left untouched.
func LoadSettings(path string) (Settings, error) {
	s := DefaultSettings()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return DefaultSettings(), err
	}
	decoder := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s); err != nil {
		return DefaultSettings(), fmt.Errorf("parse %s: %w", path, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return DefaultSettings(), fmt.Errorf("parse %s: trailing data", path)
	}
	if s.Forward == nil {
		s.Forward = []string{}
	}
	if err := s.Validate(); err != nil {
		return DefaultSettings(), fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// SaveSettings atomically writes s, creating the directory when needed.
func SaveSettings(path string, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return statefile.Write(path, append(data, '\n'), statefile.Options{TempPattern: ".tray-*.json"})
}
