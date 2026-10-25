package assemble

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

func TestAttentionSignalOptions(t *testing.T) {
	dir := t.TempDir()
	for _, row := range []struct {
		name, body                  string
		disableProtocol, disableBel bool
	}{
		{"defaults", "", false, false},
		{"legacy file", "idle_after_seconds = 30\n", false, false},
		{"disabled", "agent_protocol = false\nbell_attention = false\n", true, true},
		{"protocol only off", "agent_protocol = false\n", true, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			path := filepath.Join(dir, row.name+".toml")
			if err := os.WriteFile(path, []byte(row.body), 0600); err != nil {
				t.Fatal(err)
			}
			c, err := config.Load(path, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			o := SessionOptions(c)
			if o.DisableAgentProtocol != row.disableProtocol || o.DisableBellAttention != row.disableBel {
				t.Fatalf("options = %+v", o)
			}
		})
	}
}

func TestSavedLaunchConfigLoadsBashrcAndForwardsArguments(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WEBMUX_TEST_TOKEN", "")
	bashrc := "export WEBMUX_TEST_TOKEN=bashrc\nexport PATH='" + strings.ReplaceAll(bin, "'", "'\\''") + "':\"$PATH\"\n"
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte(bashrc), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(filepath.Join(home, "config.toml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	m := session.NewManager(SessionOptions(c))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	launches := launch.Open(t.TempDir())
	defer launches.Close()
	service := workspace.New(t.TempDir(), m, launches, home)
	defer service.Close()
	agent := "opencode"
	script := "#!/bin/sh\nprintf 'agent=%s token=%s\\n' \"" + agent + "\" \"$WEBMUX_TEST_TOKEN\"\nfor arg in \"$@\"; do printf 'arg=<%s>\\n' \"$arg\"; done\n"
	if err := os.WriteFile(filepath.Join(bin, agent), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	saved, err := launches.Create(launch.Input{Name: "Open Code", Cmd: []string{"bash", "-ic", `exec opencode "$@"`, "opencode", "--resume", "two words", "", "$HOME"}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := service.CreateSession(context.Background(), workspace.SessionInput{Cwd: home, ConfigID: &saved.ID})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("%s saved command did not exit", agent)
	}
	out := string(s.Buffer())
	if !strings.Contains(out, "agent="+agent+" token=bashrc") {
		t.Fatalf("~/.bashrc environment missing: %q", out)
	}
	if !strings.Contains(out, "arg=<--resume>") || !strings.Contains(out, "arg=<two words>") || !strings.Contains(out, "arg=<>") || !strings.Contains(out, "arg=<$HOME>") {
		t.Fatalf("saved command arguments missing or split: %q", out)
	}
}
