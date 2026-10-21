package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
)

func TestServiceCLIHelpAndUsage(t *testing.T) {
	for _, args := range [][]string{
		{"help"}, {"--help"}, {"-h"}, {"--version"},
		{"service", "--help"}, {"service", "install", "--help"},
		{"service", "logs", "--help"}, {"config", "--help"},
	} {
		var out, stderr bytes.Buffer
		if code := dispatch(args, &out, &stderr); code != contract.ExitOK {
			t.Fatalf("%q: exit %d, %s", args, code, &stderr)
		}
		if out.Len()+stderr.Len() == 0 {
			t.Fatalf("%q: no help output", args)
		}
	}
	for _, args := range [][]string{
		{"service", "unknown"}, {"service", "start", "extra"},
		{"service", "stop", "--force"}, {"service", "install", "--token", "secret"},
	} {
		var out, stderr bytes.Buffer
		if code := dispatch(args, &out, &stderr); code != contract.ExitUsage {
			t.Fatalf("%q: exit %d, %s", args, code, &stderr)
		}
	}
	if _, err := loadConfig([]string{"--listen", "127.0.0.1:0", "typo"}); err == nil {
		t.Fatal("unexpected server arguments silently ignored")
	}
}

func TestServiceConfigUsesExistingInitializer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.toml")
	if err := initializeServiceConfig(path); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(c.Token) < minTunnelTokenLength {
		t.Fatal("service configuration lacks a strong token")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("service configuration is not private")
	}
	if err := initializeServiceConfig(path); err == nil {
		t.Fatal("service installation overwrote an existing configuration")
	}
	kept, err := config.Load(path, true)
	if err != nil || kept.Token != c.Token {
		t.Fatal("service installation changed the existing token")
	}
	var out, stderr bytes.Buffer
	if code := dispatch([]string{"config", "check", "--config", path}, &out, &stderr); code != contract.ExitOK || !strings.Contains(out.String(), "OK") {
		t.Fatalf("config check alias failed: %d %s %s", code, &out, &stderr)
	}
	if strings.Contains(out.String()+stderr.String(), c.Token) {
		t.Fatal("config check exposed the token")
	}
}

func TestServiceInstallPreviewHasNoSideEffects(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("user services require a regular user")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	var out, stderr bytes.Buffer
	if code := dispatch([]string{"service", "install", "--dry-run"}, &out, &stderr); code != contract.ExitOK {
		t.Fatalf("dry-run: %d %s", code, &stderr)
	}
	if !strings.Contains(out.String(), "Service definition:") {
		t.Fatal("dry-run omitted the definition")
	}
	entries, err := os.ReadDir(home)
	if err != nil || len(entries) != 0 {
		t.Fatal("dry-run wrote configuration or service files")
	}
}
