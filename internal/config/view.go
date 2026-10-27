package config

// Field is safe diagnostic metadata. Never reflectively serialize Config:
// adding a field requires an explicit choice here and in the coverage test.
type Field struct {
	Value  any
	Source string
}

func (c *Config) SetSource(key, source string) {
	if c.Sources == nil {
		c.Sources = map[string]string{}
	}
	if source == "cli" {
		c.Sources["base:"+key] = c.Source(key)
	}
	c.Sources[key] = source
}
func (c Config) Source(key string) string {
	if s := c.Sources[key]; s != "" {
		return s
	}
	return "default"
}
func Fields(c Config) map[string]Field {
	values := map[string]any{
		"listen": c.Listen, "token": c.Token != "", "state_dir": c.StateDir, "default_cwd": c.DefaultCwd, "allowed_origins": append([]string{}, c.AllowedOrigins...),
		"scrollback_bytes": c.ScrollbackBytes, "idle_after_seconds": c.IdleAfterSeconds, "prompt_patterns": append([]string{}, c.PromptPatterns...), "agent_protocol": c.AgentProtocol, "bell_attention": c.BellAttention, "program_status": c.ProgramStatus,
		"tunnel.enabled": c.Tunnel.Enabled, "tunnel.token": c.Tunnel.Token != "", "tunnel.hostname": c.Tunnel.Hostname, "tunnel.cloudflared": c.Tunnel.Cloudflared, "tunnel.download_mirrors": append([]string{}, c.Tunnel.DownloadMirrors...),
		"tailscale.mode": c.Tailscale.Mode, "tailscale.port": c.Tailscale.Port, "tailscale.provider": c.Tailscale.Provider, "tailscale.command": c.Tailscale.Command,
		"push.enabled": c.Push.Enabled, "push.subject": c.Push.Subject, "push.ttl_seconds": c.Push.TTLSeconds, "push.kinds": append([]string{}, c.Push.Kinds...), "push.include_title": c.Push.IncludeTitle,
	}
	out := map[string]Field{}
	for key, value := range values {
		out[key] = Field{Value: value, Source: c.Source(key)}
	}
	return out
}

func (c Config) BaseSource(key string) string {
	if s := c.Sources["base:"+key]; s != "" {
		return s
	}
	return c.Source(key)
}
