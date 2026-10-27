package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestCreateRejectsInvalidInput(t *testing.T) {
	manager := NewManager(Options{DefaultCwd: t.TempDir()})
	t.Cleanup(func() { manager.Shutdown(context.Background()) })
	for _, input := range []CreateInput{
		{Cwd: filepath.Join(t.TempDir(), "missing"), Cmd: []string{"/bin/sh"}},
		{Cmd: nil},
		{Cmd: []string{"/bin/sh"}, Env: map[string]string{"INVALID-NAME": "x"}},
	} {
		if _, err := manager.Create(input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("input %+v: err = %v", input, err)
		}
	}
}

func TestCreateAfterShutdown(t *testing.T) {
	manager := NewManager(Options{DefaultCwd: t.TempDir()})
	manager.Shutdown(context.Background())
	if _, err := manager.Create(CreateInput{Cmd: []string{"/bin/sh"}}); !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("err = %v", err)
	}
}
