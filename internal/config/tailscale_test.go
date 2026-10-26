package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTailscaleConfiguration(t *testing.T) {
	for _, row := range []struct {
		name string
		cfg  TailscaleConfig
		want string
	}{
		{"default off", TailscaleConfig{}, ""},
		{"serve any port", TailscaleConfig{Mode: "serve", Port: 7681}, ""},
		{"funnel 8443", TailscaleConfig{Mode: "funnel", Port: 8443}, ""},
		{"bad mode", TailscaleConfig{Mode: "public"}, "tailscale.mode"},
		{"bad funnel port", TailscaleConfig{Mode: "funnel", Port: 9000}, "tailscale.port"},
		{"bad port", TailscaleConfig{Mode: "serve", Port: 70000}, "tailscale.port"},
		{"bad provider", TailscaleConfig{Mode: "serve", Provider: "windows7"}, "tailscale.provider"},
	} {
		t.Run(row.name, func(t *testing.T) {
			cfg := row.cfg
			err := cfg.validate()
			if row.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), row.want) {
				t.Fatalf("validate() = %v, want %q", err, row.want)
			}
		})
	}
}

func TestTailscaleDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "absent.toml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Tailscale.Mode != TailscaleOff || c.Tailscale.Provider != "auto" || c.Tailscale.Enabled() {
		t.Fatalf("defaults = %+v", c.Tailscale)
	}
}
