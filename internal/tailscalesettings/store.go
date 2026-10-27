// Package tailscalesettings owns the private, persistent web override of the
// [tailscale] mode, port and provider. It never applies an entry itself;
// startup callers must honor its fail-closed result.
package tailscalesettings

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"

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
	store             *statefile.Handle[storedFile]
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

func decodeStored(f storedFile, defaults config.TailscaleConfig) (config.TailscaleConfig, error) {
	if f.V != 1 || f.Mode == nil || f.Port == nil || f.Provider == nil {
		return config.TailscaleConfig{}, errors.New("incomplete tailscale settings")
	}
	return config.ValidateTailscaleSettings(defaults, *f.Mode, *f.Port, *f.Provider)
}
func (s *Service) load() error {
	var f storedFile
	var err error
	s.store, f, err = statefile.Open[storedFile](s.dir, statefile.TailscaleSettings, func(f storedFile) error { _, err := decodeStored(f, s.defaults); return err })
	if statefile.IsKind(err, statefile.Missing) {
		return nil
	}
	if err != nil {
		return err
	}
	s.current, err = decodeStored(f, s.defaults)
	s.web = err == nil
	return err
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
		return fmt.Errorf("%w: %w", ErrUnavailable, s.broken)
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
	if err := s.store.Commit(stored); err != nil {
		s.broken = err
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
	err := s.store.Remove()
	if err != nil {
		s.broken = err
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
	s.store.Close()
	s.broken = errors.New("tailscale settings service is closed")
}
