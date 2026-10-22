package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/session"
)

func TestOmpPresetLoadsBashrcAndForwardsArguments(t *testing.T) {
	home := t.TempDir()
	bin := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(filepath.ListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("export WEBMUX_TEST_TOKEN=bashrc\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'token=%s\\n' \"$WEBMUX_TEST_TOKEN\"\nfor arg in \"$@\"; do printf 'arg=<%s>\\n' \"$arg\"; done\n"
	if err := os.WriteFile(filepath.Join(bin, "omp"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	c, err := Load(filepath.Join(home, "config.toml"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	m := session.NewManager(c.SessionOptions())
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	s, err := m.Create(home, "omp", nil, []string{"--resume", "two words"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("omp preset did not exit")
	}
	out := string(s.Buffer())
	if !strings.Contains(out, "token=bashrc") {
		t.Fatalf("~/.bashrc environment missing: %q", out)
	}
	if !strings.Contains(out, "arg=<--resume>") || !strings.Contains(out, "arg=<two words>") {
		t.Fatalf("preset arguments missing or split: %q", out)
	}
}
