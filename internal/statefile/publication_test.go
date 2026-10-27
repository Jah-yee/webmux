package statefile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type failingStage struct {
	*os.File
	stage string
}

var errInjectedPublication = errors.New("injected publication failure")

func (f failingStage) Chmod(mode os.FileMode) error {
	if f.stage == "chmod" {
		return errInjectedPublication
	}
	return f.File.Chmod(mode)
}
func (f failingStage) Write(data []byte) (int, error) {
	if f.stage == "write" {
		return 0, errInjectedPublication
	}
	if f.stage == "short-write" {
		return 0, nil
	}
	return f.File.Write(data)
}
func (f failingStage) Sync() error {
	if f.stage == "sync" {
		return errInjectedPublication
	}
	return f.File.Sync()
}
func (f failingStage) Close() error {
	err := f.File.Close()
	if f.stage == "close" {
		return errInjectedPublication
	}
	return err
}
func TestPublicationFailureStages(t *testing.T) {
	for _, stage := range []string{"create", "chmod", "write", "short-write", "sync", "close", "rename", "directory-sync"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "data")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			ops := publicationOps{create: func(dir, pattern string) (stagedFile, error) {
				if stage == "create" {
					return nil, errInjectedPublication
				}
				f, err := os.CreateTemp(dir, pattern)
				if err != nil {
					return nil, err
				}
				return failingStage{f, stage}, nil
			}, rename: func(a, b string) error {
				if stage == "rename" {
					return errInjectedPublication
				}
				return os.Rename(a, b)
			}, link: os.Link, remove: os.Remove, syncDir: func(dir string) error {
				if stage == "directory-sync" {
					return errInjectedPublication
				}
				return syncDir(dir)
			}}
			err := writeWithOps(path, []byte("new"), Options{TempPattern: ".test-*", RequireDirSync: true}, ops)
			if err == nil {
				t.Fatal("injected failure ignored")
			}
			var uncertain *DirSyncError
			if errors.As(err, &uncertain) != (stage == "directory-sync") {
				t.Fatalf("wrong commit phase: %v", err)
			}
			got, _ := os.ReadFile(path)
			want := "old"
			if stage == "directory-sync" {
				want = "new"
			}
			if string(got) != want {
				t.Fatalf("got %q want %q", got, want)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 1 {
				t.Fatalf("staging file leaked: %v", files)
			}
		})
	}
}
