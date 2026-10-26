// Package assemble is shared wiring for the cmd/ assembly roots: it turns
// configuration into service options so no other layer depends on both.
package assemble

import (
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/tailscale"
	"github.com/rainchan/webmux/internal/wsl"
)

// SessionOptions converts the loaded configuration for session.NewManager.
func SessionOptions(c config.Config) session.Options {
	return session.Options{
		ScrollbackBytes: c.ScrollbackBytes,
		DefaultCwd:      c.DefaultCwd,
		IdleAfter:       time.Duration(c.IdleAfterSeconds) * time.Second,
		PromptPatterns:  c.PromptPatterns,

		DisableAgentProtocol: !c.AgentProtocol,
		DisableBellAttention: !c.BellAttention,
	}
}

// TailscaleOptions converts [tailscale] for tailscale.New.
func TailscaleOptions(c config.Config) tailscale.Options {
	return tailscale.Options{
		Command:        c.Tailscale.Command,
		Provider:       c.Tailscale.Provider,
		IsWSL:          wsl.Detected,
		NetworkingMode: wsl.NetworkingMode,
	}
}
