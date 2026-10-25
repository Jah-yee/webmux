package assemble

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
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

func TestAgentPresetsLoadBashrcAndForwardArguments(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(filepath.ListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("export WEBMUX_TEST_TOKEN=bashrc\n"), 0600); err != nil {
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
	for _, agent := range []string{"claude", "codex", "omp"} {
		t.Run(agent, func(t *testing.T) {
			script := "#!/bin/sh\nprintf 'agent=%s token=%s\\n' \"" + agent + "\" \"$WEBMUX_TEST_TOKEN\"\nfor arg in \"$@\"; do printf 'arg=<%s>\\n' \"$arg\"; done\n"
			if err := os.WriteFile(filepath.Join(bin, agent), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			s, err := m.Create(home, agent, nil, []string{"--resume", "two words"}, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-s.Done():
			case <-time.After(5 * time.Second):
				t.Fatalf("%s preset did not exit", agent)
			}
			out := string(s.Buffer())
			if !strings.Contains(out, "agent="+agent+" token=bashrc") {
				t.Fatalf("~/.bashrc environment missing: %q", out)
			}
			if !strings.Contains(out, "arg=<--resume>") || !strings.Contains(out, "arg=<two words>") {
				t.Fatalf("preset arguments missing or split: %q", out)
			}
		})
	}
}
