package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
)

func defaultConfigPath(home string) string {
	return filepath.Join(home, ".config", "webmux", "config.toml")
}

func TestLoadConfigCreatesDefaultConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	path := defaultConfigPath(home)

	c, err := loadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !c.ConfigCreated || !c.ConfigFound || c.ConfigExplicit {
		t.Fatalf("created=%v found=%v explicit=%v", c.ConfigCreated, c.ConfigFound, c.ConfigExplicit)
	}
	if c.ConfigPath != path || c.TokenGenerated || len(c.Token) != 43 {
		t.Fatalf("path=%q tokenGenerated=%v tokenLength=%d", c.ConfigPath, c.TokenGenerated, len(c.Token))
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("config file: %v %v", info, err)
	}
	info, err = os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("config directory: %v %v", info, err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	match := tokenValue.FindSubmatch(first)
	if match == nil || string(match[1]) != c.Token {
		t.Fatalf("generated token not persisted: %q", first)
	}
	if !bytes.Contains(first, []byte(`listen = "0.0.0.0:7681"`)) {
		t.Fatalf("generated config lost the default listen:\n%s", first)
	}
	loaded, err := config.Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("generated config does not validate: %v", err)
	}

	c.TunnelSettings.Close()
	again, err := loadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.TunnelSettings.Close)
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.ConfigCreated || !again.ConfigFound || again.Token != c.Token || !bytes.Equal(first, second) {
		t.Fatalf("restart rewrote config or changed token: created=%v token=%q", again.ConfigCreated, again.Token)
	}
}

func TestDefaultConfigWithFlagsKeepsOverridesPerProcess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	path := defaultConfigPath(home)

	c, err := loadConfig([]string{"--listen", "127.0.0.1:9000", "--token", "flag-token"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.TunnelSettings.Close)
	if !c.ConfigCreated || c.Listen != "127.0.0.1:9000" || c.Token != "flag-token" || c.TokenGenerated {
		t.Fatalf("flags were not applied: listen=%q token=%q created=%v", c.Listen, c.Token, c.ConfigCreated)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	match := tokenValue.FindSubmatch(content)
	if match == nil || len(match[1]) != 43 || string(match[1]) == "flag-token" {
		t.Fatalf("generated file did not keep its own random token: %q", content)
	}
	if !bytes.Contains(content, []byte(`listen = "0.0.0.0:7681"`)) {
		t.Fatalf("command-line listen was written to the generated file:\n%s", content)
	}
}

func TestExplicitMissingConfigIsNotCreated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	missing := filepath.Join(home, "custom.toml")
	if _, err := loadConfig([]string{"--config", missing}); err == nil {
		t.Fatal("explicit missing config was accepted")
	}
	for _, path := range []string{missing, defaultConfigPath(home)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s was created despite an explicit --config", path)
		}
	}
}

func TestExplicitConfigLeavesDefaultMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	custom := filepath.Join(home, "custom.toml")
	content := []byte("listen = '127.0.0.1:9000'\ntoken = 'custom-token'\n")
	if err := os.WriteFile(custom, content, 0600); err != nil {
		t.Fatal(err)
	}

	c, err := loadConfig([]string{"--config", custom})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.TunnelSettings.Close)
	if c.ConfigCreated || !c.ConfigFound || c.ConfigPath != custom || c.Token != "custom-token" {
		t.Fatalf("explicit config: created=%v found=%v path=%q token=%q", c.ConfigCreated, c.ConfigFound, c.ConfigPath, c.Token)
	}
	after, err := os.ReadFile(custom)
	if err != nil || !bytes.Equal(after, content) {
		t.Fatalf("explicit config changed: %v\n%s", err, after)
	}
	if _, err := os.Stat(defaultConfigPath(home)); !os.IsNotExist(err) {
		t.Fatal("explicit config created the default file")
	}
}

func TestExistingDefaultConfigIsNotRewritten(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	path := defaultConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("listen = '127.0.0.1:9100'\ntoken = 'stable-token'\n")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}

	c, err := loadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.TunnelSettings.Close)
	if c.ConfigCreated || !c.ConfigFound || c.Listen != "127.0.0.1:9100" || c.Token != "stable-token" {
		t.Fatalf("existing config: created=%v listen=%q token=%q", c.ConfigCreated, c.Listen, c.Token)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, content) {
		t.Fatalf("existing config was rewritten: %v\n%s", err, after)
	}
}

func TestEnsureDefaultConfigConcurrent(t *testing.T) {
	home := t.TempDir()
	path := defaultConfigPath(home)
	type result struct {
		created bool
		err     error
	}
	const callers = 8
	results := make(chan result, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			created, err := ensureDefaultConfig(path)
			results <- result{created, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	created := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent creation failed: %v", result.err)
		}
		if result.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("%d callers created the configuration, want 1", created)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	match := tokenValue.FindSubmatch(content)
	if match == nil || len(match[1]) != 43 {
		t.Fatalf("concurrent creation published an incomplete file: %q", content)
	}
	loaded, err := config.Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("concurrent creation published an invalid file: %v", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("staging files left behind: %v %v", leftovers, err)
	}
}

func TestLoadConfigFailsWhenDefaultPathIsUnwritable(t *testing.T) {
	root := t.TempDir()
	homeFile := filepath.Join(root, "home-is-a-file")
	if err := os.WriteFile(homeFile, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeFile)
	t.Setenv("XDG_STATE_HOME", "")
	if _, err := loadConfig(nil); err == nil {
		t.Fatal("unwritable default config path silently fell back to defaults")
	}
}

func TestImplicitDefaultPathMustBeAbsolute(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	if _, err := loadConfig(nil); err == nil {
		t.Fatal("relative default config path was used and may have been created in the working directory")
	}
}

func TestReadOnlyConfigCommandsDoNotCreate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	for _, args := range [][]string{
		{"config", "path"},
		{"config", "show"},
		{"config", "validate"},
	} {
		var stdout, stderr bytes.Buffer
		if code := dispatch(args, &stdout, &stderr); code != contract.ExitOK {
			t.Fatalf("%q: exit %d: %s", args, code, stderr.String())
		}
	}
	if _, err := os.Stat(defaultConfigPath(home)); !os.IsNotExist(err) {
		t.Fatal("a read-only config command created the default file")
	}
}

func TestLoadConfigFailsForReadOnlyHome(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("read-only directories do not stop root")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	configDir := filepath.Join(home, ".config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configDir, 0700) })
	if _, err := loadConfig(nil); err == nil {
		t.Fatal("read-only home silently fell back to defaults")
	}
}

func TestPrintConfigNotice(t *testing.T) {
	var out strings.Builder
	printConfigNotice(&out, serverRun{
		Config:        config.Config{Token: "secret-token"},
		ConfigPath:    "/home/me/.config/webmux/config.toml",
		ConfigCreated: true,
	})
	text := out.String()
	if !strings.Contains(text, "Created config:  /home/me/.config/webmux/config.toml") || !strings.Contains(text, "webmux status --json --reveal-token") {
		t.Fatalf("missing config notice: %q", text)
	}
	if strings.Contains(text, "secret-token") {
		t.Fatalf("config notice exposed the token: %q", text)
	}

	out.Reset()
	printConfigNotice(&out, serverRun{
		Config:     config.Config{Token: "secret-token"},
		ConfigPath: "/tmp/config.toml",
	})
	if text := out.String(); !strings.Contains(text, "Using config:") || !strings.Contains(text, "reveal-token") {
		t.Fatalf("missing existing config notice: %q", text)
	}

	out.Reset()
	printConfigNotice(&out, serverRun{
		Config:         config.Config{Token: "per-process"},
		ConfigPath:     "/tmp/config.toml",
		TokenGenerated: true,
	})
	if text := out.String(); strings.Contains(text, "Token:") {
		t.Fatalf("generated token notice is redundant: %q", text)
	}

	out.Reset()
	printConfigNotice(&out, serverRun{})
	if out.Len() != 0 {
		t.Fatalf("embedder without a config path printed: %q", out.String())
	}
}
