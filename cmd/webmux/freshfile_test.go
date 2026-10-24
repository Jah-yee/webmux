package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateFreshCreatesPrivateParents(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b", "file")
	f, err := createFresh(path, ".old", 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	for file, mode := range map[string]os.FileMode{path: 0600, filepath.Join(root, "a", "b"): 0700, filepath.Join(root, "a"): 0700} {
		if info, err := os.Stat(file); err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: %v %v", file, info, err)
		}
	}
}

func TestCreateFreshKeepsExistingDirectoryMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	f, err := createFresh(filepath.Join(dir, "file"), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if info, _ := os.Stat(dir); info.Mode().Perm() != 0755 {
		t.Fatalf("parent mode changed to %v", info.Mode())
	}
}

func TestCreateFreshMovesExistingAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	for _, name := range []string{path, path + ".old"} {
		if err := os.WriteFile(name, []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := createFresh(path, ".old", os.O_APPEND)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("new")
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(path)
	aside, _ := os.ReadFile(path + ".old")
	if string(current) != "new" || string(aside) != path {
		t.Fatalf("current %q aside %q", current, aside)
	}
}

func TestCreateFreshMovesDanglingSymlinkAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.Symlink("missing-target", path); err != nil {
		t.Fatal(err)
	}
	f, err := createFresh(path, ".old", 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if target, err := os.Readlink(path + ".old"); err != nil || target != "missing-target" {
		t.Fatalf("symlink not kept aside: %q %v", target, err)
	}
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("new file %v %v", info, err)
	}
}

func TestCreateFreshWithoutSuffixRefusesExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := createFresh(path, "", 0); !errors.Is(err, os.ErrExist) {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "keep" {
		t.Fatalf("existing file changed to %q", data)
	}
}
