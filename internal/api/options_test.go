package api

import (
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/files"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

// testSessionOptions mirrors cmd/internal/assemble, which tests outside cmd/
// cannot import.
func testSessionOptions(c config.Config) session.Options {
	return session.Options{
		ScrollbackBytes:      c.ScrollbackBytes,
		DefaultCwd:           c.DefaultCwd,
		Presets:              testPresets(c.Presets),
		IdleAfter:            time.Duration(c.IdleAfterSeconds) * time.Second,
		PromptPatterns:       c.PromptPatterns,
		DisableAgentProtocol: !c.AgentProtocol,
		DisableBellAttention: !c.BellAttention,
	}
}

func testPresets(presets []config.Preset) []session.Preset {
	out := make([]session.Preset, 0, len(presets))
	for _, p := range presets {
		out = append(out, session.Preset{Name: p.Name, Label: p.Label, Cmd: p.Cmd, AllowArgs: p.AllowArgs})
	}
	return out
}

// testDeps supplies the workspace, code view and files services a test
// leaves unset. The workspace resolves configs from d.Launch.
func testDeps(t testing.TB, d Deps) Deps {
	t.Helper()
	if d.Workspace == nil {
		d.Workspace = workspace.New(t.TempDir(), d.Manager, d.Launch, d.Config.DefaultCwd)
		t.Cleanup(d.Workspace.Close)
	}
	if d.Code == nil {
		d.Code = codeview.New(d.Manager)
		t.Cleanup(d.Code.Close)
	}
	if d.Files == nil {
		d.Files = files.New(d.Config.DefaultCwd)
	}
	return d
}
