package tailscalesettings

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rainchan/webmux/internal/config"
)

var tomlDefaults = config.TailscaleConfig{Mode: "serve", Port: 8443, Provider: "auto", Command: "/toml/tailscale"}

func TestSaveOverridesTOMLAcrossRestartAndResetRestores(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir, tomlDefaults)
	if got, web := s.Effective(); got != tomlDefaults || web {
		t.Fatalf("fresh store = %+v web=%v", got, web)
	}
	if err := s.Save(config.TailscaleConfig{Mode: "funnel", Port: 10000, Provider: "linux", Command: "/web/ignored"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("settings file %v %v", info, err)
	}

	s = Open(dir, tomlDefaults)
	defer s.Close()
	got, web := s.Effective()
	want := config.TailscaleConfig{Mode: "funnel", Port: 10000, Provider: "linux", Command: "/toml/tailscale"}
	if got != want || !web {
		t.Fatalf("reopened = %+v web=%v; want %+v from web with the TOML command", got, web, want)
	}
	reset, err := s.Reset()
	if err != nil || reset != tomlDefaults {
		t.Fatalf("reset = %+v %v", reset, err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !os.IsNotExist(err) {
		t.Fatalf("override survived reset: %v", err)
	}
}

func TestInvalidUpdatesAreRejectedWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir, tomlDefaults)
	defer s.Close()
	for _, bad := range []config.TailscaleConfig{
		{Mode: "", Provider: "auto"},
		{Mode: "public", Provider: "auto"},
		{Mode: "funnel", Port: 8444, Provider: "auto"},
		{Mode: "serve", Port: 70000, Provider: "auto"},
		{Mode: "serve", Provider: "macos"},
	} {
		if err := s.Save(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("Save(%+v) = %v; want ErrInvalid", bad, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !os.IsNotExist(err) {
		t.Fatalf("invalid update wrote a file: %v", err)
	}
}

func TestFaultsFailClosedWithoutOverwriting(t *testing.T) {
	for name, content := range map[string]string{
		"corrupt":     "{not json",
		"unsupported": `{"v":2,"mode":"serve","port":0,"provider":"auto"}`,
		"truncated":   `{"v":1,"mode":"serve"}`,
		"invalid":     `{"v":1,"mode":"funnel","port":9000,"provider":"auto"}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, fileName)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			s := Open(dir, tomlDefaults)
			defer s.Close()
			if got, _ := s.Effective(); got.Mode != config.TailscaleOff {
				t.Fatalf("faulty store kept mode %q; want off", got.Mode)
			}
			if err := s.Save(tomlDefaults); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("Save on faulty store = %v", err)
			}
			if data, _ := os.ReadFile(path); string(data) != content {
				t.Fatalf("faulty file overwritten: %q", data)
			}
		})
	}
}

func TestSecondInstanceIsLockedOut(t *testing.T) {
	dir := t.TempDir()
	first := Open(dir, tomlDefaults)
	defer first.Close()
	second := Open(dir, tomlDefaults)
	defer second.Close()
	if err := second.Unavailable(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second instance = %v", err)
	}
	if got, _ := second.Effective(); got.Mode != config.TailscaleOff {
		t.Fatalf("locked-out instance kept mode %q", got.Mode)
	}
}
