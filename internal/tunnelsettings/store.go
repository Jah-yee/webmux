// Package tunnelsettings owns private, persistent tunnel settings. It never
// grants public access; startup callers must honor its fail-closed result.
package tunnelsettings

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
	store             *statefile.Handle[storedFile]
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
		s.dir, s.path = dir, filepath.Join(dir, "tunnel-settings.json")
		err = s.load()
	}
	if err != nil {
		s.broken = err
		s.current.Enabled = false
	}
	return s
}

func decodeStored(f storedFile, defaults config.TunnelConfig) (config.TunnelConfig, error) {
	if f.V != 1 || f.Enabled == nil || f.Hostname == nil {
		return config.TunnelConfig{}, errors.New("incomplete tunnel settings")
	}
	next, err := config.ValidateTunnelSettings(f.Mode, *f.Hostname, f.Token)
	if err != nil || (f.Mode == "quick" && (f.Token != "" || *f.Hostname != "")) {
		return config.TunnelConfig{}, errors.New("invalid tunnel settings")
	}
	next.Enabled = *f.Enabled
	next.Cloudflared = defaults.Cloudflared
	next.DownloadMirrors = defaults.DownloadMirrors
	return next, nil
}
func (s *Service) load() error {
	var f storedFile
	var err error
	s.store, f, err = statefile.Open[storedFile](s.dir, statefile.Tunnel, func(f storedFile) error { _, err := decodeStored(f, s.defaults); return err })
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
		return fmt.Errorf("%w: %w", ErrUnavailable, s.broken)
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
	if err := s.store.Commit(stored); err != nil {
		s.broken = err
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
	err := s.store.Remove()
	if err != nil {
		s.broken = err
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
	s.store.Close()
	s.broken = errors.New("tunnel settings service is closed")
}
