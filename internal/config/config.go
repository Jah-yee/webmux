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

	"github.com/BurntSushi/toml"
	"github.com/rainchan/webmux/internal/attentionkind"
	"github.com/rainchan/webmux/internal/pathutil"
)

type Config struct {
	Listen           string       `toml:"listen"`
	Token            string       `toml:"token"`
	ScrollbackBytes  int          `toml:"scrollback_bytes"`
	DefaultCwd       string       `toml:"default_cwd"`
	StateDir         string       `toml:"state_dir"`
	Shell            string       `toml:"shell"`
	IdleAfterSeconds int          `toml:"idle_after_seconds"`
	PromptPatterns   []string     `toml:"prompt_patterns"`
	AgentProtocol    bool         `toml:"agent_protocol"`
	BellAttention    bool         `toml:"bell_attention"`
	AllowedOrigins   []string     `toml:"allowed_origins"`
	Presets          []Preset     `toml:"presets"`
	Tunnel           TunnelConfig `toml:"tunnel"`
	Push             PushConfig   `toml:"push"`
}

// Preset is a configured launch command.
type Preset struct {
	Name      string   `toml:"name"`
	Label     string   `toml:"label"`
	Cmd       []string `toml:"cmd"`
	AllowArgs bool     `toml:"allow_args"`
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

// NamedHostname returns the public hostname of a configured Named Tunnel, or
// "" when the instance has no Named Tunnel.
func (t TunnelConfig) NamedHostname() string {
	if !t.Named() {
		return ""
	}
	return t.Hostname
}

// PushConfig controls Web Push attention alerts.
type PushConfig struct {
	Enabled bool   `toml:"enabled"`
	Subject string `toml:"subject"`
	// TTLSeconds is how long a push service keeps an undelivered alert.
	TTLSeconds   int      `toml:"ttl_seconds"`
	Kinds        []string `toml:"kinds"`
	IncludeTitle bool     `toml:"include_title"`
}

const (
	DefaultPushSubject = "https://github.com/mzdluo123/webmux"
	// MaxPushTTLSeconds is the four-week ceiling push services accept.
	MaxPushTTLSeconds = 2419200
)

// DefaultPushKinds leaves out bell: a bare BEL is too weak to wake a phone.
func DefaultPushKinds() []string {
	return []string{"question", "permission", "error", "done", "notify"}
}

func (p *PushConfig) validate() error {
	if p.Subject == "" {
		p.Subject = DefaultPushSubject
	}
	if p.Kinds == nil {
		p.Kinds = DefaultPushKinds()
	}
	u, err := url.Parse(p.Subject)
	switch {
	case err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil:
	case err == nil && u.Scheme == "mailto" && strings.Contains(u.Opaque, "@"):
	default:
		return fmt.Errorf("push.subject %q must be an https:// URL or a mailto: address", p.Subject)
	}
	if p.TTLSeconds < 0 || p.TTLSeconds > MaxPushTTLSeconds {
		return fmt.Errorf("push.ttl_seconds %d must be between 0 and %d", p.TTLSeconds, MaxPushTTLSeconds)
	}
	seen := map[string]bool{}
	for _, kind := range p.Kinds {
		if !attentionkind.Kind(kind).Valid() {
			return fmt.Errorf("push.kinds: %q is not supported; use question, permission, error, done, notify or bell", kind)
		}
		if seen[kind] {
			return fmt.Errorf("push.kinds: %q is listed twice", kind)
		}
		seen[kind] = true
	}
	return nil
}

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

// bashrcCommand runs program through an interactive bash so it sees the
// environment and PATH from ~/.bashrc, which a service manager never loads.
// The trailing program is bash -c's $0; appended arguments reach it via "$@".
func bashrcCommand(program string) []string {
	return []string{"bash", "-ic", `exec ` + program + ` "$@"`, program}
}

func Load(path string, required bool) (Config, error) {
	c := Config{
		Listen:           "0.0.0.0:7681",
		ScrollbackBytes:  2097152,
		DefaultCwd:       "~",
		IdleAfterSeconds: 30,
		AgentProtocol:    true,
		BellAttention:    true,
		PromptPatterns: []string{
			`[$#%>❯›?:]\s*$`,
			`(?i)\(y/n\)|\[y/n\]|yes.*no|press enter`,
		},
		Presets: []Preset{
			{Name: "shell", Label: "Shell", Cmd: []string{}},
			{Name: "claude", Label: "Claude Code", Cmd: bashrcCommand("claude"), AllowArgs: true},
			{Name: "codex", Label: "Codex CLI", Cmd: bashrcCommand("codex"), AllowArgs: true},
			{Name: "omp", Label: "omp", Cmd: bashrcCommand("omp"), AllowArgs: true},
		},
		Push: PushConfig{
			Enabled:      true,
			Subject:      DefaultPushSubject,
			TTLSeconds:   86400,
			Kinds:        DefaultPushKinds(),
			IncludeTitle: true,
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
	cwd, err := pathutil.Expand(c.DefaultCwd)
	if err != nil {
		return err
	}
	c.DefaultCwd = cwd
	if c.StateDir == "" {
		c.StateDir = DefaultStateDir()
	}
	// Storage accessibility is a workspace capability, not a startup prerequisite.
	if expanded, expandErr := pathutil.Expand(c.StateDir); expandErr == nil {
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
	if err := c.Tunnel.validate(); err != nil {
		return err
	}
	return c.Push.validate()
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
		path, err := pathutil.Expand(t.Cloudflared)
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
