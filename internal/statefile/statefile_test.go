package statefile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

var opts = Options{TempPattern: ".state-*"}

// entries lists dir so tests can assert that no staging file is left behind.
func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(list))
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestWriteReplacesWithPrivateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old content that is longer"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new"), opts); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("content %q %v", data, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != Perm {
		t.Fatalf("mode %v %v", info.Mode(), err)
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"state.json"}) {
		t.Fatalf("leftover files %v", got)
	}
}

func TestWriteJSONIsIndentedWithoutNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := WriteJSON(path, map[string]any{"v": 1, "list": []int{}}, opts); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if want := "{\n  \"list\": [],\n  \"v\": 1\n}"; string(data) != want {
		t.Fatalf("got %q", data)
	}
	if err := WriteJSON(path, func() {}, opts); err == nil {
		t.Fatal("unencodable value written")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, data) {
		t.Fatal("failed encoding changed the file")
	}
}

func TestFailedRenameKeepsTargetAndRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "child"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("data"), opts); err == nil {
		t.Fatal("replaced a directory")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatalf("target changed: %v %v", info, err)
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"state"}) {
		t.Fatalf("leftover files %v", got)
	}
}

func TestReadOnlyDirectoryFailsWithoutLeftovers(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if err := Write(path, []byte("new"), opts); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("got %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		t.Fatalf("target changed to %q", data)
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"state"}) {
		t.Fatalf("leftover files %v", got)
	}
}

func TestMissingDirectoryFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "state")
	if err := Write(path, []byte("x"), opts); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("got %v", err)
	}
}

func TestExclusiveNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unit")
	exclusive := Options{TempPattern: ".state-*", Exclusive: true}
	if err := Write(path, []byte("first"), exclusive); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("second"), exclusive); !errors.Is(err, os.ErrExist) {
		t.Fatalf("got %v", err)
	}
	data, _ := os.ReadFile(path)
	info, err := os.Stat(path)
	if err != nil || string(data) != "first" || info.Mode().Perm() != Perm {
		t.Fatalf("published %q %v %v", data, info, err)
	}
	if got := entries(t, dir); !slices.Equal(got, []string{"unit"}) {
		t.Fatalf("leftover files %v", got)
	}
}

func TestReadersNeverSeePartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	a := []byte(strings.Repeat("a", 256<<10))
	b := []byte(strings.Repeat("b", 128<<10))
	if err := Write(path, a, opts); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, a) && !bytes.Equal(data, b) {
				t.Errorf("read %d bytes, err %v", len(data), err)
				return
			}
		}
	})
	for i := range 50 {
		next := a
		if i%2 == 0 {
			next = b
		}
		if err := Write(path, next, opts); err != nil {
			t.Error(err)
			break
		}
	}
	close(done)
	wg.Wait()
}
