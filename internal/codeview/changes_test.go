package codeview

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLargeChangesPaginationLookupAndChecks(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	prefix := filepath.Join(strings.Repeat("a", 160), strings.Repeat("b", 160), strings.Repeat("c", 160), strings.Repeat("d", 160))
	if err := os.MkdirAll(filepath.Join(root, prefix), 0700); err != nil {
		t.Fatal(err)
	}
	const count = 3305
	for i := 0; i < count; i++ {
		write(t, root, filepath.Join(prefix, fmt.Sprintf("file-%04d.txt", i)), "original\n")
	}
	// Large enough to exceed the former whole-list 2 MiB output limit, while
	// each real filesystem path remains below macOS's path-length bound.
	output := gitTest(t, root, "ls-files", "--others", "--exclude-standard", "-z")
	if len(output) <= MaxBytes {
		t.Fatal("fixture did not exceed old output limit")
	}
	c, err := s.Context(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Changes(context.Background(), id, c.ID)
	if err != nil || first.Total != count || len(first.Entries) != ChangePageSize || first.Next == "" {
		t.Fatal("first page", first.Total, len(first.Entries), err)
	}
	seen := map[string]bool{}
	page := first
	for {
		if page.Version != first.Version || page.Total != count || len(page.Entries) > ChangePageSize {
			t.Fatal("inconsistent page")
		}
		for _, entry := range page.Entries {
			if seen[entry.ID] {
				t.Fatal("duplicate entry")
			}
			seen[entry.ID] = true
		}
		if page.Next == "" {
			break
		}
		page, err = s.Changes(context.Background(), id, c.ID, ChangeQuery{Cursor: page.Next})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != count {
		t.Fatal("missing entries", len(seen))
	}
	path := filepath.ToSlash(filepath.Join(prefix, fmt.Sprintf("file-%04d.txt", count-1)))
	lookup, err := s.Changes(context.Background(), id, c.ID, ChangeQuery{NodeID: nodeID(path)})
	if err != nil || len(lookup.Entries) != 1 || lookup.Entries[0].NewPath != path {
		t.Fatal("late lookup", err)
	}
	diff, err := s.Diff(context.Background(), id, c.ID, lookup.Entries[0].ID)
	if err != nil || diff.New.Text != "original\n" {
		t.Fatal("late diff", err)
	}
	state, err := s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{diff.Snapshot}, Changes: first.Version})
	if err != nil || state.Snapshots[diff.Snapshot] != "unchanged" || state.Changes != "unchanged" {
		t.Fatal(state, err)
	}
	write(t, root, path, "updated\n")
	state, err = s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{diff.Snapshot}, Changes: first.Version})
	if err != nil || state.Snapshots[diff.Snapshot] != "updated" {
		t.Fatal(state, err)
	}
	if _, err = s.Tree(context.Background(), id, c.ID, "", false, ""); err != nil {
		t.Fatal("large status blocked file navigation", err)
	}
	filtered, err := s.Changes(context.Background(), id, c.ID, ChangeQuery{Q: "file-3304"})
	if err != nil || filtered.Total != 1 || len(filtered.Entries) != 1 {
		t.Fatal("path filter", err)
	}
	if _, err = s.Changes(context.Background(), id, c.ID, ChangeQuery{Cursor: first.Next, Q: "other"}); errorKind(err) != ErrConflict {
		t.Fatal("cursor reused across filters", err)
	}
	write(t, root, "new-file.txt", "new\n")
	if _, err = s.Changes(context.Background(), id, c.ID, ChangeQuery{Cursor: first.Next}); errorKind(err) != ErrConflict {
		t.Fatal("stale cursor", err)
	}
	gitTest(t, root, "add", ".")
	lookup, err = s.Changes(context.Background(), id, c.ID, ChangeQuery{NodeID: nodeID(path), Group: "staged"})
	if err != nil || len(lookup.Entries) != 1 {
		t.Fatal("staged lookup", err)
	}
	diff, err = s.Diff(context.Background(), id, c.ID, lookup.Entries[0].ID)
	if err != nil || diff.Old.Source != "empty" || diff.New.Source != "index" || diff.New.Text != "updated\n" {
		t.Fatal("large unborn staged pair", err)
	}
	for _, q := range []ChangeQuery{{Cursor: "bad"}, {Cursor: "bad:-1"}, {Group: "wrong"}, {NodeID: nodeID("../outside")}} {
		if _, err = s.Changes(context.Background(), id, c.ID, q); errorKind(err) != ErrInvalid {
			t.Fatal("invalid query", q, err)
		}
	}
}
