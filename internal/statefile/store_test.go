package statefile

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type testDocument struct {
	V     int    `json:"v"`
	Value string `json:"value"`
}

func testStore(t *testing.T) (*Handle[testDocument], string) {
	t.Helper()
	dir := t.TempDir()
	h, _, err := Open[testDocument](dir, Settings, nil)
	if !IsKind(err, Missing) {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h, dir
}
func TestHandleLockAndClosed(t *testing.T) {
	h, dir := testStore(t)
	other, _, err := Open[testDocument](dir, Settings, nil)
	t.Cleanup(other.Close)
	if !IsKind(err, Locked) {
		t.Fatal(err)
	}
	if err := h.Commit(testDocument{1, "first"}); err != nil {
		t.Fatal(err)
	}
	h.Close()
	if err := h.Commit(testDocument{1, "second"}); !IsKind(err, Closed) {
		t.Fatal(err)
	}
	next, v, err := Open[testDocument](dir, Settings, nil)
	t.Cleanup(next.Close)
	if err != nil || v.Value != "first" {
		t.Fatalf("reopen: %+v %v", v, err)
	}
}
func TestStrictSnapshotsPreserveDocument(t *testing.T) {
	for i, data := range []string{`{"v":1,"value":"secret","value":"other"}`, `{"v":1,"Value":"secret"}`, `{"v":1,"unknown":"secret"}`, `{"v":1} {}`, `null`, "{\"v\":1,\"value\":\"\xff\"}", `{"v":2,"value":"secret"}`} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(p, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			h, _, err := Open[testDocument](dir, Settings, nil)
			t.Cleanup(h.Close)
			if err == nil {
				t.Fatal("accepted invalid document")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("leaked value")
			}
			if err := h.Commit(testDocument{1, "replacement"}); err == nil {
				t.Fatal("overwrote broken document")
			}
			got, _ := os.ReadFile(p)
			if string(got) != data {
				t.Fatal("original changed")
			}
		})
	}
}
func TestCommitPhases(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-publication", true: "after-publication"}[uncertain], func(t *testing.T) {
			h, dir := testStore(t)
			if err := h.Commit(testDocument{1, "old"}); err != nil {
				t.Fatal(err)
			}
			realPublish := h.publish
			h.publish = func(path string, data []byte, opt Options) error {
				if uncertain {
					if err := realPublish(path, data, opt); err != nil {
						return err
					}
					return &DirSyncError{Err: errors.New("injected")}
				}
				return errors.New("injected")
			}
			err := h.Commit(testDocument{1, "new"})
			var se *Error
			if !errors.As(err, &se) {
				t.Fatal(err)
			}
			want := NotCommitted
			if uncertain {
				want = Uncertain
			}
			if se.Phase != want {
				t.Fatalf("phase %s", se.Phase)
			}
			h.publish = realPublish
			if uncertain {
				if !IsKind(h.Commit(testDocument{1, "retry"}), CommitUncertain) {
					t.Fatal("uncertain handle not frozen")
				}
			} else if err := h.Commit(testDocument{1, "retry"}); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
			if uncertain && !strings.Contains(string(data), "new") {
				t.Fatal("published data lost")
			}
		})
	}
}
func TestRemoveUncertainFreezesHandle(t *testing.T) {
	h, _ := testStore(t)
	if err := h.Commit(testDocument{1, "old"}); err != nil {
		t.Fatal(err)
	}
	h.syncDir = func(string) error { return errors.New("injected") }
	if !IsKind(h.Remove(), CommitUncertain) {
		t.Fatal("missing uncertainty")
	}
	if !IsKind(h.Commit(testDocument{1, "new"}), CommitUncertain) {
		t.Fatal("write after uncertain remove")
	}
}
func TestReadSnapshotDoesNotCreateOrChmod(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if _, err := ReadSnapshot[testDocument](dir, Settings, nil); !IsKind(err, Missing) {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("created directory")
	}
	dir = t.TempDir()
	p := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(p, []byte(`{"v":1,"value":"ok"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshot[testDocument](dir, Settings, nil); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(p)
	if info.Mode().Perm() != 0644 {
		t.Fatal("read changed permissions")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("read created lock")
	}
}
func TestLegacyBlocksEvenWithNewFile(t *testing.T) {
	for _, id := range []ID{Tunnel, TailscaleOwnership} {
		dir := t.TempDir()
		d := Describe(id)
		for _, p := range []string{d.Legacy, d.Path} {
			if err := os.WriteFile(filepath.Join(dir, p), []byte(`{"v":1}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		h, _, err := Open[testDocument](dir, id, nil)
		h.Close()
		if !IsKind(err, LegacyLayout) {
			t.Fatal(err)
		}
	}
}
func TestSymlinkCannotBeReadOrReplaced(t *testing.T) {
	h, dir := testStore(t)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte(`{"v":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := h.Commit(testDocument{1, "bad"}); err == nil {
		t.Fatal("replaced link")
	}
	if _, err := ReadSnapshot[testDocument](dir, Settings, nil); err == nil {
		t.Fatal("followed link")
	}
	data, _ := os.ReadFile(target)
	if string(data) != `{"v":1}` {
		t.Fatal("changed link target")
	}
}
func TestEveryDescriptorStorageContract(t *testing.T) {
	for _, d := range Catalog() {
		t.Run(string(d.ID), func(t *testing.T) {
			dir := t.TempDir()
			h, _, err := Open[map[string]any](dir, d.ID, nil)
			t.Cleanup(h.Close)
			if !IsKind(err, Missing) {
				t.Fatal(err)
			}
			value := map[string]any{d.VersionKey: d.Version}
			if err := h.Commit(value); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{d.Path, d.LockPath()} {
				info, err := os.Stat(filepath.Join(dir, p))
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatalf("private file %s: %v %v", p, info, err)
				}
			}
			other, _, err := Open[map[string]any](dir, d.ID, nil)
			other.Close()
			if !IsKind(err, Locked) {
				t.Fatal(err)
			}
			h.Close()
			if _, err := ReadSnapshot[map[string]any](dir, d.ID, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHandleCrossProcess(t *testing.T) {
	if dir := os.Getenv("WEBMUX_STATEFILE_LOCK_TEST"); dir != "" {
		h, _, err := Open[testDocument](dir, Settings, nil)
		t.Cleanup(h.Close)
		if os.Getenv("WEBMUX_STATEFILE_EXPECT_LOCKED") == "1" {
			if !IsKind(err, Locked) {
				t.Fatalf("child acquired parent's lock: %v", err)
			}
		} else if err != nil {
			t.Fatalf("child could not acquire released lock: %v", err)
		}
		return
	}
	h, dir := testStore(t)
	if err := h.Commit(testDocument{1, "durable"}); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(locked string) {
		t.Helper()
		cmd := exec.Command(exe, "-test.run=^TestHandleCrossProcess$")
		cmd.Env = append(os.Environ(), "WEBMUX_STATEFILE_LOCK_TEST="+dir, "WEBMUX_STATEFILE_EXPECT_LOCKED="+locked)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v %s", err, out)
		}
	}
	run("1")
	h.Close()
	run("0")
}

func TestSymlinkedStateDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	h, _, err := Open[testDocument](link, Settings, nil)
	if !IsKind(err, Missing) {
		t.Fatal(err)
	}
	if err := h.Commit(testDocument{1, "kept"}); err != nil {
		t.Fatal(err)
	}
	h.Close()
	next, value, err := Open[testDocument](link, Settings, nil)
	defer next.Close()
	if err != nil || value.Value != "kept" {
		t.Fatalf("reopen: %+v %v", value, err)
	}
}
