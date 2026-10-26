// Package tailscalesettings owns the private, persistent web override of the
// [tailscale] mode, port and provider. It never applies an entry itself;
// startup callers must honor its fail-closed result.
package tailscalesettings

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
	ErrUnavailable = errors.New("tailscale settings storage unavailable")
	ErrInvalid     = errors.New("invalid tailscale settings")
	// ErrEnableBlocked rejects an entry this process cannot safely publish.
	ErrEnableBlocked = errors.New("tailscale entry cannot be enabled")
	// ErrFunnelUnconfirmed rejects a switch to Funnel without the explicit
	// public-exposure confirmation.
	ErrFunnelUnconfirmed = errors.New("enabling Funnel exposes WebMux to the public internet; confirm the risk to continue")
)

const fileName = "tailscale-settings.json"

// Pointer fields distinguish complete files from truncated/missing values.
type storedFile struct {
	V        int     `json:"v"`
	Mode     *string `json:"mode"`
	Port     *int    `json:"port"`
	Provider *string `json:"provider"`
}

// Service holds an exclusive lock for its lifetime. A fault disables writes
// and forces the effective mode off without changing the original file.
type Service struct {
	mu                sync.Mutex
	dir, path         string
	lock              *os.File
	defaults, current config.TailscaleConfig
	web               bool
	broken            error
}

// Open loads the override for stateDir. defaults is the validated TOML value.
func Open(stateDir string, defaults config.TailscaleConfig) *Service {
	s := &Service{defaults: defaults, current: defaults}
	dir, err := pathutil.Expand(stateDir)
	if err == nil {
		dir, err = filepath.Abs(dir)
	}
	if err == nil {
		s.dir, s.path = dir, filepath.Join(dir, fileName)
		err = s.load()
	}
	if err != nil {
		s.broken = err
		s.current.Mode = config.TailscaleOff
	}
	return s
}

func (s *Service) load() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return errors.New("cannot create state directory")
	}
	lock, err := os.OpenFile(filepath.Join(s.dir, "tailscale-settings.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return errors.New("cannot open tailscale settings lock")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return errors.New("tailscale settings are locked by another instance")
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
		return errors.New("cannot read tailscale settings file")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var stored storedFile
	if err := d.Decode(&stored); err != nil {
		return errors.New("tailscale settings file is corrupt")
	}
	if err := d.Decode(new(any)); err != io.EOF || stored.V != 1 || stored.Mode == nil || stored.Port == nil || stored.Provider == nil {
		return errors.New("tailscale settings file is corrupt or has an unsupported version")
	}
	next, err := config.ValidateTailscaleSettings(s.defaults, *stored.Mode, *stored.Port, *stored.Provider)
	if err != nil {
		return errors.New("tailscale settings file contains invalid fields")
	}
	if err := os.Chmod(s.path, 0600); err != nil {
		return errors.New("cannot secure tailscale settings file")
	}
	s.current, s.web = next, true
	return nil
}

// Effective returns the current settings and whether they came from the web.
// An unavailable store returns mode off even when TOML enables an entry.
func (s *Service) Effective() (config.TailscaleConfig, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, s.web
}

// Prepare validates an update against current, keeping the TOML command.
func Prepare(current config.TailscaleConfig, mode string, port int, provider string) (config.TailscaleConfig, error) {
	next, err := config.ValidateTailscaleSettings(current, mode, port, provider)
	if err != nil {
		return config.TailscaleConfig{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return next, nil
}

func (s *Service) unavailableLocked() error {
	if s.broken != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, s.broken)
	}
	return nil
}

// Unavailable reports the storage fault, if any.
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
func (s *Service) Save(next config.TailscaleConfig) error {
	if s == nil {
		return ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unavailableLocked(); err != nil {
		return err
	}
	normalized, err := Prepare(s.defaults, next.Mode, next.Port, next.Provider)
	if err != nil {
		return err
	}
	stored := storedFile{V: 1, Mode: &normalized.Mode, Port: &normalized.Port, Provider: &normalized.Provider}
	if err := statefile.WriteJSON(s.path, stored, statefile.Options{TempPattern: ".tailscale-settings-*", RequireDirSync: true}); err != nil {
		s.broken = errors.New("cannot persist tailscale settings")
		return s.unavailableLocked()
	}
	s.current, s.web = normalized, true
	return nil
}

// Reset removes the override durably and returns the TOML settings.
func (s *Service) Reset() (config.TailscaleConfig, error) {
	if s == nil {
		return config.TailscaleConfig{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unavailableLocked(); err != nil {
		return config.TailscaleConfig{}, err
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
		s.broken = errors.New("cannot remove tailscale settings")
		return config.TailscaleConfig{}, s.unavailableLocked()
	}
	s.current, s.web = s.defaults, false
	return s.current, nil
}

// Close releases the lock; later writes report the store unavailable.
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
	s.broken = errors.New("tailscale settings service is closed")
}
