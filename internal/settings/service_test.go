package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func themeChange(value string) Changes {
	return Changes{Fields: map[string]bool{"theme": true}, Values: Values{Theme: &value}}
}
func TestDurableRevisionsAndSubscriptions(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	initial, err := s.Snapshot()
	if err != nil || initial.Revision != 0 || initial.Values.Theme != nil {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("read created data: %v", err)
	}
	second := Open(dir)
	defer second.Close()
	if _, err := second.Snapshot(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("second writer: %v", err)
	}
	sub, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if first := <-sub.Events; first.Snapshot == nil || first.Snapshot.Revision != 0 {
		t.Fatalf("first: %+v", first)
	}
	next, err := s.Patch(0, themeChange("dark"))
	if err != nil || next.Revision != 1 {
		t.Fatalf("patch: %+v %v", next, err)
	}
	if e := <-sub.Events; e.Revision != 1 || !e.Changes.Fields["theme"] {
		t.Fatalf("event: %+v", e)
	}
	*next.Values.Theme = "light"
	if _, err := s.Patch(0, themeChange("light")); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	s.Close()
	reopened := Open(dir)
	defer reopened.Close()
	got, err := reopened.Snapshot()
	if err != nil || got.Revision != 1 || *got.Values.Theme != "dark" {
		t.Fatalf("reopen: %+v %v", got, err)
	}
}
func TestAtomicConcurrentPatch(t *testing.T) {
	s := Open(t.TempDir())
	defer s.Close()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, theme := range []string{"dark", "light"} {
		wg.Go(func() { _, err := s.Patch(0, themeChange(theme)); results <- err })
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
}
func TestCorruptionPreserved(t *testing.T) {
	for _, data := range []string{`{"v":1,"revision":0,"values":{},"values":{}}`, `{"v":999}`, `{"v":1,"secret":"do-not-leak"}`, `null`} {
		t.Run(data, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			s := Open(dir)
			defer s.Close()
			if _, err := s.Patch(0, themeChange("dark")); !errors.Is(err, ErrUnavailable) {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != data {
				t.Fatalf("changed: %q %v", got, err)
			}
		})
	}
}
func TestFailedCommitDoesNotPublish(t *testing.T) {
	s := Open(t.TempDir())
	defer s.Close()
	sub, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	<-sub.Events
	s.commit = func(storedFile) error { return errors.New("injected disk failure") }
	if _, err := s.Patch(0, themeChange("dark")); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if s.current.Revision != 0 {
		t.Fatal("uncommitted revision adopted")
	}
	if _, ok := <-sub.Events; ok {
		t.Fatal("uncommitted event")
	}
	if _, err := s.Patch(0, themeChange("light")); !errors.Is(err, ErrUnavailable) {
		t.Fatal("writes not frozen")
	}
}
func TestNullAndEmptyDistinct(t *testing.T) {
	s := Open(t.TempDir())
	defer s.Close()
	empty := []Command{}
	v, err := s.Patch(0, Changes{Fields: map[string]bool{"quickCommands": true}, Values: Values{QuickCommands: &empty}})
	if err != nil || v.Values.QuickCommands == nil {
		t.Fatalf("empty: %+v %v", v, err)
	}
	v, err = s.Patch(1, Changes{Fields: map[string]bool{"quickCommands": true}})
	if err != nil || v.Values.QuickCommands != nil {
		t.Fatalf("null: %+v %v", v, err)
	}
}
func TestSlowSubscriberDoesNotBlock(t *testing.T) {
	s := Open(t.TempDir())
	defer s.Close()
	sub, err := s.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	for i := range int64(40) {
		if _, err := s.Patch(i, themeChange("dark")); err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for range sub.Events {
		count++
	}
	if count != 16 {
		t.Fatalf("queue: %d", count)
	}
}

func TestSubscribeConcurrentCommitHasNoGap(t *testing.T) {
	for range 20 {
		s := Open(t.TempDir())
		start := make(chan struct{})
		subscribed := make(chan *Subscription, 1)
		committed := make(chan error, 1)
		go func() {
			<-start
			sub, err := s.Subscribe()
			if err != nil {
				subscribed <- nil
				return
			}
			subscribed <- sub
		}()
		go func() { <-start; _, err := s.Patch(0, themeChange("dark")); committed <- err }()
		close(start)
		sub := <-subscribed
		if sub == nil {
			t.Fatal("subscribe failed")
		}
		if err := <-committed; err != nil {
			t.Fatal(err)
		}
		first := <-sub.Events
		if first.Snapshot == nil {
			t.Fatal("first event was not snapshot")
		}
		if first.Snapshot.Revision == 0 {
			select {
			case next := <-sub.Events:
				if next.Revision != 1 {
					t.Fatal("revision skipped")
				}
			default:
				t.Fatal("commit fell in subscription gap")
			}
		} else if first.Snapshot.Revision != 1 {
			t.Fatal("invalid snapshot revision")
		}
		sub.Close()
		s.Close()
	}
}
func TestValidationBoundaries(t *testing.T) {
	for _, size := range []int{7, 21} {
		if validateValues(Values{FontSize: &size}) == nil {
			t.Fatal("font range accepted")
		}
	}
	for _, p := range []Preview{{0, 0}, {51, 0}, {1, -1}, {1, 51}} {
		if validateValues(Values{Preview: &p}) == nil {
			t.Fatal("preview range accepted")
		}
	}
	for _, p := range []Preview{{1, 0}, {50, 50}} {
		if err := validateValues(Values{Preview: &p}); err != nil {
			t.Fatal(err)
		}
	}
	duplicate := []Command{{ID: "a", Command: "echo a"}, {ID: "a", Command: "echo b"}}
	if validateValues(Values{QuickCommands: &duplicate}) == nil {
		t.Fatal("duplicate command ID")
	}
	if validateValues(Values{HomeWorktrees: map[string][]string{"repo": {"a", "a"}}}) == nil {
		t.Fatal("duplicate worktree")
	}
	list := []Command{{ID: "a", Name: "  label  ", Command: "echo a\r\n\n"}}
	v, err := apply(Values{}, Changes{Fields: map[string]bool{"quickCommands": true}, Values: Values{QuickCommands: &list}})
	if err != nil || (*v.QuickCommands)[0].Command != "echo a" || (*v.QuickCommands)[0].Name != "label" {
		t.Fatal("normalization")
	}
}

func TestBrowserCompatibleStringLimits(t *testing.T) {
	if !validText(strings.Repeat("😀", 20), 40) || validText(strings.Repeat("😀", 21), 40) {
		t.Fatal("UTF-16 limit differs from browser contract")
	}
	if validateValues(Values{HomeWorktrees: map[string][]string{"repo": nil}}) == nil {
		t.Fatal("null worktree array accepted")
	}
}
