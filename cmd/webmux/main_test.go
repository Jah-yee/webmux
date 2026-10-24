package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rainchan/webmux/internal/contract"
)

// The tray parses `version --json` before anything else; a broken
// configuration must not matter, and the text form stays stable for older
// trays that parse "webmux <version>".
func TestVersionCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	broken := filepath.Join(dir, ".config", "webmux", "config.toml")
	if err := os.MkdirAll(filepath.Dir(broken), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(broken, []byte("listen = [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	version = "0.3.0"
	t.Cleanup(func() { version = "dev" })
	run := func(args ...string) (int, string, string) {
		var out, stderr bytes.Buffer
		code := dispatch(args, &out, &stderr)
		return code, out.String(), stderr.String()
	}
	code, out, _ := run("version", "--json")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); code != contract.ExitOK || err != nil {
		t.Fatalf("version --json: exit %d %q %v", code, out, err)
	}
	if want := map[string]any{"v": float64(contract.V), "version": "0.3.0", "contract": float64(contract.V)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("version --json = %v, want %v", got, want)
	}
	for _, args := range [][]string{{"version"}, {"--version"}} {
		if code, out, _ := run(args...); code != contract.ExitOK || out != "webmux 0.3.0\n" {
			t.Fatalf("%q: exit %d %q", args, code, out)
		}
	}
	for _, args := range [][]string{{"version", "--yaml"}, {"version", "extra"}} {
		if code, out, _ := run(args...); code != contract.ExitUsage || out != "" {
			t.Fatalf("%q: exit %d %q", args, code, out)
		}
	}
}

func TestConfigurationPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/sh")
	file := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(file, []byte("listen = '127.0.0.1:9000'\ntoken = 'file-token'\nshell = '/bin/bash'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name                 string
		args                 []string
		listen, token, shell string
		generated            bool
	}{
		{"defaults", nil, "0.0.0.0:7681", "", "/bin/sh", true},
		{"file", []string{"--config", file}, "127.0.0.1:9000", "file-token", "/bin/bash", false},
		{"flags", []string{"--config", file, "--listen", "127.0.0.1:9001", "--token", "flag-token"}, "127.0.0.1:9001", "flag-token", "/bin/bash", false},
		{"explicit empty token on loopback", []string{"--config", file, "--token="}, "127.0.0.1:9000", "", "/bin/bash", false},
		{"explicit empty token on localhost", []string{"--config", file, "--listen", "localhost:9002", "--token="}, "localhost:9002", "", "/bin/bash", false},
		{"explicit empty token on IPv6 wildcard", []string{"--config", file, "--listen", "[::]:9003", "--token="}, "[::]:9003", "", "/bin/bash", true},
		{"explicit empty token on VPN address", []string{"--config", file, "--listen", "100.64.0.5:9004", "--token="}, "100.64.0.5:9004", "", "/bin/bash", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			c, err := loadConfig(row.args)
			if err != nil {
				t.Fatal(err)
			}
			if c.TokenGenerated != row.generated || row.generated && len(c.Token) < minTunnelTokenLength {
				t.Fatalf("token generated=%v len=%d, want generated=%v", c.TokenGenerated, len(c.Token), row.generated)
			}
			if !row.generated && c.Token != row.token {
				t.Fatalf("token %q, want %q", c.Token, row.token)
			}
			if c.Listen != row.listen || c.Shell != row.shell {
				t.Fatalf("configuration: listen=%q shell=%q", c.Listen, c.Shell)
			}
			if c.DefaultCwd != dir || !reflect.DeepEqual(c.Presets[0].Cmd, []string{row.shell, "-l", "-i"}) {
				t.Fatalf("unresolved defaults: cwd=%q shell argv=%q", c.DefaultCwd, c.Presets[0].Cmd)
			}
		})
	}
}

func TestExplicitConfigurationAndOverridesAreValidated(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadConfig([]string{"--config", filepath.Join(dir, "missing.toml")}); err == nil {
		t.Fatal("explicit missing config was ignored")
	}
	file := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(file, []byte("listen = 'invalid'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig([]string{"--config", file}); err == nil {
		t.Fatal("invalid file listen accepted")
	}
	if _, err := loadConfig([]string{"--config", file, "--listen", "127.0.0.1:9000"}); err != nil {
		t.Fatalf("file validated before flag override: %v", err)
	}
	if _, err := loadConfig([]string{"--config", file, "--listen="}); err == nil {
		t.Fatal("explicit empty listen accepted")
	}
}
