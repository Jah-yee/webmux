package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/rainchan/webmux/internal/session"
)

type Config struct {
	Listen           string           `toml:"listen"`
	Token            string           `toml:"token"`
	ScrollbackBytes  int              `toml:"scrollback_bytes"`
	DefaultCwd       string           `toml:"default_cwd"`
	StateDir         string           `toml:"state_dir"`
	Shell            string           `toml:"shell"`
	IdleAfterSeconds int              `toml:"idle_after_seconds"`
	PromptPatterns   []string         `toml:"prompt_patterns"`
	AgentProtocol    bool             `toml:"agent_protocol"`
	BellAttention    bool             `toml:"bell_attention"`
	AllowedOrigins   []string         `toml:"allowed_origins"`
	Presets          []session.Preset `toml:"presets"`
	Tunnel           TunnelConfig     `toml:"tunnel"`
}

// TunnelConfig selects a Cloudflare Tunnel. An empty Token means a Quick
// Tunnel; a nonempty Token runs the Named Tunnel published at Hostname.
type TunnelConfig struct {
	Enabled     bool   `toml:"enabled"`
	Token       string `toml:"token"`
	Hostname    string `toml:"hostname"`
	Cloudflared string `toml:"cloudflared"`
	// DownloadMirrors replaces the built-in mirror prefixes when non-nil; an
	// empty list downloads from GitHub only.
	DownloadMirrors []string `toml:"download_mirrors"`
}

// Named reports whether the tunnel runs with Cloudflare credentials.
func (t TunnelConfig) Named() bool { return t.Token != "" }

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "webmux", "config.toml")
}
func DefaultStateDir() string {
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, "webmux")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "webmux")
}
func Load(path string, required bool) (Config, error) {
	c := Config{
		Listen:           "127.0.0.1:7681",
		ScrollbackBytes:  2097152,
		DefaultCwd:       "~",
		IdleAfterSeconds: 30,
		AgentProtocol:    true,
		BellAttention:    true,
		PromptPatterns: []string{
			`[$#%>❯›?:]\s*$`,
			`(?i)\(y/n\)|\[y/n\]|yes.*no|press enter`,
		},
		Presets: []session.Preset{
			{Name: "shell", Label: "Shell", Cmd: []string{}},
			{Name: "claude", Label: "Claude Code", Cmd: []string{"claude"}, AllowArgs: true},
			{Name: "codex", Label: "Codex CLI", Cmd: []string{"codex"}, AllowArgs: true},
			{Name: "omp", Label: "omp", Cmd: []string{"bash", "-ic", `exec omp "$@"`, "omp"}, AllowArgs: true},
		},
	}
	if _, err := toml.DecodeFile(path, &c); err != nil {
		if required || !errors.Is(err, os.ErrNotExist) {
			return c, err
		}
	}
	return c, nil
}
func (c *Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	if c.ScrollbackBytes < 1 || c.IdleAfterSeconds < 2 {
		return errors.New("scrollback_bytes must be positive and idle_after_seconds >= 2")
	}
	cwd, err := session.ExpandPath(c.DefaultCwd)
	if err != nil {
		return err
	}
	c.DefaultCwd = cwd
	if c.StateDir == "" {
		c.StateDir = DefaultStateDir()
	}
	// Storage accessibility is a workspace capability, not a startup prerequisite.
	if expanded, expandErr := session.ExpandPath(c.StateDir); expandErr == nil {
		c.StateDir = expanded
	}
	if c.Shell == "" {
		c.Shell = os.Getenv("SHELL")
	}
	if c.Shell == "" {
		c.Shell = "/bin/bash"
	}
	seen := map[string]bool{}
	for i := range c.Presets {
		p := &c.Presets[i]
		if p.Name == "" || seen[p.Name] {
			return errors.New("preset names must be nonempty and unique")
		}
		if p.Name == "custom" {
			return errors.New("preset name custom is reserved for custom commands")
		}
		seen[p.Name] = true
		if p.Name == "shell" && len(p.Cmd) == 0 {
			p.Cmd = []string{c.Shell, "-l", "-i"}
		}
		if len(p.Cmd) == 0 || p.Cmd[0] == "" {
			return fmt.Errorf("preset %s has no command", p.Name)
		}
	}
	for _, pattern := range c.PromptPatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("prompt pattern: %w", err)
		}
	}
	return c.Tunnel.validate()
}
func (t *TunnelConfig) validate() error {
	if t.Hostname != "" {
		host := strings.TrimRight(strings.ToLower(t.Hostname), ".")
		if strings.Contains(host, "*") {
			return fmt.Errorf("tunnel.hostname %q must be the concrete hostname Cloudflare forwards, not a wildcard", t.Hostname)
		}
		for _, r := range host {
			if r > 127 {
				return fmt.Errorf("tunnel.hostname %q must use the ASCII/punycode form", t.Hostname)
			}
		}
		u, err := url.Parse("https://" + host)
		if err != nil || host == "" || strings.ContainsAny(host, ":/@?#[] ") || u.Hostname() != host {
			return fmt.Errorf("tunnel.hostname %q must be a bare hostname such as term.example.com", t.Hostname)
		}
		t.Hostname = host
	}
	if t.Enabled && t.Named() && t.Hostname == "" {
		return errors.New("tunnel.hostname is required when tunnel.token is set")
	}
	if t.Cloudflared != "" {
		path, err := session.ExpandPath(t.Cloudflared)
		if err != nil {
			return fmt.Errorf("tunnel.cloudflared: %w", err)
		}
		t.Cloudflared = path
	}
	for i, mirror := range t.DownloadMirrors {
		mirror = strings.TrimRight(mirror, "/")
		u, err := url.Parse(mirror)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(mirror, "?#") {
			return fmt.Errorf("tunnel.download_mirrors: %q must be an https:// prefix without credentials, query, or fragment", t.DownloadMirrors[i])
		}
		t.DownloadMirrors[i] = mirror
	}
	return nil
}
func (c Config) SessionOptions() session.Options {
	return session.Options{
		ScrollbackBytes: c.ScrollbackBytes,
		DefaultCwd:      c.DefaultCwd,
		Presets:         c.Presets,
		IdleAfter:       time.Duration(c.IdleAfterSeconds) * time.Second,
		PromptPatterns:  c.PromptPatterns,

		DisableAgentProtocol: !c.AgentProtocol,
		DisableBellAttention: !c.BellAttention,
	}
}
