package launch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func open(t *testing.T, dir string) *Service {
	t.Helper()
	s := Open(dir)
	t.Cleanup(s.Close)
	return s
}

func mustCreate(t *testing.T, s *Service, in Input) Config {
	t.Helper()
	c, err := s.Create(in)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFirstOpenIsEmptyAndCreatesNoFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := open(t, dir)
	if err := s.Unavailable(); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("list %v %v", list, err)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file created on open: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("state dir mode %v %v", info, err)
	}
	info, err = os.Stat(filepath.Join(dir, lockName))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("lock mode %v %v", info, err)
	}
	if s.Path() != filepath.Join(dir, fileName) {
		t.Fatal(s.Path())
	}
}

func TestSeedFillsOnlyANewFile(t *testing.T) {
	names := func(s *Service) []string {
		t.Helper()
		list, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, c := range list {
			out = append(out, c.Name)
		}
		return out
	}
	dir := t.TempDir()
	s := Open(dir)
	if err := s.Seed(Presets("/bin/zsh")); err != nil {
		t.Fatal(err)
	}
	want := []string{"Shell", "Claude Code", "Codex CLI", "Gemini CLI", "Open Code", "omp"}
	if got := names(s); !reflect.DeepEqual(got, want) {
		t.Fatalf("seeded %v", got)
	}
	// A second Seed in the same process does not duplicate the presets.
	if err := s.Seed(Presets("")); err != nil || len(names(s)) != len(want) {
		t.Fatalf("reseeded %v %v", names(s), err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	wantIcons := []string{"terminal", "claudecode", "codex", "geminicli", "opencode", "pi"}
	for i, c := range list {
		if c.Icon != wantIcons[i] {
			t.Fatalf("preset %s icon %q", c.Name, c.Icon)
		}
	}
	for _, c := range list {
		if err := s.Delete(c.ID); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	// The user deleted every preset: an existing empty file is not reseeded.
	s = open(t, dir)
	if err := s.Seed(Presets("")); err != nil || len(names(s)) != 0 {
		t.Fatalf("seeded existing file %v %v", names(s), err)
	}
	// A user's first write wins over a later Seed.
	other := open(t, t.TempDir())
	mustCreate(t, other, Input{Name: "mine", Cmd: []string{"bash"}})
	if err := other.Seed(Presets("")); err != nil || !reflect.DeepEqual(names(other), []string{"mine"}) {
		t.Fatalf("seed after create %v %v", names(other), err)
	}
}

func TestReopenPersists(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	a := mustCreate(t, s, Input{Name: "  Claude 恢复  ", Cmd: []string{"claude", "--resume", ""}, Env: map[string]string{"B": "2", "A": "1"}})
	b := mustCreate(t, s, Input{Name: "shell", Cmd: []string{"bash"}})
	updated, err := s.Update(a.ID, Input{Name: "Claude", Cmd: []string{"claude"}, Env: map[string]string{"A": "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != a.ID || !updated.CreatedAt.Equal(a.CreatedAt) || updated.UpdatedAt.Before(a.UpdatedAt) {
		t.Fatalf("update changed identity: %+v vs %+v", updated, a)
	}
	s.Close()
	info, err := os.Stat(filepath.Join(dir, fileName))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("file mode %v %v", info, err)
	}
	s = open(t, dir)
	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Cmd, []string{"claude"}) || !reflect.DeepEqual(got.Env, map[string]string{"A": "x"}) || got.Name != "Claude" {
		t.Fatalf("reloaded %+v", got)
	}
	if !got.UpdatedAt.Equal(updated.UpdatedAt) || !got.CreatedAt.Equal(a.CreatedAt) || got.CreatedAt.Location().String() != "UTC" {
		t.Fatalf("times %+v", got)
	}
	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatalf("order %+v %v", list, err)
	}
	gotB, _ := s.Get(b.ID)
	data, _ := json.Marshal(gotB)
	if !strings.Contains(string(data), `"env":{}`) {
		t.Fatalf("empty env must serialize as {}: %s", data)
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestIconsPersistAndLegacyConfigsRemainReadable(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	a := mustCreate(t, s, Input{Name: "Claude", Icon: "claudecode", Cmd: []string{"claude"}})
	b := mustCreate(t, s, Input{Name: "Legacy", Cmd: []string{"bash"}})
	s.Close()
	s = open(t, dir)
	got, err := s.Get(a.ID)
	if err != nil || got.Icon != "claudecode" {
		t.Fatalf("reopened icon %q: %v", got.Icon, err)
	}
	legacy, err := s.Get(b.ID)
	if err != nil || legacy.Icon != "" {
		t.Fatalf("legacy icon %q: %v", legacy.Icon, err)
	}
	updated, err := s.Update(a.ID, Input{Name: a.Name, Icon: "codex", Cmd: a.Cmd})
	if err != nil || updated.Icon != "codex" {
		t.Fatalf("updated icon %q: %v", updated.Icon, err)
	}
	list, err := s.List()
	if err != nil || list[0].Icon != "codex" {
		t.Fatalf("summary icon %+v: %v", list, err)
	}
	cleared, err := s.Update(a.ID, Input{Name: a.Name, Cmd: a.Cmd})
	if err != nil || cleared.Icon != "" {
		t.Fatalf("cleared icon %q: %v", cleared.Icon, err)
	}
	for _, icon := range []string{"unknown", "https://example.com/icon.svg", "../icon", "<svg>"} {
		_, err := s.Create(Input{Name: "Invalid", Icon: icon, Cmd: []string{"bash"}})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid icon %q: %v", icon, err)
		}
	}
}

func TestEmptyArgumentKept(t *testing.T) {
	s := open(t, t.TempDir())
	c := mustCreate(t, s, Input{Name: "x", Cmd: []string{"echo", "", "b"}})
	if !reflect.DeepEqual(c.Cmd, []string{"echo", "", "b"}) {
		t.Fatal(c.Cmd)
	}
}

func TestSecondInstanceUnavailable(t *testing.T) {
	dir := t.TempDir()
	first := open(t, dir)
	mustCreate(t, first, Input{Name: "a", Cmd: []string{"a"}})
	before, err := os.ReadFile(filepath.Join(dir, fileName))
	if err != nil {
		t.Fatal(err)
	}
	second := open(t, dir)
	err = second.Unavailable()
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "另一实例") || !strings.Contains(err.Error(), filepath.Join(dir, fileName)) {
		t.Fatalf("second instance: %v", err)
	}
	if _, err := second.List(); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := second.Create(Input{Name: "b", Cmd: []string{"b"}}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, fileName))
	if string(before) != string(after) {
		t.Fatal("second instance modified the file")
	}
	if _, err := first.List(); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptFileKept(t *testing.T) {
	for name, content := range map[string]string{
		"syntax":       `{"v":1,"configs":[{"id":"abcdefgh","env":{"K":"secret-value"`,
		"version":      `{"v":2,"configs":[]}`,
		"null configs": `{"v":1}`,
		"unknown":      `{"v":1,"configs":[],"extra":true}`,
		"trailing":     `{"v":1,"configs":[]} {}`,
		"bad id":       `{"v":1,"configs":[{"id":"BAD","name":"a","cmd":["a"],"env":{},"createdAt":"2026-10-23T12:00:00Z","updatedAt":"2026-10-23T12:00:00Z"}]}`,
		"reserved env": `{"v":1,"configs":[{"id":"abcdefgh","name":"a","cmd":["a"],"env":{"WEBMUX_SESSION_ID":"secret-value"},"createdAt":"2026-10-23T12:00:00Z","updatedAt":"2026-10-23T12:00:00Z"}]}`,
		"type":         `{"v":1,"configs":[{"id":"abcdefgh","name":"a","cmd":["a"],"env":{"K":12345},"createdAt":"2026-10-23T12:00:00Z","updatedAt":"2026-10-23T12:00:00Z"}]}`,
		"dup name":     `{"v":1,"configs":[{"id":"abcdefgh","name":"a","cmd":["a"],"env":{},"createdAt":"2026-10-23T12:00:00Z","updatedAt":"2026-10-23T12:00:00Z"},{"id":"abcdefgi","name":"a","cmd":["a"],"env":{},"createdAt":"2026-10-23T12:00:00Z","updatedAt":"2026-10-23T12:00:00Z"}]}`,
		"no times":     `{"v":1,"configs":[{"id":"abcdefgh","name":"a","cmd":["a"],"env":{}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, fileName)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			s := open(t, dir)
			err := s.Unavailable()
			if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), path) {
				t.Fatalf("corrupt file: %v", err)
			}
			if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "12345") {
				t.Fatalf("error leaks a value: %v", err)
			}
			if _, err := s.Create(Input{Name: "b", Cmd: []string{"b"}}); !errors.Is(err, ErrUnavailable) {
				t.Fatal(err)
			}
			if err := s.Delete("abcdefgh"); !errors.Is(err, ErrUnavailable) {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != content {
				t.Fatalf("file changed: %q %v", got, err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 2 {
				t.Fatalf("unexpected files: %v", entries)
			}
		})
	}
}

func TestUnreadableFileUnavailable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, fileName), 0700); err != nil {
		t.Fatal(err)
	}
	if err := open(t, dir).Unavailable(); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestNilAndClosedUnavailable(t *testing.T) {
	var s *Service
	if _, err := s.List(); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := s.Get("abcdefgh"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	s.Close()
	closed := Open(t.TempDir())
	closed.Close()
	if _, err := closed.List(); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestDuplicateName(t *testing.T) {
	s := open(t, t.TempDir())
	a := mustCreate(t, s, Input{Name: "Same", Cmd: []string{"a"}})
	b := mustCreate(t, s, Input{Name: "same", Cmd: []string{"b"}})
	if _, err := s.Create(Input{Name: " Same ", Cmd: []string{"c"}}); !errors.Is(err, ErrDuplicate) {
		t.Fatal(err)
	}
	if _, err := s.Update(b.ID, Input{Name: "Same", Cmd: []string{"b"}}); !errors.Is(err, ErrDuplicate) {
		t.Fatal(err)
	}
	if _, err := s.Update(a.ID, Input{Name: "Same", Cmd: []string{"a2"}}); err != nil {
		t.Fatalf("renaming to own name: %v", err)
	}
	if _, err := s.Update("zzzzzzzz", Input{Name: "x", Cmd: []string{"x"}}); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Delete("zzzzzzzz"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestLimits(t *testing.T) {
	long := strings.Repeat("x", MaxItemBytes+1)
	many := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "a"
		}
		return out
	}
	env := func(n int) map[string]string {
		out := map[string]string{}
		for i := range n {
			out[fmt.Sprintf("V%d", i)] = "v"
		}
		return out
	}
	for name, row := range map[string]struct {
		in Input
		ok bool
	}{
		"blank name":       {Input{Name: "   ", Cmd: []string{"a"}}, false},
		"64 chars":         {Input{Name: strings.Repeat("名", 64), Cmd: []string{"a"}}, true},
		"65 chars":         {Input{Name: strings.Repeat("名", 65), Cmd: []string{"a"}}, false},
		"no cmd":           {Input{Name: "a"}, false},
		"empty first":      {Input{Name: "a", Cmd: []string{"", "b"}}, false},
		"128 items":        {Input{Name: "a", Cmd: many(128)}, true},
		"129 items":        {Input{Name: "a", Cmd: many(129)}, false},
		"long item":        {Input{Name: "a", Cmd: []string{"a", long}}, false},
		"max item":         {Input{Name: "a", Cmd: []string{"a", long[1:]}}, true},
		"64 env":           {Input{Name: "a", Cmd: []string{"a"}, Env: env(64)}, true},
		"65 env":           {Input{Name: "a", Cmd: []string{"a"}, Env: env(65)}, false},
		"long env value":   {Input{Name: "a", Cmd: []string{"a"}, Env: map[string]string{"K": long}}, false},
		"bad env name":     {Input{Name: "a", Cmd: []string{"a"}, Env: map[string]string{"1K": "v"}}, false},
		"reserved env":     {Input{Name: "a", Cmd: []string{"a"}, Env: map[string]string{"WEBMUX_SESSION_ID": "v"}}, false},
		"nul in env value": {Input{Name: "a", Cmd: []string{"a"}, Env: map[string]string{"K": "a\x00b"}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			s := open(t, t.TempDir())
			_, err := s.Create(row.in)
			if row.ok != (err == nil) || (err != nil && !errors.Is(err, ErrInvalid)) {
				t.Fatalf("err %v, want ok=%v", err, row.ok)
			}
			if err != nil && strings.Contains(err.Error(), "a\x00b") {
				t.Fatal("error leaks a value")
			}
		})
	}
	s := open(t, t.TempDir())
	for i := range MaxConfigs {
		mustCreate(t, s, Input{Name: fmt.Sprint(i), Cmd: []string{"a"}})
	}
	if _, err := s.Create(Input{Name: "one more", Cmd: []string{"a"}}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if list, _ := s.List(); len(list) != MaxConfigs {
		t.Fatal(len(list))
	}
}

func TestSaveFailureRollsBack(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	s := open(t, dir)
	a := mustCreate(t, s, Input{Name: "a", Cmd: []string{"a"}, Env: map[string]string{"K": "v"}})
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	if _, err := s.Create(Input{Name: "b", Cmd: []string{"b"}}); err == nil || errors.Is(err, ErrInvalid) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("create on read-only dir: %v", err)
	}
	if _, err := s.Update(a.ID, Input{Name: "renamed", Cmd: []string{"z"}}); err == nil {
		t.Fatal("update succeeded on read-only dir")
	}
	if err := s.Delete(a.ID); err == nil {
		t.Fatal("delete succeeded on read-only dir")
	}
	list, err := s.List()
	if err != nil || len(list) != 1 || list[0].Name != "a" || !reflect.DeepEqual(list[0].Cmd, []string{"a"}) {
		t.Fatalf("memory not rolled back: %+v %v", list, err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, Input{Name: "b", Cmd: []string{"b"}})
}

func TestIDsUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id, err := newID(func(id string) bool { return seen[id] })
		if err != nil {
			t.Fatal(err)
		}
		if !idPattern.MatchString(id) || seen[id] {
			t.Fatalf("id %q", id)
		}
		seen[id] = true
	}
	calls := 0
	if _, err := newID(func(string) bool { calls++; return true }); err == nil || calls == 0 {
		t.Fatal("exhausted retries must fail")
	}
}

func TestSummariesCarrySortedNamesOnly(t *testing.T) {
	s := open(t, t.TempDir())
	mustCreate(t, s, Input{Name: "a", Cmd: []string{"a"}, Env: map[string]string{"ZED": "secret-z", "API_KEY": "secret-value"}})
	mustCreate(t, s, Input{Name: "b", Cmd: []string{"b"}})
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(list[0].EnvNames, []string{"API_KEY", "ZED"}) || list[1].EnvNames == nil || len(list[1].EnvNames) != 0 {
		t.Fatalf("env names %+v", list)
	}
	data, _ := json.Marshal(list)
	if strings.Contains(string(data), "secret") || !strings.Contains(string(data), `"envNames":[]`) {
		t.Fatalf("summary json %s", data)
	}
}

func TestDefensiveCopies(t *testing.T) {
	s := open(t, t.TempDir())
	cmd, env := []string{"a", "b"}, map[string]string{"K": "v"}
	c := mustCreate(t, s, Input{Name: "a", Cmd: cmd, Env: env})
	cmd[0], env["K"] = "mutated", "mutated"
	c.Cmd[1], c.Env["K"] = "mutated", "mutated"
	got, _ := s.Get(c.ID)
	got.Cmd[0] = "mutated"
	again, _ := s.Get(c.ID)
	if !reflect.DeepEqual(again.Cmd, []string{"a", "b"}) || again.Env["K"] != "v" {
		t.Fatalf("shared state: %+v", again)
	}
}
