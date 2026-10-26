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
	Listen           string          `toml:"listen"`
	Token            string          `toml:"token"`
	ScrollbackBytes  int             `toml:"scrollback_bytes"`
	DefaultCwd       string          `toml:"default_cwd"`
	StateDir         string          `toml:"state_dir"`
	IdleAfterSeconds int             `toml:"idle_after_seconds"`
	PromptPatterns   []string        `toml:"prompt_patterns"`
	AgentProtocol    bool            `toml:"agent_protocol"`
	BellAttention    bool            `toml:"bell_attention"`
	ProgramStatus    bool            `toml:"program_status"`
	AllowedOrigins   []string        `toml:"allowed_origins"`
	Tunnel           TunnelConfig    `toml:"tunnel"`
	Tailscale        TailscaleConfig `toml:"tailscale"`
	Push             PushConfig      `toml:"push"`
}

// TailscaleConfig selects the managed Tailscale Serve or Funnel entry point.
// The default mode "off" never runs a modifying Tailscale command.
type TailscaleConfig struct {
	// Mode is off, serve or funnel.
	Mode string `toml:"mode"`
	// Port is 0 for automatic selection; Serve allows any 1-65535 port and
	// Funnel only 443, 8443 or 10000.
	Port int `toml:"port"`
	// Provider is auto, linux or windows. Windows must be chosen explicitly
	// on WSL; auto never manages the Windows host.
	Provider string `toml:"provider"`
	// Command overrides CLI discovery.
	Command string `toml:"command"`
}

const (
	TailscaleOff    = "off"
	TailscaleServe  = "serve"
	TailscaleFunnel = "funnel"
)

// Enabled reports whether the configuration asks for a managed entry point.
func (t TailscaleConfig) Enabled() bool {
	return t.Mode == TailscaleServe || t.Mode == TailscaleFunnel
}

// ValidateTailscaleSettings checks the web-editable [tailscale] fields and
// returns them normalized; Command always stays the TOML value.
func ValidateTailscaleSettings(current TailscaleConfig, mode string, port int, provider string) (TailscaleConfig, error) {
	if mode == "" {
		return TailscaleConfig{}, errors.New("tailscale.mode is required")
	}
	next := TailscaleConfig{Mode: mode, Port: port, Provider: provider}
	if err := next.validate(); err != nil {
		return TailscaleConfig{}, err
	}
	next.Command = current.Command
	return next, nil
}

func (t *TailscaleConfig) validate() error {
	switch t.Mode {
	case "", TailscaleOff, TailscaleServe, TailscaleFunnel:
	default:
		return fmt.Errorf("tailscale.mode %q must be off, serve or funnel", t.Mode)
	}
	if t.Mode == "" {
		t.Mode = TailscaleOff
	}
	switch t.Provider {
	case "", "auto":
		t.Provider = "auto"
	case "linux", "windows":
	default:
		return fmt.Errorf("tailscale.provider %q must be auto, linux or windows", t.Provider)
	}
	if t.Port < 0 || t.Port > 65535 {
		return fmt.Errorf("tailscale.port %d must be between 0 and 65535", t.Port)
	}
	if t.Mode == TailscaleFunnel {
		switch t.Port {
		case 0, 443, 8443, 10000:
		default:
			return fmt.Errorf("tailscale.port %d is not supported by Funnel; use 0, 443, 8443 or 10000", t.Port)
		}
	}
	if t.Command != "" {
		path, err := pathutil.Expand(t.Command)
		if err != nil {
			return fmt.Errorf("tailscale.command: %w", err)
		}
		t.Command = path
	}
	return nil
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

// ValidateTunnelSettings validates the web-managed subset without exposing
// submitted values in errors. Operational fields remain owned by TOML.
func ValidateTunnelSettings(mode, hostname, token string) (TunnelConfig, error) {
	switch mode {
	case "quick":
		return TunnelConfig{}, nil
	case "named":
		if token == "" {
			return TunnelConfig{}, errors.New("token is required for a named tunnel")
		}
		if hostname == "" {
			return TunnelConfig{}, errors.New("hostname is required for a named tunnel")
		}
		t := TunnelConfig{Enabled: true, Token: token, Hostname: hostname}
		if err := t.validate(); err != nil {
			return TunnelConfig{}, errors.New("hostname must be a bare ASCII/punycode hostname without a scheme, port, path, wildcard or whitespace")
		}
		for _, label := range strings.Split(t.Hostname, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return TunnelConfig{}, errors.New("hostname contains an invalid DNS label")
			}
			for _, ch := range label {
				if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
					return TunnelConfig{}, errors.New("hostname contains an invalid DNS label")
				}
			}
		}
		if len(t.Hostname) > 253 {
			return TunnelConfig{}, errors.New("hostname is too long")
		}
		return t, nil
	default:
		return TunnelConfig{}, errors.New("mode must be quick or named")
	}
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

func Load(path string, required bool) (Config, error) {
	c := Config{
		Listen:           "0.0.0.0:7681",
		ScrollbackBytes:  2097152,
		DefaultCwd:       "~",
		IdleAfterSeconds: 30,
		AgentProtocol:    true,
		BellAttention:    true,
		ProgramStatus:    true,
		PromptPatterns: []string{
			`[$#%>❯›?:]\s*$`,
			`(?i)\(y/n\)|\[y/n\]|yes.*no|press enter`,
		},
		Push: PushConfig{
			Enabled:      true,
			Subject:      DefaultPushSubject,
			TTLSeconds:   86400,
			Kinds:        DefaultPushKinds(),
			IncludeTitle: true,
		},
		Tailscale: TailscaleConfig{Mode: TailscaleOff, Provider: "auto"},
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
	for _, pattern := range c.PromptPatterns {
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("prompt pattern: %w", err)
		}
	}
	if err := c.Tunnel.validate(); err != nil {
		return err
	}
	if err := c.Tailscale.validate(); err != nil {
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
