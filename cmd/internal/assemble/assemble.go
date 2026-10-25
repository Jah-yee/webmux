// Package assemble is shared wiring for the cmd/ assembly roots: it turns
// configuration into service options so no other layer depends on both.
package assemble

import (
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
)

// SessionOptions converts the loaded configuration for session.NewManager.
func SessionOptions(c config.Config) session.Options {
	presets := make([]session.Preset, 0, len(c.Presets))
	for _, p := range c.Presets {
		presets = append(presets, session.Preset{Name: p.Name, Label: p.Label, Cmd: p.Cmd, AllowArgs: p.AllowArgs})
	}
	return session.Options{
		ScrollbackBytes: c.ScrollbackBytes,
		DefaultCwd:      c.DefaultCwd,
		Presets:         presets,
		IdleAfter:       time.Duration(c.IdleAfterSeconds) * time.Second,
		PromptPatterns:  c.PromptPatterns,

		DisableAgentProtocol: !c.AgentProtocol,
		DisableBellAttention: !c.BellAttention,
	}
}
