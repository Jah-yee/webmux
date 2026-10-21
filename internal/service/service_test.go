package service

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/config"
)

type exitStatus int

func (s exitStatus) Error() string { return "fake command failure" }
func (s exitStatus) ExitCode() int { return int(s) }

type fixture struct {
	m       *Manager
	output  bytes.Buffer
	calls   [][]string
	loaded  bool
	failure string
}

func newFixture(t *testing.T, platform string) *fixture {
	t.Helper()
	home := t.TempDir()
	f := &fixture{}
	f.m = &Manager{
		platform: platform, home: home, configHome: filepath.Join(home, ".config"),
		executable: filepath.Join(home, "space & 中文 % $", "webmux"), domain: "gui/501",
		environment: map[string]string{"PATH": "/bin:/usr/bin", "SHELL": "/bin/sh", "LANG": "en_US.UTF-8"},
		out:         &f.output, stderr: &f.output,
		lookPath: func(name string) (string, error) { return name, nil },
		initConfig: func(path string) error {
			return os.WriteFile(path, []byte("token = \""+strings.Repeat("a", 48)+"\"\n"), 0600)
		},
	}
	f.m.run = func(ctx context.Context, name string, args []string, out, stderr io.Writer) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		f.calls = append(f.calls, append([]string{name}, args...))
		if f.failure != "" && strings.Contains(strings.Join(args, " "), f.failure) {
			return errors.New("fake failure")
		}
		if name == "launchctl" {
			switch args[0] {
			case "print":
				if !f.loaded {
					return exitStatus(113)
				}
			case "bootstrap":
				f.loaded = true
			case "bootout":
				f.loaded = false
			}
		}
		return nil
	}
	return f
}

func TestInstallAndLifecycle(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		t.Run(platform, func(t *testing.T) {
			f := newFixture(t, platform)
			path := filepath.Join(f.m.home, "config & 中文.toml")
			ctx := context.Background()
			if err := f.m.Install(ctx, path, true, false, false); err != nil {
				t.Fatal(err)
			}
			c, err := config.Load(path, true)
			if err != nil || len(c.Token) != 48 {
				t.Fatal("default configuration not initialized")
			}
			if strings.Contains(f.output.String(), c.Token) {
				t.Fatal("token exposed in service output")
			}
			body, err := os.ReadFile(f.m.definitionPath())
			if err != nil || bytes.Contains(body, []byte(c.Token)) {
				t.Fatalf("service definition missing or exposes token: %v", err)
			}
			if platform == "linux" {
				want := [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "enable", "--now", "webmux.service"}}
				if !reflect.DeepEqual(f.calls, want) {
					t.Fatalf("install calls: %q", f.calls)
				}
			} else if !f.loaded {
				t.Fatal("LaunchAgent was not bootstrapped")
			}
			f.calls = nil
			if err := f.m.Install(ctx, path, true, false, false); err == nil || len(f.calls) != 0 {
				t.Fatal("existing installation overwritten without --force")
			}
			if err := f.m.Install(ctx, path, true, true, false); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"start", "status", "restart", "stop", "stop", "start", "uninstall"} {
				if err := f.m.Control(ctx, action, false); err != nil {
					t.Fatalf("%s: %v", action, err)
				}
			}
			if _, err := os.Stat(f.m.definitionPath()); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("service definition not removed")
			}
			kept, err := config.Load(path, true)
			if err != nil || kept.Token != c.Token {
				t.Fatal("configuration modified during service management")
			}
			if platform == "darwin" && f.loaded {
				t.Fatal("LaunchAgent still loaded after uninstall")
			}
			for _, call := range f.calls {
				if strings.Contains(strings.Join(call, " "), "-k") {
					t.Fatal("restart used an immediate kill instead of graceful stop")
				}
			}
		})
	}
}

func TestDryRunAndInvalidConfigHaveNoSideEffects(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		f := newFixture(t, platform)
		path := filepath.Join(f.m.home, "config.toml")
		if err := f.m.Install(context.Background(), path, true, false, true); err != nil {
			t.Fatal(err)
		}
		if len(f.calls) != 0 {
			t.Fatal("dry-run called the service manager")
		}
		entries, err := os.ReadDir(f.m.home)
		if err != nil || len(entries) != 0 {
			t.Fatal("dry-run wrote files")
		}
		if err := f.m.Install(context.Background(), path, false, false, false); err == nil {
			t.Fatal("explicit missing config accepted")
		}
		if err := os.WriteFile(path, []byte("listen = 'invalid'\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := f.m.Install(context.Background(), path, true, false, false); err == nil || len(f.calls) != 0 {
			t.Fatal("invalid configuration installed")
		}
	}
}

func TestFailureDoesNotRemoveInstallation(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		f := newFixture(t, platform)
		path := filepath.Join(f.m.home, "config.toml")
		if err := f.m.Install(context.Background(), path, true, false, false); err != nil {
			t.Fatal(err)
		}
		f.failure = "--now"
		if platform == "darwin" {
			f.failure = "bootout"
		}
		if err := f.m.Control(context.Background(), "uninstall", false); err == nil {
			t.Fatal("manager failure ignored")
		}
		if _, err := os.Stat(f.m.definitionPath()); err != nil {
			t.Fatal("definition removed despite stop failure")
		}
	}
	f := newFixture(t, "darwin")
	f.failure = "print"
	if err := f.m.stopLaunchd(context.Background()); err == nil {
		t.Fatal("query failure mistaken for missing service")
	}
}

func TestLogsAndCancellation(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		f := newFixture(t, platform)
		for _, follow := range []bool{false, true} {
			if err := f.m.Control(context.Background(), "logs", follow); err != nil {
				t.Fatal(err)
			}
			last := f.calls[len(f.calls)-1]
			flag := "--follow"
			if platform == "darwin" {
				flag = "-F"
			}
			if strings.Contains(strings.Join(last, " "), flag) != follow {
				t.Fatalf("incorrect log follow arguments: %q", last)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := f.m.Control(ctx, "logs", true); err != nil {
			t.Fatalf("interrupting logs failed: %v", err)
		}
		if err := f.m.Install(ctx, filepath.Join(f.m.home, "missing.toml"), true, false, false); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled installation was not stopped: %v", err)
		}
	}
}

func TestSystemdWorkingDirectory(t *testing.T) {
	base := t.TempDir()
	for _, row := range []struct{ home, escaped string }{
		{"alice", "alice"},
		{"alice smith", "alice smith"},
		{"alice % cache", "alice %% cache"},
	} {
		t.Run(row.home, func(t *testing.T) {
			f := newFixture(t, "linux")
			f.m.home = filepath.Join(base, row.home)
			f.m.executable = "/bin/true"
			if err := os.MkdirAll(f.m.home, 0700); err != nil {
				t.Fatal(err)
			}
			body, err := f.m.render(filepath.Join(f.m.home, "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			// WorkingDirectory consumes a raw path, unlike ExecStart and
			// Environment. Outer quotes become part of the path and invalidate it.
			want := "\nWorkingDirectory=" + filepath.Join(base, row.escaped) + "\n"
			if !bytes.Contains(body, []byte(want)) {
				t.Fatalf("working directory must be unquoted with literal spaces and escaped %%: want %q\n%s", want, body)
			}
			t.Run("systemd-verify", func(t *testing.T) {
				tool, err := exec.LookPath("systemd-analyze")
				if err != nil {
					t.Skip("systemd-analyze is not installed")
				}
				file := filepath.Join(base, "webmux.service")
				if err := os.WriteFile(file, body, 0600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if output, err := exec.CommandContext(ctx, tool, "verify", "--man=no", file).CombinedOutput(); err != nil {
					t.Fatalf("systemd rejected generated unit: %s\n%v", output, err)
				}
			})
		})
	}
}

func TestRenderedDefinitionsEscapePathsAndKeepCleanup(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		f := newFixture(t, platform)
		path := filepath.Join(f.m.home, "a & b % $ ' \" 中文.toml")
		body, err := f.m.render(path)
		if err != nil {
			t.Fatal(err)
		}
		if platform == "linux" {
			if !bytes.Contains(body, []byte("ExecStart=:"+unitQuote(f.m.executable)+" serve --config "+unitQuote(path))) || !bytes.Contains(body, []byte("KillMode=mixed\nTimeoutStopSec=15")) {
				t.Fatalf("unsafe unit definition: %s", body)
			}
			continue
		}
		decoder := xml.NewDecoder(bytes.NewReader(body))
		var values []string
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if start, ok := token.(xml.StartElement); ok && start.Name.Local == "string" {
				var value string
				if err := decoder.DecodeElement(&value, &start); err != nil {
					t.Fatal(err)
				}
				values = append(values, value)
			}
		}
		if len(values) < 5 || !reflect.DeepEqual(values[:5], []string{"com.webmux", f.m.executable, "serve", "--config", path}) {
			t.Fatalf("plist argv changed after decoding: %q", values)
		}
		if runtime.GOOS == "darwin" {
			file := filepath.Join(t.TempDir(), "com.webmux.plist")
			if err := os.WriteFile(file, body, 0600); err != nil {
				t.Fatal(err)
			}
			if output, err := exec.Command("/usr/bin/plutil", "-lint", file).CombinedOutput(); err != nil {
				t.Fatalf("invalid launchd plist: %s %v", output, err)
			}
		}
	}
}

func TestEnvironmentCapturesOnlySupportedVariables(t *testing.T) {
	t.Setenv("PATH", ".:relative:/custom/bin:/custom/bin")
	t.Setenv("WEBMUX_TOKEN", "private-test-secret")
	t.Setenv("XDG_STATE_HOME", "/custom/state")
	env := serviceEnvironment("/home/test")
	if !strings.HasPrefix(env["PATH"], "/custom/bin:") || strings.Contains(env["PATH"], "relative") || strings.Contains(env["PATH"], "/custom/bin:/custom/bin") {
		t.Fatalf("unstable PATH: %s", env["PATH"])
	}
	if _, ok := env["WEBMUX_TOKEN"]; ok || env["XDG_STATE_HOME"] != "/custom/state" {
		t.Fatal("service environment included secrets or lost state location")
	}
}

func TestDefinitionPublicationIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "webmux.service")
	if err := writeDefinition(path, []byte("original"), false); err != nil {
		t.Fatal(err)
	}
	if err := writeDefinition(path, []byte("replace"), false); !errors.Is(err, os.ErrExist) {
		t.Fatal("non-force publication overwrote existing definition")
	}
	if err := writeDefinition(path, []byte("replace"), true); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	info, statErr := os.Stat(path)
	if err != nil || statErr != nil || string(body) != "replace" || info.Mode().Perm() != 0600 {
		t.Fatal("forced publication failed or permissions changed")
	}
}
