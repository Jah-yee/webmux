package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/session"
)

func previewTest(t *testing.T, s *Service, r Repository, tree Worktree) Preview {
	t.Helper()
	preview, err := s.PreviewRemoval(context.Background(), r.ID, tree.ID)
	if err != nil {
		t.Fatal(err)
	}
	return preview
}
func forceRequest(p Preview) RemoveRequest {
	return RemoveRequest{ConfirmationID: p.ConfirmationID, Mode: "force", ConfirmPath: p.Worktree.Path, CloseSessions: true}
}
func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
func TestSafeRemovalProtectsEveryContentCategoryAndRetainsBranches(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	for _, category := range []string{"modified", "untracked", "ignored", "running"} {
		t.Run(category, func(t *testing.T) {
			tree := linkedTest(t, s, r, category)
			var item *session.Session
			switch category {
			case "modified":
				putFile(t, filepath.Join(tree.Path, "tracked"), "changed")
			case "untracked":
				putFile(t, filepath.Join(tree.Path, "untracked"), "new")
			case "ignored":
				putFile(t, filepath.Join(tree.Path, ".env"), "secret")
			case "running":
				var err error
				item, err = s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf READY; read value"}})
				if err != nil {
					t.Fatal(err)
				}
				waitOutput(t, item, "READY")
			}
			preview := previewTest(t, s, r, tree)
			if len(preview.Blockers) == 0 || !preview.ForceAllowed {
				t.Fatalf("incorrect blockers: %+v", preview)
			}
			err := s.RemoveWorktree(context.Background(), r.ID, tree.ID, RemoveRequest{ConfirmationID: preview.ConfirmationID, Mode: "safe"})
			if errorKind(err) != ErrConflict {
				t.Fatalf("unsafe safe removal: %v", err)
			}
			assertExists(t, tree.Path)
			if item != nil && item.Info().Status == session.StatusExited {
				t.Fatal("safe removal closed session")
			}
			if err := s.RemoveWorktree(context.Background(), r.ID, tree.ID, RemoveRequest{ConfirmationID: preview.ConfirmationID, Mode: "safe"}); errorKind(err) != ErrConflict {
				t.Fatal("confirmation reused")
			}
		})
	}
	clean := linkedTest(t, s, r, "clean")
	exited, err := s.CreateSession(context.Background(), SessionInput{Cwd: clean.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf RETAINED"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, exited)
	preview := previewTest(t, s, r, clean)
	if len(preview.Blockers) != 0 {
		t.Fatalf("clean blockers: %+v", preview.Blockers)
	}
	if err := s.RemoveWorktree(context.Background(), r.ID, clean.ID, RemoveRequest{ConfirmationID: preview.ConfirmationID, Mode: "safe"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(clean.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree not removed: %v", err)
	}
	gitTest(t, root, "show-ref", "--verify", "refs/heads/clean")
	if !strings.Contains(string(exited.Buffer()), "RETAINED") || exited.Info().Workspace == nil {
		t.Fatal("exited session record lost")
	}
}
func TestStaleConfirmationsRejectBeforeClosingPTY(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	for _, change := range []string{"expired", "branch", "lock", "new-session", "replacement"} {
		t.Run(change, func(t *testing.T) {
			tree := linkedTest(t, s, r, "stale-"+change)
			item, err := s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf ALIVE; read value"}})
			if err != nil {
				t.Fatal(err)
			}
			waitOutput(t, item, "ALIVE")
			preview := previewTest(t, s, r, tree)
			switch change {
			case "expired":
				s.coord.Lock()
				token := s.confirmations[preview.ConfirmationID]
				token.expires = time.Now().Add(-time.Second)
				s.confirmations[preview.ConfirmationID] = token
				s.coord.Unlock()
			case "branch":
				gitTest(t, tree.Path, "checkout", "--detach")
			case "lock":
				gitTest(t, root, "worktree", "lock", tree.Path)
			case "new-session":
				if _, err := s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "read value"}}); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				if err := os.Rename(tree.Path, tree.Path+"-old"); err != nil {
					t.Fatal(err)
				}
				gitTest(t, root, "worktree", "prune")
				gitTest(t, root, "worktree", "add", tree.Path, "stale-"+change)
			}
			err = s.RemoveWorktree(context.Background(), r.ID, tree.ID, forceRequest(preview))
			if errorKind(err) != ErrConflict {
				t.Fatalf("stale %s confirmation: %v", change, err)
			}
			if item.Info().Status == session.StatusExited {
				t.Fatalf("stale %s confirmation closed PTY", change)
			}
			assertExists(t, tree.Path)
		})
	}
}
func TestForceRemovalRequiresExplicitAuthorizationAndWaitsForPTYCleanup(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, m := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	tree := linkedTest(t, s, r, "force")
	child := filepath.Join(tree.Path, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	// A separate job ignores HUP and retains the PTY. The existing session
	// cleanup must terminate/reap it before worktree removal can succeed.
	script := `set -m; (trap '' HUP; exec sleep 30) & printf 'CHILD=%s\n' "$!"; trap 'printf FINAL_DRAIN; exit 0' HUP; read value`
	item, err := s.CreateSession(context.Background(), SessionInput{Cwd: alias, Preset: "custom", Cmd: []string{"/bin/bash", "-c", script}})
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(t, item, "CHILD=")
	if item.Info().Cwd != child || item.Info().Workspace == nil || item.Info().Workspace.WorktreeID != tree.ID {
		t.Fatalf("symlink launch metadata: %+v", item.Info())
	}
	line := strings.Split(strings.Split(string(item.Buffer()), "CHILD=")[1], "\r")[0]
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(tree.Path, ".env"), "discard")
	for _, invalid := range []string{"path", "authorization"} {
		p := previewTest(t, s, r, tree)
		req := forceRequest(p)
		if invalid == "path" {
			req.ConfirmPath = filepath.Base(tree.Path)
		} else {
			req.CloseSessions = false
		}
		if err := s.RemoveWorktree(context.Background(), r.ID, tree.ID, req); errorKind(err) != ErrInvalid {
			t.Fatalf("missing %s accepted: %v", invalid, err)
		}
		if item.Info().Status == session.StatusExited {
			t.Fatal("invalid force authorization closed PTY")
		}
	}
	p := previewTest(t, s, r, tree)
	if err := s.RemoveWorktree(context.Background(), r.ID, tree.ID, forceRequest(p)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-item.Done():
	default:
		t.Fatal("Git deletion returned before PTY Done")
	}
	if item.Info().Status != session.StatusExited || !strings.Contains(string(item.Buffer()), "FINAL_DRAIN") {
		t.Fatalf("PTY final output not drained: %q %+v", item.Buffer(), item.Info())
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("child process survived deletion: pid=%d err=%v", pid, err)
	}
	if _, err := m.Get(item.Info().ID); err != nil {
		t.Fatal("exit record removed", err)
	}
	gitTest(t, root, "show-ref", "--verify", "refs/heads/force")
	if _, err := os.Stat(tree.Path); !os.IsNotExist(err) {
		t.Fatalf("force did not remove tree: %v", err)
	}
}
func TestUnsupportedRemovalAndReservationRelease(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	main := previewTest(t, s, r, r.Worktrees[0])
	if main.ForceAllowed {
		t.Fatal("main deletion enabled")
	}
	for _, kind := range []string{"nested", "ignored-nested", "submodule", "bare-nested"} {
		t.Run(kind, func(t *testing.T) {
			tree := linkedTest(t, s, r, kind)
			switch kind {
			case "nested", "ignored-nested":
				path := filepath.Join(tree.Path, "nested")
				if kind == "ignored-nested" {
					path = filepath.Join(tree.Path, "build")
				}
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				gitTest(t, path, "init")
			case "submodule":
				oid := gitTest(t, root, "rev-parse", "HEAD")
				gitTest(t, tree.Path, "update-index", "--add", "--cacheinfo", "160000,"+oid+",module")
			case "bare-nested":
				gitTest(t, tree.Path, "init", "--bare", filepath.Join(tree.Path, "nested.git"))
			}
			p := previewTest(t, s, r, tree)
			if p.ForceAllowed {
				t.Fatalf("%s enabled force", kind)
			}
			if err := s.RemoveWorktree(context.Background(), r.ID, tree.ID, forceRequest(p)); errorKind(err) != ErrConflict {
				t.Fatalf("unsupported removal: %v", err)
			}
			item, err := s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf UNRESERVED"}})
			if err != nil {
				t.Fatal("reservation leaked", err)
			}
			waitDone(t, item)
		})
	}
}
func TestDeletionReservationsBlockDirectAndSymlinkChildLaunches(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	tree := linkedTest(t, s, r, "concurrency")
	child := filepath.Join(tree.Path, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	if err := s.reserve(context.Background(), tree.Path); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, path := range []string{tree.Path, child, alias} {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			_, err := s.CreateSession(context.Background(), SessionInput{Cwd: path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf BYPASS"}})
			if errorKind(err) != ErrConflict {
				t.Errorf("deletion reservation bypassed by %q: %v", path, err)
			}
		}(path)
	}
	wg.Wait()
	s.unreserve(tree.Path)
	item, err := s.CreateSession(context.Background(), SessionInput{Cwd: alias, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf RELEASED"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
}
func TestDeleteWaitsForInflightLaunchThenRejectsStaleSessionSet(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, m := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	tree := linkedTest(t, s, r, "inflight")
	p := previewTest(t, s, r, tree)
	if err := s.beginStart(tree.Path); err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	go func() { removed <- s.RemoveWorktree(context.Background(), r.ID, tree.ID, forceRequest(p)) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.coord.Lock()
		reserved := s.reservations[tree.Path]
		s.coord.Unlock()
		if reserved {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("removal did not acquire reservation")
		}
		time.Sleep(time.Millisecond)
	}
	item, err := m.Create(tree.Path, "custom", []string{"/bin/sh", "-c", "printf EARLIER; read value"}, nil, "", nil)
	if err != nil {
		s.endStart(tree.Path)
		t.Fatal(err)
	}
	s.endStart(tree.Path)
	select {
	case err := <-removed:
		if errorKind(err) != ErrConflict {
			t.Fatalf("inflight session not reconfirmed: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("inflight launch/removal deadlock")
	}
	if item.Info().Status == session.StatusExited {
		t.Fatal("newly completed launch closed without confirmation")
	}
	assertExists(t, tree.Path)
}
func TestRemovalCancellationAndGitFailurePreserveTruth(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	tree := linkedTest(t, s, r, "failed-remove")
	item, err := s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf ALIVE; read value"}})
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(t, item, "ALIVE")
	p := previewTest(t, s, r, tree)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.RemoveWorktree(ctx, r.ID, tree.ID, forceRequest(p)); errorKind(err) != ErrTimeout {
		t.Fatalf("cancelled removal: %v", err)
	}
	if item.Info().Status == session.StatusExited {
		t.Fatal("cancelled request closed PTY")
	}
	// Use a controlled failing Git executable only for remove, preserving all real
	// validation/status commands. Verify one --force and no alternate cleanup.
	realGit := s.gitPath
	wrapper := filepath.Join(t.TempDir(), "git")
	log := filepath.Join(t.TempDir(), "argv")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$3\" = worktree ] && [ \"$4\" = remove ]; then printf '%%s\\n' \"$@\" > %q; exit 71; fi\nexec %q \"$@\"\n", log, realGit)
	putFile(t, wrapper, script)
	if err := os.Chmod(wrapper, 0700); err != nil {
		t.Fatal(err)
	}
	s.gitPath = wrapper
	p = previewTest(t, s, r, tree)
	if err := s.RemoveWorktree(context.Background(), r.ID, tree.ID, forceRequest(p)); err == nil {
		t.Fatal("failed Git removal reported complete success")
	}
	waitDone(t, item)
	assertExists(t, tree.Path)
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(args), "--force\n") != 1 {
		t.Fatalf("not single force: %s", args)
	}
	retry, err := s.CreateSession(context.Background(), SessionInput{Cwd: tree.Path, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf RETRY"}})
	if err != nil {
		t.Fatal("failure leaked reservation", err)
	}
	waitDone(t, retry)
}
