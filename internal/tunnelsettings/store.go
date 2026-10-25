// Package tunnelsettings owns private, persistent tunnel settings. It never
// grants public access; startup callers must honor its fail-closed result.
package tunnelsettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/pathutil"
	"github.com/rainchan/webmux/internal/statefile"
)

var (
	ErrUnavailable   = errors.New("tunnel settings storage unavailable")
	ErrInvalid       = errors.New("invalid tunnel settings")
	ErrEnableBlocked = errors.New("tunnel cannot be enabled")
	ErrNotPersisted  = errors.New("tunnel is off for this process, but the disable was not persisted; repair storage before restarting")
)

// Pointer fields distinguish complete files from truncated/missing values.
type storedFile struct {
	V        int     `json:"v"`
	Enabled  *bool   `json:"enabled"`
	Mode     string  `json:"mode"`
	Hostname *string `json:"hostname"`
	Token    string  `json:"token,omitempty"`
}

// Service holds an exclusive lock for its lifetime. A fault disables writes
// and startup exposure without changing the original file.
type Service struct {
	mu                sync.Mutex
	dir, path         string
	lock              *os.File
	defaults, current config.TunnelConfig
	web               bool
	broken            error
}

func Open(stateDir string, defaults config.TunnelConfig) *Service {
	if defaults.Named() {
		if normalized, err := config.ValidateTunnelSettings("named", defaults.Hostname, defaults.Token); err == nil {
			defaults.Hostname = normalized.Hostname
		}
	}
	s := &Service{defaults: defaults, current: defaults}
	dir, err := pathutil.Expand(stateDir)
	if err == nil {
		dir, err = filepath.Abs(dir)
	}
	if err == nil {
		s.dir, s.path = dir, filepath.Join(dir, "tunnel.json")
		err = s.load()
	}
	if err != nil {
		s.broken = err
		s.current.Enabled = false
	}
	return s
}

func (s *Service) load() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return errors.New("cannot create state directory")
	}
	if err := os.Chmod(s.dir, 0700); err != nil {
		return errors.New("cannot secure state directory")
	}
	lock, err := os.OpenFile(filepath.Join(s.dir, "tunnel-config.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return errors.New("cannot open tunnel settings lock")
	}
	if err := lock.Chmod(0600); err != nil {
		_ = lock.Close()
		return errors.New("cannot secure tunnel settings lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return errors.New("tunnel settings are locked by another instance")
	}
	s.lock = lock
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		// A dangling symlink is not a confirmed missing settings file.
		if _, statErr := os.Lstat(s.path); errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
	}
	if err != nil {
		return errors.New("cannot read tunnel settings file")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var stored storedFile
	if err := d.Decode(&stored); err != nil {
		return errors.New("tunnel settings file is corrupt")
	}
	if err := d.Decode(new(any)); err != io.EOF || stored.V != 1 || stored.Enabled == nil || stored.Hostname == nil {
		return errors.New("tunnel settings file is corrupt or has an unsupported version")
	}
	next, err := config.ValidateTunnelSettings(stored.Mode, *stored.Hostname, stored.Token)
	if err != nil || (stored.Mode == "quick" && (stored.Token != "" || *stored.Hostname != "")) {
		return errors.New("tunnel settings file contains invalid fields")
	}
	if err := os.Chmod(s.path, 0600); err != nil {
		return errors.New("cannot secure tunnel settings file")
	}
	s.current.Enabled, s.current.Token, s.current.Hostname = *stored.Enabled, next.Token, next.Hostname
	s.web = true
	return nil
}

// Effective returns the current settings and whether they came from the web.
// An unavailable store returns Enabled=false even when TOML enables exposure.
func (s *Service) Effective() (config.TunnelConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, s.web
}

// Prepare validates an update, preserving the credential only on omission.
func Prepare(current config.TunnelConfig, mode, hostname string, token *string) (config.TunnelConfig, error) {
	credential := current.Token
	if token != nil {
		credential = *token
	}
	next, err := config.ValidateTunnelSettings(mode, hostname, credential)
	if err != nil {
		return config.TunnelConfig{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	current.Token, current.Hostname = next.Token, next.Hostname
	return current, nil
}

func (s *Service) unavailableLocked() error {
	if s.broken != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, s.broken)
	}
	return nil
}

func (s *Service) Unavailable() error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.unavailableLocked()
}

// Save publishes before changing in-memory settings. A failed write leaves
// the previous effective settings intact and marks storage unavailable.
func (s *Service) Save(next config.TunnelConfig) error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unavailableLocked(); err != nil {
		return err
	}
	mode := "quick"
	if next.Named() {
		mode = "named"
	}
	normalized, err := Prepare(next, mode, next.Hostname, nil)
	if err != nil {
		return err
	}
	stored := storedFile{V: 1, Enabled: &normalized.Enabled, Mode: mode, Token: normalized.Token, Hostname: &normalized.Hostname}
	if err := statefile.WriteJSON(s.path, stored, statefile.Options{TempPattern: ".tunnel-*", RequireDirSync: true}); err != nil {
		s.broken = errors.New("cannot persist tunnel settings")
		return s.unavailableLocked()
	}
	s.current, s.web = normalized, true
	return nil
}

// Reset removes the override durably. The controller stops this process,
// while a later startup uses the original TOML Enabled value.
func (s *Service) Reset() (config.TunnelConfig, error) {
	if s == nil {
		return config.TunnelConfig{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unavailableLocked(); err != nil {
		return config.TunnelConfig{}, err
	}
	err := os.Remove(s.path)
	if err == nil {
		var dir *os.File
		dir, err = os.Open(s.dir)
		if err == nil {
			err = dir.Sync()
			_ = dir.Close()
		}
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		s.broken = errors.New("cannot remove tunnel settings")
		return config.TunnelConfig{}, s.unavailableLocked()
	}
	s.current, s.web = s.defaults, false
	return s.current, nil
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		_ = s.lock.Close()
		s.lock = nil
	}
	s.broken = errors.New("tunnel settings service is closed")
}
