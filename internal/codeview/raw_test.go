package codeview

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func rawBytes(t *testing.T, s *Service, id, sid, side string) []byte {
	t.Helper()
	tr, e := s.Raw(context.Background(), id, sid, side)
	if e != nil {
		t.Fatal(e)
	}
	defer tr.Close()
	var out bytes.Buffer
	if e = tr.WriteTo(context.Background(), &out); e != nil {
		t.Fatal(e)
	}
	if int64(out.Len()) != tr.Size {
		t.Fatalf("size %d want %d", out.Len(), tr.Size)
	}
	return out.Bytes()
}

func TestRawSourcesMatchSnapshotBytes(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	head := pngBytes(t, 1)
	writeBytes(t, root, "pic.png", head)
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "pic")
	index := pngBytes(t, 2)
	writeBytes(t, root, "pic.png", index)
	gitTest(t, root, "add", "pic.png")
	work := bytes.Repeat(pngBytes(t, 3), 2000) // several copy chunks
	writeBytes(t, root, "pic.png", work)
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	f, e := s.File(context.Background(), id, c.ID, nodeID("pic.png"))
	if e != nil {
		t.Fatal(e)
	}
	if got := rawBytes(t, s, id, f.Snapshot, ""); sha256.Sum256(got) != sha256.Sum256(work) {
		t.Fatal("worktree bytes differ")
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range changes.Entries {
		d, e := s.Diff(context.Background(), id, c.ID, change.ID)
		if e != nil {
			t.Fatal(e)
		}
		oldWant, newWant := index, work
		if change.Group == "staged" {
			oldWant, newWant = head, index
			blob := gitTest(t, root, "cat-file", "blob", d.Old.OID)
			if blob != string(head) {
				t.Fatal("fixture")
			}
		}
		if got := rawBytes(t, s, id, d.Snapshot, "old"); sha256.Sum256(got) != sha256.Sum256(oldWant) {
			t.Fatalf("%s old bytes differ", change.Group)
		}
		if got := rawBytes(t, s, id, d.Snapshot, "new"); sha256.Sum256(got) != sha256.Sum256(newWant) {
			t.Fatalf("%s new bytes differ", change.Group)
		}
		for _, side := range []string{"", "both"} {
			if _, e := s.Raw(context.Background(), id, d.Snapshot, side); errorKind(e) != ErrInvalid {
				t.Fatalf("side %q: %v", side, e)
			}
		}
	}
	for _, source := range []string{"index", "head"} {
		g, e := s.FileSource(context.Background(), id, c.ID, nodeID("pic.png"), source)
		if e != nil {
			t.Fatal(e)
		}
		want := head
		if source == "index" {
			want = index
		}
		if got := rawBytes(t, s, id, g.Snapshot, "ignored"); !bytes.Equal(got, want) {
			t.Fatalf("%s bytes differ", source)
		}
	}
}

func TestRawRejectsChangesAndInvalidReferences(t *testing.T) {
	t.Parallel()
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	writeBytes(t, root, "gone.png", pngBytes(t, 5))
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "gone")
	os.Remove(filepath.Join(root, "gone.png"))
	writeBytes(t, root, "pic.png", bytes.Repeat(pngBytes(t, 1), 1000))
	sparse(t, root, "giant.png", pngBytes(t, 1), MaxDownloadBytes+1)
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	open := func(p string) File {
		t.Helper()
		f, e := s.File(context.Background(), id, c.ID, nodeID(p))
		if e != nil {
			t.Fatal(e)
		}
		return f
	}
	f := open("pic.png")
	// Rewritten after the snapshot, before the transfer opens.
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(filepath.Join(root, "pic.png"), later, later)
	if _, e := s.Raw(context.Background(), id, f.Snapshot, ""); errorKind(e) != ErrConflict {
		t.Fatalf("changed before open: %v", e)
	}
	// Rewritten while streaming.
	f = open("pic.png")
	tr, e := s.Raw(context.Background(), id, f.Snapshot, "")
	if e != nil {
		t.Fatal(e)
	}
	rewrite := &hookWriter{hook: func() { writeBytes(t, root, "pic.png", []byte("short")) }}
	if e = tr.WriteTo(context.Background(), rewrite); !errors.Is(e, ErrChanged) {
		t.Fatalf("mid-stream change: %v", e)
	}
	tr.Close()
	tr.Close()

	big := open("giant.png")
	if big.Document.Downloadable {
		t.Fatal("over download limit marked downloadable")
	}
	if _, e := s.Raw(context.Background(), id, big.Snapshot, ""); errorKind(e) != ErrTooLarge {
		t.Fatalf("too large: %v", e)
	}
	changes, e := s.Changes(context.Background(), id, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	var deleted Diff
	for _, change := range changes.Entries {
		if change.OldPath == "gone.png" {
			deleted, e = s.Diff(context.Background(), id, c.ID, change.ID)
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	if deleted.Snapshot == "" {
		t.Fatal("deletion not listed")
	}
	if _, e := s.Raw(context.Background(), id, deleted.Snapshot, "new"); errorKind(e) != ErrNotFound {
		t.Fatalf("empty side: %v", e)
	}
	other, e := s.manager.Create(root, "custom", []string{"/bin/sh"}, nil, "", nil)
	if e != nil {
		t.Fatal(e)
	}
	writeBytes(t, root, "small.png", pngBytes(t, 2))
	small := open("small.png")
	if _, e := s.Raw(context.Background(), other.Info().ID, small.Snapshot, ""); errorKind(e) != ErrInvalid {
		t.Fatalf("cross session: %v", e)
	}
	if _, e := s.Raw(context.Background(), id, "unknown", ""); errorKind(e) != ErrGone {
		t.Fatalf("unknown: %v", e)
	}
	s.mu.Lock()
	s.snapshots[small.Snapshot].created = time.Now().Add(-Lifetime - time.Second)
	s.mu.Unlock()
	if _, e := s.Raw(context.Background(), id, small.Snapshot, ""); errorKind(e) != ErrGone {
		t.Fatalf("expired: %v", e)
	}

	// Four concurrent transfers fill the slots; the fifth is refused at once.
	fresh := open("small.png")
	var held []*Transfer
	for range 4 {
		tr, e := s.Raw(context.Background(), id, fresh.Snapshot, "")
		if e != nil {
			t.Fatal(e)
		}
		held = append(held, tr)
	}
	if _, e := s.Raw(context.Background(), id, fresh.Snapshot, ""); errorKind(e) != ErrBusy {
		t.Fatalf("slots: %v", e)
	}
	held[0].Close()
	tr, e = s.Raw(context.Background(), id, fresh.Snapshot, "")
	if e != nil {
		t.Fatal("slot not released", e)
	}
	tr.Close()
	for _, h := range held[1:] {
		h.Close()
	}
}

type hookWriter struct {
	hook func()
	done bool
	n    int
}

func (h *hookWriter) Write(p []byte) (int, error) {
	if !h.done {
		h.done = true
		h.hook()
	}
	h.n += len(p)
	return len(p), nil
}

// gitChildren counts live git processes whose parent is this test binary.
func gitChildren(t *testing.T) int {
	t.Helper()
	entries, e := os.ReadDir("/proc")
	if e != nil {
		t.Skip("no /proc")
	}
	self := strconv.Itoa(os.Getpid())
	count := 0
	for _, entry := range entries {
		raw, e := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if e != nil {
			continue
		}
		stat := string(raw)
		end := strings.LastIndexByte(stat, ')')
		start := strings.IndexByte(stat, '(')
		if start < 0 || end < start {
			continue
		}
		fields := strings.Fields(stat[end+1:])
		if stat[start+1:end] == "git" && len(fields) > 1 && fields[1] == self && fields[0] != "Z" {
			count++
		}
	}
	return count
}

// Not parallel: counts this process's git children.
func TestRawCancellationStopsGit(t *testing.T) {
	s, id, root := setup(t)
	gitTest(t, root, "init", "-q")
	data := make([]byte, 8<<20)
	rand.Read(data)
	writeBytes(t, root, "noise.bin", data)
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "noise")
	c, e := s.Context(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	f, e := s.FileSource(context.Background(), id, c.ID, nodeID("noise.bin"), "head")
	if e != nil || !f.Document.Downloadable || f.Document.Kind != "too-large" {
		t.Fatal(f.Document, e)
	}
	tr, e := s.Raw(context.Background(), id, f.Snapshot, "")
	if e != nil {
		t.Fatal(e)
	}
	defer tr.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &hookWriter{hook: cancel}
	start := time.Now()
	e = tr.WriteTo(ctx, &slowWriter{w})
	if errorKind(e) != ErrTimeout || time.Since(start) > 3*time.Second {
		t.Fatalf("cancel: %v after %v", e, time.Since(start))
	}
	if w.n >= len(data) {
		t.Fatal("cancelled transfer read everything")
	}
	if n := gitChildren(t); n != 0 {
		t.Fatalf("%d git processes leaked", n)
	}
}

// slowWriter keeps the pipe full so cancellation lands mid-stream.
type slowWriter struct{ w *hookWriter }

func (s *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(5 * time.Millisecond)
	return s.w.Write(p)
}
