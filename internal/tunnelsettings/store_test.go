package tunnelsettings

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rainchan/webmux/internal/config"
)

func TestPrepareCredentialAndHostname(t *testing.T) {
	current := config.TunnelConfig{Enabled: true, Token: "private-credential", Hostname: "old.example.com", Cloudflared: "/configured/cloudflared"}
	empty, replacement := "", "replacement-credential"
	for _, row := range []struct {
		name, mode, hostname string
		token                *string
		valid                bool
	}{
		{"preserve", "named", "NEW.Example.COM.", nil, true},
		{"replace", "named", "new.example.com", &replacement, true},
		{"empty", "named", "new.example.com", &empty, false},
		{"missing hostname", "named", "", nil, false},
		{"unknown mode", "other", "", nil, false},
		{"scheme", "named", "https://example.com", nil, false},
		{"port", "named", "example.com:443", nil, false},
		{"wildcard", "named", "*.example.com", nil, false},
		{"unicode", "named", "中文.example.com", nil, false},
		{"space", "named", "a b.example.com", nil, false},
		{"tab", "named", "a\tb.example.com", nil, false},
		{"invalid DNS label", "named", "-bad.example.com", nil, false},
		{"punycode", "named", "xn--fiq228c.example.com", nil, true},
		{"quick clears", "quick", "ignored.example.com", &replacement, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			next, err := Prepare(current, row.mode, row.hostname, row.token)
			if (err == nil) != row.valid {
				t.Fatalf("validation error %v", err)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalid) || strings.Contains(err.Error(), current.Token) {
					t.Fatalf("unsafe validation error %v", err)
				}
				return
			}
			if next.Enabled != current.Enabled || next.Cloudflared != current.Cloudflared {
				t.Fatal("update changed operational configuration")
			}
			if row.mode == "quick" && (next.Token != "" || next.Hostname != "") {
				t.Fatal("quick did not clear credentials")
			}
			if row.mode == "named" && row.token == nil && next.Token != current.Token {
				t.Fatal("omitted credential was not preserved")
			}
		})
	}
	if _, err := Prepare(config.TunnelConfig{}, "named", "example.com", nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("missing credential accepted")
	}
}

func TestStoreAtomicPermissionsPrecedenceAndReset(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	defaults := config.TunnelConfig{Enabled: true, Token: "toml-secret", Hostname: "toml.example.com", Cloudflared: "/toml/cloudflared"}
	s := Open(dir, defaults)
	if err := s.Unavailable(); err != nil {
		t.Fatal(err)
	}
	got, web := s.Effective()
	if web || got.Token != defaults.Token || !got.Enabled {
		t.Fatal("missing file did not use TOML")
	}
	path := filepath.Join(dir, "tunnel.json")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("opening created settings file")
	}
	next := defaults
	next.Enabled, next.Hostname = false, "web.example.com"
	if err := s.Save(next); err != nil {
		t.Fatal(err)
	}
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := old.Close(); err != nil {
			t.Error(err)
		}
	}()
	next.Token = "replacement-secret"
	if err := s.Save(next); err != nil {
		t.Fatal(err)
	}
	before, _ := io.ReadAll(old)
	after, _ := os.ReadFile(path)
	if strings.Contains(string(before), next.Token) || !strings.Contains(string(after), next.Token) {
		t.Fatal("settings were not atomically replaced")
	}
	for name, perm := range map[string]os.FileMode{dir: 0700, path: 0600, filepath.Join(dir, "tunnel-config.lock"): 0600} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != perm {
			t.Fatalf("incorrect permissions on %s: %v", name, err)
		}
	}
	s.Close()
	s = Open(dir, defaults)
	defer s.Close()
	got, web = s.Effective()
	if !web || got.Enabled || got.Hostname != next.Hostname || got.Token != next.Token || got.Cloudflared != defaults.Cloudflared {
		t.Fatal("web settings did not survive reopen with TOML operational fields")
	}
	reset, err := s.Reset()
	if err != nil || !reset.Enabled || reset.Token != defaults.Token {
		t.Fatal("reset did not restore TOML")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reset did not remove file")
	}
}

func TestFaultsFailClosedWithoutOverwriting(t *testing.T) {
	for _, fault := range []string{"corrupt", "unsupported", "unreadable", "dangling", "locked"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "tunnel.json")
			data := []byte(`{"v":1,"enabled":false,"mode":"quick","hostname":"","sensitive-secret":"value"}`)
			switch fault {
			case "corrupt":
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "unsupported":
				if err := os.WriteFile(path, []byte(`{"v":2,"enabled":true,"mode":"quick","hostname":""}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "dangling":
				if err := os.Symlink(filepath.Join(dir, "missing"), path); err != nil {
					t.Fatal(err)
				}
			case "locked":
				first := Open(dir, config.TunnelConfig{})
				defer first.Close()
			}
			s := Open(dir, config.TunnelConfig{Enabled: true})
			defer s.Close()
			if err := s.Unavailable(); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "sensitive-secret") {
				t.Fatalf("fault not safely reported: %v", err)
			}
			if cfg, _ := s.Effective(); cfg.Enabled {
				t.Fatal("fault fell back to enabled TOML")
			}
			if err := s.Save(config.TunnelConfig{}); !errors.Is(err, ErrUnavailable) {
				t.Fatal("fault allowed save")
			}
			if _, err := s.Reset(); !errors.Is(err, ErrUnavailable) {
				t.Fatal("fault allowed reset")
			}
			if fault == "corrupt" {
				got, _ := os.ReadFile(path)
				if string(got) != string(data) {
					t.Fatal("corrupt file changed")
				}
			}
		})
	}
}

func TestConcurrentSaveAndFailedWrite(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir, config.TunnelConfig{})
	defer s.Close()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if err := s.Save(config.TunnelConfig{Enabled: i%2 == 0}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	before, _ := s.Effective()
	if err := os.Remove(filepath.Join(dir, "tunnel.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "tunnel.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(config.TunnelConfig{Enabled: !before.Enabled}); !errors.Is(err, ErrUnavailable) {
		t.Fatal("write failure not unavailable")
	}
	after, _ := s.Effective()
	if after.Enabled != before.Enabled {
		t.Fatal("failed save changed effective settings")
	}
}
