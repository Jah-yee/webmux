package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/session"
)

func TestMultiplePTYsKeepRealChildCwdAndIndependentBuffers(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, m := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	tree := linkedTest(t, s, r, "sessions")
	child := filepath.Join(tree.Path, "packages", "app")
	if err := os.MkdirAll(child, 0700); err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateSession(context.Background(), SessionInput{Cwd: child, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf FIRST; read value; printf 'GOT:%s' \"$value\""}, Name: "first", Workspace: &Reference{r.ID, tree.ID}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateSession(context.Background(), SessionInput{Cwd: child, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf SECOND; read value"}, Name: "second"})
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(t, first, "FIRST")
	waitOutput(t, second, "SECOND")
	if first.Info().Cwd != child || first.Info().Workspace == nil || second.Info().Workspace == nil || first.Info().ID == second.Info().ID {
		t.Fatalf("independent launch metadata: %+v %+v", first.Info(), second.Info())
	}
	if err := first.Write([]byte("one\n")); err != nil {
		t.Fatal(err)
	}
	waitDone(t, first)
	if !strings.Contains(string(first.Buffer()), "GOT:one") || strings.Contains(string(second.Buffer()), "GOT:one") || second.Info().Status == session.StatusExited {
		t.Fatal("PTY input/buffer/exit isolation failed")
	}
	if err := m.Remove(first.Info().ID); err != nil {
		t.Fatal(err)
	}
	assertExists(t, tree.Path)
	// Workspace metadata updates are delivered through the existing list stream.
	sub := m.Subscribe()
	defer m.Unsubscribe(sub)
	<-sub.Out
	if err := s.Unregister(r.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-sub.Out:
		if event.Type != "updated" || event.Session == nil || event.Session.ID != second.Info().ID || event.Session.Workspace != nil {
			t.Fatalf("regroup event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("workspace regroup event missing")
	}
	if second.Info().Status == session.StatusExited {
		t.Fatal("regroup stopped independent PTY")
	}
}
func TestExplicitWorkspaceNeverFallsBackOrMatchesNestedRepository(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	tree := linkedTest(t, s, r, "explicit")
	nested := filepath.Join(tree.Path, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, nested, "init")
	for _, cwd := range []string{root, nested} {
		if _, err := s.CreateSession(context.Background(), SessionInput{Cwd: cwd, Preset: "custom", Cmd: []string{"/bin/sh"}, Workspace: &Reference{r.ID, tree.ID}}); err == nil {
			t.Fatalf("conflicting cwd accepted: %q", cwd)
		}
	}
	direct, err := s.CreateSession(context.Background(), SessionInput{Cwd: nested, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf NESTED"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, direct)
	if direct.Info().Workspace != nil {
		t.Fatal("nested unregistered repository matched outer worktree")
	}
	prior, err := s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf OLD; read value"}})
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(t, prior, "OLD")
	if err := os.Rename(tree.Path, tree.Path+"-gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSession(context.Background(), SessionInput{Preset: "custom", Cmd: []string{"/bin/sh"}, Workspace: &Reference{r.ID, tree.ID}}); err == nil {
		t.Fatal("missing selected tree fell back to default")
	}
	if _, err := s.Refresh(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	if prior.Info().Workspace == nil || prior.Info().Workspace.WorktreeID != tree.ID || prior.Info().Status == session.StatusExited {
		t.Fatal("unavailable workspace session lost association or process")
	}
}
func TestCloseTimeoutDoesNotDeleteTreeAndReleasesReservation(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	tree := linkedTest(t, s, r, "close-timeout")
	item, err := s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/bash", "-c", `trap '' HUP; printf READY; while :; do sleep 1; done`}})
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(t, item, "READY")
	preview := previewTest(t, s, r, tree)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = s.RemoveWorktree(ctx, r.ID, tree.ID, forceRequest(preview))
	if errorKind(err) != ErrTimeout {
		t.Fatalf("close timeout incorrectly succeeded: %v", err)
	}
	assertExists(t, tree.Path)
	waitDone(t, item)
	next, err := s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf RELEASED"}})
	if err != nil {
		t.Fatal("timeout retained reservation", err)
	}
	waitDone(t, next)
}
