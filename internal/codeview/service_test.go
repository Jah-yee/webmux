package codeview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rainchan/webmux/internal/session"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func setup(t *testing.T) (*Service, string, string) {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	m := session.NewManager(session.Options{DefaultCwd: root})
	item, e := m.Create(root, "custom", []string{"/bin/sh"}, nil, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	s := New(m)
	t.Cleanup(func() {
		s.Close()
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		m.Shutdown(ctx)
	})
	return s, item.Info().ID, root
}
func testGitCommand(root string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com", "LC_ALL=C")
	return cmd
}
func gitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := testGitCommand(root, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if e := session.RunCommand(cmd); e != nil {
		t.Fatalf("git %v: %s %v", args, out.String(), e)
	}
	return out.String()
}
func write(t *testing.T, root, p, text string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(root, p), []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func TestPlainFilesBoundariesAndDetection(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	write(t, root, "normal.go", "one\ntwo\n")
	write(t, root, "a \"\n.go", "unicode\n")
	os.Mkdir(filepath.Join(root, ".github"), 0700)
	os.Mkdir(filepath.Join(root, ".git"), 0700)
	write(t, root, ".git/secret", "hidden")
	outside := t.TempDir()
	write(t, outside, "secret", "external")
	os.Symlink(outside, filepath.Join(root, "link"))
	unix.Mkfifo(filepath.Join(root, "fifo"), 0600)
	os.Mkdir(filepath.Join(root, "nested"), 0700)
	os.Mkdir(filepath.Join(root, "nested/.git"), 0700)
	c, e := s.Context(context.Background(), id)
	if e != nil || c.Diff {
		t.Fatalf("context: %+v %v", c, e)
	}
	page, e := s.Tree(context.Background(), id, c.ID, "", false, "")
	if e != nil {
		t.Fatal(e)
	}
	names := map[string]Node{}
	for _, n := range page.Children {
		names[n.Name] = n
	}
	if _, ok := names[".git"]; ok {
		t.Fatal("Git data exposed")
	}
	if names[".github"].Kind != "directory" || names["link"].Kind != "symlink" || names["nested"].Kind != "boundary" {
		t.Fatalf("nodes %+v", names)
	}
	for _, p := range []string{"../secret", ".git/secret", "link/secret", "fifo", "nested", "nested/.git/secret"} {
		if _, e = s.File(context.Background(), id, c.ID, nodeID(p)); errorKind(e) != ErrInvalid {
			t.Errorf("path %q: %v", p, e)
		}
	}
	f, e := s.File(context.Background(), id, c.ID, nodeID("normal.go"))
	if e != nil || f.Document.Lines != 2 {
		t.Fatalf("file %+v %v", f, e)
	}
	write(t, root, "normal.go", "changed\n")
	check, e := s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{f.Snapshot}})
	if e != nil || check.Snapshots[f.Snapshot] != "updated" {
		t.Fatalf("check %+v %v", check, e)
	}
	frozen, e := s.SnapshotFile(context.Background(), id, f.Snapshot, "")
	if e != nil || frozen.Document.Text != "one\ntwo\n" {
		t.Fatal("snapshot mixed live content")
	}
	os.Remove(filepath.Join(root, "normal.go"))
	check, e = s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{f.Snapshot}})
	if e != nil || check.Snapshots[f.Snapshot] != "missing" {
		t.Fatalf("missing: %+v %v", check, e)
	}
}
func TestFileLimitsEncodingAndIdentity(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	c, _ := s.Context(context.Background(), id)
	cases := map[string]string{"empty": "", "crlf": "a\r\nb\r\n", "nonewline": "last", "binary": "a\x00b", "encoding": "\xff", "bytes": strings.Repeat("x", MaxBytes+1), "lines": strings.Repeat("x\n", MaxLines+1)}
	for name, text := range cases {
		write(t, root, name, text)
		f, e := s.File(context.Background(), id, c.ID, nodeID(name))
		if e != nil {
			t.Fatal(e)
		}
		if (name == "bytes" || name == "lines") && (f.Document.Kind != "too-large" || f.Document.Text != "") {
			t.Fatalf("truncated full file %+v", f.Document)
		}
		if name == "empty" && f.Document.Lines != 0 {
			t.Fatal("empty line invented")
		}
		if name == "crlf" && f.Document.Lines != 2 {
			t.Fatal("CRLF line mapping")
		}
		if name == "binary" && f.Document.Kind != "binary" {
			t.Fatal("binary")
		}
		if name == "encoding" && f.Document.Kind != "unsupported" {
			t.Fatal("encoding")
		}
	}
	os.Rename(root, root+"-old")
	defer os.RemoveAll(root + "-old")
	os.Mkdir(root, 0700)
	if _, e := s.Context(context.Background(), id); errorKind(e) != ErrConflict {
		t.Fatalf("root replacement: %v", e)
	}
}
func TestGitDiffSnapshotsSourcesAndIgnored(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	write(t, root, ".gitignore", ".env\nignored/\ntracked.go\n")
	write(t, root, ".env", "private")
	os.Mkdir(filepath.Join(root, "ignored"), 0700)
	write(t, root, "ignored/test", "hidden")
	var base strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&base, "line %d\n", i)
	}
	write(t, root, "tracked.go", base.String())
	gitTest(t, root, "add", "-f", "tracked.go")
	gitTest(t, root, "add", ".gitignore")
	gitTest(t, root, "commit", "-qm", "initial")
	staged := strings.Replace(base.String(), "line 50\n", "staged 50\n", 1)
	write(t, root, "tracked.go", staged)
	gitTest(t, root, "add", "-f", "tracked.go")
	work := strings.Replace(staged, "line 80\n", "working 80\n", 1)
	write(t, root, "tracked.go", work)
	write(t, root, "new file\n.txt", "new\n")
	c, e := s.Context(context.Background(), id)
	if e != nil || !c.Diff {
		t.Fatal(c, e)
	}
	page, e := s.Tree(context.Background(), id, c.ID, "", false, "")
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, n := range page.Children {
		seen[n.Name] = true
	}
	if !seen["tracked.go"] || seen[".env"] || seen["ignored"] {
		t.Fatalf("ignore: %+v", seen)
	}
	full, e := s.Tree(context.Background(), id, c.ID, "", true, "")
	if e != nil || len(full.Children) <= len(page.Children) {
		t.Fatal("includeIgnored", e)
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil || len(changes.Entries) != 3 {
		t.Fatalf("changes %+v %v", changes, e)
	}
	for _, change := range changes.Entries {
		d, e := s.Diff(context.Background(), id, c.ID, change.ID)
		if e != nil {
			t.Fatal(e)
		}
		if change.Group == "staged" && (d.Old.Source != "head" || d.New.Source != "index" || d.New.Text != staged) {
			t.Fatal("index source mixed")
		}
		if change.Group == "unstaged" {
			if d.Old.Text != staged || d.New.Text != work {
				t.Fatal("unstaged sources")
			}
			write(t, root, "tracked.go", "external modification\n")
			frozen, e := s.SnapshotFile(context.Background(), id, d.Snapshot, "new")
			if e != nil || frozen.Document.Text != work {
				t.Fatal("captured new source changed", e)
			}
			old, e := s.SnapshotFile(context.Background(), id, d.Snapshot, "old")
			if e != nil || old.Document.Text != staged {
				t.Fatal("old full source")
			}
			write(t, root, "tracked.go", work)
		}
	}
}
func TestPaginationSearchAndExpiry(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	for i := 0; i < 205; i++ {
		write(t, root, fmt.Sprintf("file-%03d.txt", i), "text")
	}
	c, _ := s.Context(context.Background(), id)
	first, e := s.Tree(context.Background(), id, c.ID, "", false, "")
	if e != nil || len(first.Children) != 200 || first.Next == "" {
		t.Fatal(first, e)
	}
	second, e := s.Tree(context.Background(), id, c.ID, "", false, first.Next)
	if e != nil || len(second.Children) != 5 {
		t.Fatal(second, e)
	}
	write(t, root, "new.txt", "new")
	if _, e = s.Tree(context.Background(), id, c.ID, "", false, first.Next); errorKind(e) != ErrConflict {
		t.Fatal("stale cursor", e)
	}
	found, e := s.Search(context.Background(), id, c.ID, "file-", false)
	if e != nil || len(found.Children) != 200 || found.Complete {
		t.Fatal("search limit", found, e)
	}
	f, e := s.File(context.Background(), id, c.ID, nodeID("new.txt"))
	if e != nil {
		t.Fatal(e)
	}
	other, e := s.manager.Create(root, "custom", []string{"/bin/sh"}, nil, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SnapshotFile(context.Background(), other.Info().ID, f.Snapshot, ""); errorKind(e) != ErrInvalid {
		t.Fatal("cross session", e)
	}
	s.mu.Lock()
	s.snapshots[f.Snapshot].created = time.Now().Add(-Lifetime - time.Second)
	s.mu.Unlock()
	if _, e = s.SnapshotFile(context.Background(), id, f.Snapshot, ""); errorKind(e) != ErrGone {
		t.Fatal("expired", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = s.Search(ctx, id, c.ID, "", true); errorKind(e) != ErrTimeout {
		t.Fatal("cancel", e)
	}
}
func TestUnbornAndDeletedSources(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	write(t, root, "file.txt", "first\n")
	gitTest(t, root, "add", "file.txt")
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil || len(changes.Entries) != 1 {
		t.Fatal(changes, e)
	}
	d, e := s.Diff(context.Background(), id, c.ID, changes.Entries[0].ID)
	if e != nil || d.Old.Source != "empty" || d.New.Source != "index" {
		t.Fatal(d, e)
	}
	gitTest(t, root, "commit", "-qm", "first")
	os.Remove(filepath.Join(root, "file.txt"))
	changes, e = s.Changes(context.Background(), id, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	d, e = s.Diff(context.Background(), id, c.ID, changes.Entries[0].ID)
	if e != nil || d.New.Source != "empty" || d.Old.Text != "first\n" {
		t.Fatal(d, e)
	}
	f, e := s.SnapshotFile(context.Background(), id, d.Snapshot, "old")
	if e != nil || f.Document.Text != "first\n" {
		t.Fatal(f, e)
	}
}

func TestLinkedSubdirectoryAndNoGitBinding(t *testing.T) {
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	write(t, root, "base.txt", "base")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "base")
	linked := filepath.Join(t.TempDir(), "linked tree")
	gitTest(t, root, "worktree", "add", "-qb", "linked-test", linked)
	os.Mkdir(filepath.Join(linked, "src"), 0700)
	item, e := s.manager.Create(filepath.Join(linked, "src"), "custom", []string{"/bin/sh"}, nil, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Context(context.Background(), item.Info().ID)
	canonical, _ := filepath.EvalSymlinks(linked)
	if e != nil || c.Root != canonical || !c.Diff {
		t.Fatalf("linked root %+v %v", c, e)
	}
	if _, e = s.File(context.Background(), item.Info().ID, c.ID, nodeID("base.txt")); e != nil {
		t.Fatal(e)
	}
	sub := filepath.Join(root, "plain")
	os.Mkdir(sub, 0700)
	plain, e := s.manager.Create(sub, "custom", []string{"/bin/sh"}, nil, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	fallback, e := s.Context(context.Background(), plain.Info().ID)
	if e != nil || fallback.Root != sub || fallback.Diff {
		t.Fatal(fallback, e)
	}
	t.Setenv("PATH", originalPath)
	again, e := s.Context(context.Background(), plain.Info().ID)
	if e != nil || again.ID != fallback.ID || again.Root != sub || again.Diff {
		t.Fatal("rebound no-Git context", again, e)
	}
	_, _ = s.Context(context.Background(), id)
}

func TestCacheEvictionAndSessionCleanup(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	write(t, root, "file.txt", "content")
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	f, e := s.File(context.Background(), id, c.ID, nodeID("file.txt"))
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 65; i++ {
		s.put(&snapshot{session: id, context: c.ID, size: 1024 * 1024})
	}
	if _, e = s.get(id, f.Snapshot); errorKind(e) != ErrGone {
		t.Fatalf("budget did not evict old snapshot: %v", e)
	}
	s.mu.Lock()
	if s.bytes > CacheBytes {
		t.Fatal("cache exceeded budget")
	}
	s.mu.Unlock()
	s.manager.Close(id)
	item, _ := s.manager.Get(id)
	select {
	case <-item.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session close timeout")
	}
	if e = s.manager.Remove(id); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		count := len(s.snapshots)
		roots := len(s.roots)
		s.mu.Unlock()
		if count == 0 && roots == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("removed session retained code resources")
}

func TestRenameCRLFAndReadOnlyIndex(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	write(t, root, ".gitattributes", "*.txt text eol=crlf\n")
	write(t, root, "old name.txt", "one\r\ntwo\r\nlast")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "initial")
	gitTest(t, root, "mv", "old name.txt", "new name.txt")
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	before, e := os.ReadFile(filepath.Join(root, ".git/index"))
	if e != nil {
		t.Fatal(e)
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(changes.Entries) != 1 || !strings.HasPrefix(changes.Entries[0].Status, "R") {
		t.Fatalf("rename %+v", changes)
	}
	d, e := s.Diff(context.Background(), id, c.ID, changes.Entries[0].ID)
	if e != nil || d.Old.Path != "old name.txt" || d.New.Path != "new name.txt" {
		t.Fatal(d, e)
	}
	after, e := os.ReadFile(filepath.Join(root, ".git/index"))
	if e != nil || string(before) != string(after) {
		t.Fatal("code reads wrote Git index")
	}
	write(t, root, "new name.txt", "one\r\nchanged\r\nlast")
	changes, e = s.Changes(context.Background(), id, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range changes.Entries {
		if change.Group != "unstaged" {
			continue
		}
		d, e = s.Diff(context.Background(), id, c.ID, change.ID)
		if e != nil {
			t.Fatal(e)
		}
		if d.Old.Text != "one\ntwo\nlast" || d.New.Text != "one\r\nchanged\r\nlast" {
			t.Fatal("raw CRLF or final newline was changed", d)
		}
	}
}

func TestComparisonCapturesLargeTextsWithoutPatchOrTemporaryFiles(t *testing.T) {
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	old := strings.Repeat(strings.Repeat("a", 70)+"\n", MaxLines)
	next := strings.Repeat(strings.Repeat("b", 70)+"\n", MaxLines)
	write(t, root, "large.txt", old)
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "initial")
	write(t, root, "large.txt", next)
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil || len(changes.Entries) != 1 {
		t.Fatal(changes, e)
	}
	// Capturing a pair must work with no writable temporary directory. The
	// combined text exceeds the former 2 MiB patch-output budget.
	t.Setenv("TMPDIR", filepath.Join(root, "no-such-temp-directory"))
	d, e := s.Diff(context.Background(), id, c.ID, changes.Entries[0].ID)
	if e != nil || d.Kind != "text" || d.Old.Text != old || d.New.Text != next {
		t.Fatal("pair capture", e)
	}
	raw, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(raw, &fields); e != nil {
		t.Fatal(e)
	}
	if _, found := fields["blocks"]; found {
		t.Fatal("server returned presentation blocks")
	}
	write(t, root, "large.txt", "later version\n")
	frozen, e := s.SnapshotFile(context.Background(), id, d.Snapshot, "new")
	if e != nil || frozen.Document.Text != next {
		t.Fatal("snapshot lost original text", e)
	}
}

func TestGitConflictAndSubmoduleMetadata(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	write(t, root, "conflict.txt", "base\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "base")
	gitTest(t, root, "checkout", "-qb", "side")
	write(t, root, "conflict.txt", "side\n")
	gitTest(t, root, "commit", "-qam", "side")
	gitTest(t, root, "checkout", "-")
	write(t, root, "conflict.txt", "main\n")
	gitTest(t, root, "commit", "-qam", "main")
	cmd := testGitCommand(root, "merge", "side")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	mergeErr := session.RunCommand(cmd)
	var exit *exec.ExitError
	if !errors.As(mergeErr, &exit) || exit.ExitCode() != 1 || !strings.Contains(output.String(), "CONFLICT") {
		t.Fatalf("expected merge conflict, got %v: %s", mergeErr, output.String())
	}
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range changes.Entries {
		if strings.HasPrefix(change.Status, "U") {
			d, e := s.Diff(context.Background(), id, c.ID, change.ID)
			if e != nil || d.Kind != "metadata" {
				t.Fatal(d, e)
			}
			return
		}
	}
	t.Fatal("conflict not listed")
}

func TestCurrentIndexFileRefreshAndDetection(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	write(t, root, "file.txt", "first\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "first")
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	f, e := s.FileSource(context.Background(), id, c.ID, nodeID("file.txt"), "index")
	if e != nil || f.Document.Source != "index" || f.Document.Text != "first\n" {
		t.Fatal(f, e)
	}
	write(t, root, "file.txt", "indexed\n")
	gitTest(t, root, "add", "file.txt")
	write(t, root, "file.txt", "working\n")
	r, e := s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{f.Snapshot}})
	if e != nil || r.Snapshots[f.Snapshot] != "updated" {
		t.Fatal(r, e)
	}
	next, e := s.FileSource(context.Background(), id, c.ID, nodeID("file.txt"), "index")
	if e != nil || next.Document.Text != "indexed\n" {
		t.Fatal(next, e)
	}
	head, e := s.FileSource(context.Background(), id, c.ID, nodeID("file.txt"), "head")
	if e != nil || head.Document.Text != "first\n" {
		t.Fatal(head, e)
	}
	if _, e = s.FileSource(context.Background(), id, c.ID, nodeID("file.txt"), "HEAD~1"); errorKind(e) != ErrInvalid {
		t.Fatal("arbitrary revision accepted", e)
	}
}

func TestGitAdministrationCannotBeBrowseRoot(t *testing.T) {
	t.Parallel()
	s, _, root := setup(t)
	gitTest(t, root, "init", "-q")
	item, e := s.manager.Create(filepath.Join(root, ".git"), "custom", []string{"/bin/sh"}, nil, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Context(context.Background(), item.Info().ID); errorKind(e) != ErrConflict {
		t.Fatal("Git admin root accepted", e)
	}
}

func TestInvalidUTF8PathsUseOpaqueIdentity(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	raw := string([]byte{'a', 0xff, '.', 't', 'x', 't'})
	doc := document(root, raw, "worktree", "", []byte("readable bytes\n"))
	if doc.Referenceable || doc.NodeID != nodeID(raw) || !strings.Contains(doc.Path, "\\xff") {
		t.Fatalf("invalid path document lost opaque identity: %+v", doc)
	}
	if e := os.WriteFile(filepath.Join(root, raw), []byte("readable bytes\n"), 0600); e != nil {
		if errors.Is(e, syscall.EILSEQ) {
			t.Log("filesystem rejects invalid UTF-8 names; opaque document identity and disabled references verified")
			return
		}
		t.Fatal(e)
	}
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	page, e := s.Tree(context.Background(), id, c.ID, "", true, "")
	if e != nil || len(page.Children) != 1 {
		t.Fatal(page, e)
	}
	n := page.Children[0]
	if n.Referenceable || !strings.Contains(n.Name, "\\xff") {
		t.Fatal("invalid path display", n)
	}
	f, e := s.File(context.Background(), id, c.ID, n.ID)
	if e != nil || f.Document.Text != "readable bytes\n" || f.Document.Referenceable || f.Document.NodeID != n.ID {
		t.Fatal(f, e)
	}
	r, e := s.Check(context.Background(), id, CheckRequest{Context: c.ID, Snapshots: []string{f.Snapshot}})
	if e != nil || r.Snapshots[f.Snapshot] != "unchanged" {
		t.Fatal("raw identity lost", r, e)
	}
}

func TestCodeReadsDoNotRunCleanFiltersOrExternalDiff(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	write(t, root, "file.txt", "first\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "first")
	write(t, root, ".gitattributes", "file.txt filter=spy diff=spy\n")
	write(t, root, "file.txt", "changed\n")
	marker := filepath.Join(root, "filter-ran")
	command := fmt.Sprintf("printf ran > %q; cat", marker)
	gitTest(t, root, "config", "filter.spy.clean", command)
	gitTest(t, root, "config", "filter.spy.required", "true")
	gitTest(t, root, "config", "diff.spy.command", command)
	gitTest(t, root, "config", "diff.spy.textconv", command)
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Tree(context.Background(), id, c.ID, "", false, ""); e != nil {
		t.Fatal(e)
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range changes.Entries {
		if change.NewPath != "file.txt" {
			continue
		}
		d, e := s.Diff(context.Background(), id, c.ID, change.ID)
		if e != nil || d.Kind != "metadata" {
			t.Fatal("filter must degrade", d, e)
		}
	}
	if _, e = os.Stat(marker); !os.IsNotExist(e) {
		t.Fatalf("read-only code comparison executed configured helper: %v", e)
	}
}

// Shells keep logical $PWD paths through symlinks (macOS /tmp, linked home
// directories) while the code root is canonical.
func TestLocateResolvesLogicalBasesAgainstCanonicalRoot(t *testing.T) {
	t.Parallel()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	gitTest(t, root, "init", "-q")
	for _, dir := range []string{"src", "other"} {
		if e := os.Mkdir(filepath.Join(root, dir), 0700); e != nil {
			t.Fatal(e)
		}
	}
	write(t, root, "src/main.go", "package main\n")
	write(t, root, "other/x.go", "package other\n")
	alias := filepath.Join(t.TempDir(), "alias")
	if e := os.Symlink(root, alias); e != nil {
		t.Fatal(e)
	}
	m := session.NewManager(session.Options{DefaultCwd: root})
	s := New(m)
	t.Cleanup(func() {
		s.Close()
		ctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		m.Shutdown(ctx)
	})
	start := func(host string) string {
		t.Helper()
		argv := []string{"/bin/sh", "-c", "sleep 30"}
		if host != "" {
			argv = []string{"/bin/sh", "-c", `printf '\033]7;file://%s%s\007' "$1" "$2"; sleep 30`, "sh", host, filepath.Join(alias, "other")}
		}
		item, e := m.Create(filepath.Join(alias, "src"), "custom", argv, nil, "", nil)
		if e != nil {
			t.Fatal(e)
		}
		id := item.Info().ID
		for deadline := time.Now().Add(5 * time.Second); host != "" && item.Info().ReportedCwd == nil; {
			if time.Now().After(deadline) {
				t.Fatal("OSC 7 report was not recorded")
			}
			time.Sleep(10 * time.Millisecond)
		}
		return id
	}
	locate := func(id, input string) (Location, error) {
		t.Helper()
		c, e := s.Context(context.Background(), id)
		if e != nil || c.Root != root {
			t.Fatalf("context %+v %v", c, e)
		}
		return s.Locate(context.Background(), id, c.ID, input)
	}
	launch, local, remote := start(""), start("localhost"), start("elsewhere.invalid")
	for _, tc := range []struct {
		id, input, path, kind string
	}{
		{launch, "main.go", "src/main.go", "file"},
		{launch, " ../other/x.go ", "other/x.go", "file"},
		{launch, filepath.Join(alias, "other"), "other", "directory"},
		{launch, "..", "", "directory"},
		{local, "x.go", "other/x.go", "file"},
	} {
		got, e := locate(tc.id, tc.input)
		if e != nil || got.Path != tc.path || got.Kind != tc.kind || got.NodeID != nodeID(tc.path) {
			t.Errorf("locate %q: %+v %v", tc.input, got, e)
		}
	}
	for _, tc := range []struct {
		id, input string
		kind      error
	}{
		{launch, "missing.go", ErrNotFound},
		{launch, "main.go/child", ErrNotFound},
		{launch, "../..", ErrForbidden},
		{launch, filepath.Dir(root), ErrForbidden},
		{launch, "../.git/config", ErrInvalid},
		{launch, " ", ErrInvalid},
		{remote, "x.go", ErrConflict},
	} {
		if got, e := locate(tc.id, tc.input); errorKind(e) != tc.kind {
			t.Errorf("locate %q: want %v, got %+v %v", tc.input, tc.kind, got, e)
		}
	}
}
