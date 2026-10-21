package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rainchan/webmux/deploy"
	"github.com/rainchan/webmux/internal/contract"
)

func runConfig(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := dispatch(append([]string{"config"}, args...), &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestConfigShowAndPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	defaultPath := filepath.Join(home, ".config", "webmux", "config.toml")

	stdout, _, code := runConfig(t, "path", "--json")
	var where contract.ConfigPathV1
	if err := json.Unmarshal([]byte(stdout), &where); err != nil || code != 0 || where.Path != defaultPath || where.Exists {
		t.Fatalf("path %d %s", code, stdout)
	}
	stdout, _, code = runConfig(t, "show", "--json")
	var shown contract.ConfigV1
	if err := json.Unmarshal([]byte(stdout), &shown); err != nil || code != 0 || shown.Exists || shown.Listen != "127.0.0.1:7681" || shown.TokenConfigured || shown.StateDir != filepath.Join(home, ".local", "state", "webmux") || len(shown.Presets) != 4 {
		t.Fatalf("defaults %d %s", code, stdout)
	}
	if _, _, code := runConfig(t, "show", "--config", filepath.Join(home, "missing.toml")); code != 1 {
		t.Fatalf("explicit missing file exit %d", code)
	}

	path := filepath.Join(home, "custom.toml")
	content := "token = 'very-secret-token'\nlisten = '0.0.0.0:9000'\n[[presets]]\nname = 'tool'\nlabel = 'Tool'\ncmd = ['tool', '--flag value']\nallow_args = true\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"show", "--config", "~/custom.toml", "--json"}, {"show", "--config", path}} {
		stdout, stderr, code := runConfig(t, args...)
		if code != 0 || strings.Contains(stdout+stderr, "very-secret-token") {
			t.Fatalf("%v: %d %s %s", args, code, stdout, stderr)
		}
		if args[len(args)-1] == "--json" {
			var custom contract.ConfigV1
			if err := json.Unmarshal([]byte(stdout), &custom); err != nil || !custom.TokenConfigured || custom.Path != path || len(custom.Presets) != 1 ||
				custom.Presets[0].Argv[1] != "--flag value" || !custom.Presets[0].AllowArgs {
				t.Fatalf("custom %s", stdout)
			}
		}
	}
}

func TestConfigValidate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	for name, row := range map[string]struct {
		content, want string
	}{
		"syntax":         {"listen = '127.0.0.1:7681'\nscrollback_bytes = 10\ntoken = \"unterminated\n", ":3:"},
		"invalid preset": {"[[presets]]\nname = 'custom'\ncmd = ['x']\n", "reserved"},
		"short tunnel":   {"token = 'short'\n[tunnel]\nenabled = true\n", "at least 43"},
	} {
		path := filepath.Join(dir, name+".toml")
		if err := os.WriteFile(path, []byte(row.content), 0600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(path)
		_, stderr, code := runConfig(t, "validate", "--config", path)
		if code != 1 || !strings.Contains(stderr, row.want) {
			t.Errorf("%s: %d %q", name, code, stderr)
		}
		stdout, _, _ := runConfig(t, "validate", "--config", path, "--json")
		var result contract.ValidateV1
		if err := json.Unmarshal([]byte(stdout), &result); err != nil || result.OK || len(result.Errors) != 1 {
			t.Errorf("%s JSON: %s", name, stdout)
		}
		if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
			t.Errorf("%s: validate modified the file", name)
		}
	}
}

var tokenValue = regexp.MustCompile(`(?m)^token = "([A-Za-z0-9_-]+)"$`)

func TestConfigInit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	path := filepath.Join(dir, "nested", "webmux", "config.toml")

	stdout, stderr, code := runConfig(t, "init", "--config", path, "--listen", "100.64.0.5:7681")
	if code != 0 || stdout != path+"\n" || stderr != "" {
		t.Fatalf("init %d %q %q", code, stdout, stderr)
	}
	for file, mode := range map[string]os.FileMode{path: 0600, filepath.Dir(path): 0700, filepath.Dir(filepath.Dir(path)): 0700} {
		if info, err := os.Stat(file); err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: %v %v", file, info, err)
		}
	}
	first, _ := os.ReadFile(path)
	match := tokenValue.FindSubmatch(first)
	if match == nil || len(match[1]) != 43 || strings.Contains(stdout, string(match[1])) {
		t.Fatalf("generated token in %s", first)
	}
	expected := tokenLine.ReplaceAllLiteralString(deploy.ConfigExample, `token = "`+string(match[1])+`"`)
	expected = listenLine.ReplaceAllLiteralString(expected, `listen = "100.64.0.5:7681"`)
	if string(first) != expected {
		t.Fatalf("template changed beyond token and listen:\n%s", first)
	}
	if _, _, code := runConfig(t, "validate", "--config", path); code != 0 {
		t.Fatal("generated configuration does not validate")
	}

	_, stderr, code = runConfig(t, "init", "--config", path)
	if after, _ := os.ReadFile(path); code != 1 || !strings.Contains(stderr, "--force") || !bytes.Equal(after, first) {
		t.Fatalf("existing file: %d %q", code, stderr)
	}
	stdout, _, code = runConfig(t, "init", "--config", path, "--force")
	second, _ := os.ReadFile(path)
	backup, _ := os.ReadFile(path + ".bak")
	next := tokenValue.FindSubmatch(second)
	if code != 0 || stdout != path+"\n" || !bytes.Equal(backup, first) || next == nil || bytes.Equal(next[1], match[1]) {
		t.Fatalf("forced init %d backup=%v", code, bytes.Equal(backup, first))
	}
	if strings.Contains(string(second), "100.64.0.5") {
		t.Fatal("listen written without --listen")
	}
}
