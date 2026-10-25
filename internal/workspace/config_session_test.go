package workspace

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/session"
)

func configService(t *testing.T, launches *launch.Service) (*Service, string) {
	t.Helper()
	cwd := t.TempDir()
	m := session.NewManager(session.Options{DefaultCwd: cwd})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	s := New(t.TempDir(), m, launches, cwd)
	t.Cleanup(s.Close)
	return s, cwd
}

func TestConfigIDRulesRunBeforeDirectoryChecks(t *testing.T) {
	launches := launch.Open(t.TempDir())
	t.Cleanup(launches.Close)
	s, cwd := configService(t, launches)
	saved, err := launches.Create(launch.Input{Name: "saved", Cmd: []string{"/bin/sh", "-c", "printf SAVED"}, Env: map[string]string{"SAVED": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	empty, unknown, id := "", "zzzzzzzz", saved.ID
	// A missing cwd proves that each rejection happens before any
	// filesystem or Git work.
	missing := filepath.Join(cwd, "missing")
	for _, row := range []struct {
		name string
		in   SessionInput
		kind error
		text string
	}{
		{"empty", SessionInput{Cwd: missing, ConfigID: &empty}, ErrInvalid, "configId must not be empty"},
		{"with preset", SessionInput{Cwd: missing, ConfigID: &id, Preset: "custom"}, ErrInvalid, "configId cannot be combined with preset, cmd, args or env"},
		{"with cmd", SessionInput{Cwd: missing, ConfigID: &id, Cmd: []string{"x"}}, ErrInvalid, "configId cannot be combined with preset, cmd, args or env"},
		{"with args", SessionInput{Cwd: missing, ConfigID: &id, Args: []string{"x"}}, ErrInvalid, "configId cannot be combined with preset, cmd, args or env"},
		{"with env", SessionInput{Cwd: missing, ConfigID: &id, Env: map[string]string{"A": "1"}}, ErrInvalid, "configId cannot be combined with preset, cmd, args or env"},
		{"unknown", SessionInput{Cwd: missing, ConfigID: &unknown}, launch.ErrNotFound, launch.ErrNotFound.Error()},
		{"forged env", SessionInput{Cwd: missing, Preset: "custom", Cmd: []string{"/bin/sh"}, Env: map[string]string{"WEBMUX_SESSION_ID": "x"}}, ErrInvalid, ""},
	} {
		_, err := s.CreateSession(context.Background(), row.in)
		if !errors.Is(err, row.kind) || (row.text != "" && err.Error() != row.text) {
			t.Errorf("%s: %v, want %v %q", row.name, err, row.kind, row.text)
		}
	}
	item, err := s.CreateSession(context.Background(), SessionInput{ConfigID: &id})
	if err != nil {
		t.Fatal(err)
	}
	info := item.Info()
	if info.Preset != "custom" || !reflect.DeepEqual(info.Cmd, saved.Cmd) || info.Cwd != cwd {
		t.Fatalf("session from config: %+v", info)
	}
}

func TestConfigIDWithoutLaunchStoreIsUnavailable(t *testing.T) {
	s, _ := configService(t, nil)
	id := "abcdefgh"
	if _, err := s.CreateSession(context.Background(), SessionInput{ConfigID: &id}); !errors.Is(err, launch.ErrUnavailable) {
		t.Fatalf("nil store: %v", err)
	}
}
