package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiagnosticFieldsCoverConfiguration(t *testing.T) {
	fields := Fields(Config{})
	var visit func(reflect.Type, string)
	visit = func(typ reflect.Type, prefix string) {
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			key := f.Tag.Get("toml")
			if key == "-" {
				continue
			}
			if f.Type.Kind() == reflect.Struct {
				visit(f.Type, prefix+key+".")
				continue
			}
			if _, ok := fields[prefix+key]; !ok {
				t.Errorf("new config field needs an explicit safe diagnostic projection: %s%s", prefix, key)
			}
		}
	}
	visit(reflect.TypeFor[Config](), "")
	c := Config{Token: "secret-login", Tunnel: TunnelConfig{Token: "secret-tunnel"}}
	if Fields(c)["token"].Value != true || Fields(c)["tunnel.token"].Value != true {
		t.Fatal("credentials must be booleans")
	}
}
func TestExplicitDefaultStillHasTOMLSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("listen = '127.0.0.1:7681'\n[tunnel]\nenabled = false\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Source("listen") != "toml" || c.Source("tunnel.enabled") != "toml" || c.Source("tunnel.token") != "default" {
		t.Fatalf("sources: %v", c.Sources)
	}
	c.SetSource("listen", "cli")
	if Fields(c)["listen"].Source != "cli" || c.BaseSource("listen") != "toml" {
		t.Fatal("CLI precedence lost")
	}
}
