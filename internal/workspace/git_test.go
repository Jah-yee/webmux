package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNULParsersPreservePathsAndRenameRecords(t *testing.T) {
	// Absent roots keep the parser test free of host symlinks such as macOS /tmp.
	path := "/webmux-absent/path with spaces\nand newline"
	trees, err := parseWorktrees([]byte("worktree "+path+"\x00HEAD abc123\x00branch refs/heads/main\x00future value\x00\x00worktree /webmux-absent/linked\x00HEAD def456\x00detached\x00locked reason with\nnewline\x00\x00"), "repo")
	if err != nil || len(trees) != 2 || trees[0].Path != path || !trees[1].Detached || trees[1].LockReason != "reason with\nnewline" {
		t.Fatalf("NUL parser: %+v %v", trees, err)
	}
	dirty, untracked, ignored, err := parseStatus([]byte("R  destination\x00!! source\x00?? file\nname\x00!! .env\x00"))
	if err != nil || !dirty || !untracked || !ignored {
		t.Fatalf("status: %v %v %v %v", dirty, untracked, ignored, err)
	}
	for _, malformed := range [][]byte{[]byte("R  new\x00"), []byte("?? path"), []byte("x\x00")} {
		if _, _, _, err := parseStatus(malformed); err == nil {
			t.Fatalf("accepted malformed status %q", malformed)
		}
	}
}
func TestSnapshotsTrackExternalStateAndProtectedContents(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	path := filepath.Join(t.TempDir(), "external\nworktree")
	gitTest(t, root, "worktree", "add", "-b", "external", path, "HEAD")
	gitTest(t, path, "checkout", "--detach")
	putFile(t, filepath.Join(path, "tracked"), "modified\n")
	putFile(t, filepath.Join(path, "unknown\nname"), "untracked")
	putFile(t, filepath.Join(path, ".env"), "secret")
	gitTest(t, root, "worktree", "lock", "--reason", "keep\nreason", path)
	snapshot, err := s.Refresh(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := findTree(snapshot, worktreeID(r.ID, path))
	if err != nil {
		t.Fatal(err)
	}
	if !tree.Detached || tree.Branch != nil || tree.Head == nil || !tree.Locked || tree.LockReason != "keep\nreason" || tree.Dirty == nil || !*tree.Dirty || !tree.HasUntracked || !tree.HasIgnored {
		t.Fatalf("external state: %+v", tree)
	}
	preview, err := s.PreviewRemoval(context.Background(), r.ID, tree.ID)
	if err != nil || preview.ForceAllowed {
		t.Fatalf("locked deletion allowed: %+v %v", preview, err)
	}
	gitTest(t, root, "worktree", "unlock", path)
	if err := os.Rename(path, path+"-missing"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.Refresh(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	tree, err = findTree(snapshot, tree.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Available || tree.Dirty != nil || tree.Error == "" {
		t.Fatalf("missing tree shown clean: %+v", tree)
	}
}
func TestBareUnbornAndBranchBaselines(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	unborn := t.TempDir()
	gitTest(t, unborn, "init", "-b", "main")
	s, _ := testService(t, t.TempDir(), unborn)
	r := registerTest(t, s, unborn)
	branches, err := s.Branches(context.Background(), r.ID)
	if err != nil || branches.DefaultBase != nil || len(branches.Bases) != 0 {
		t.Fatalf("unborn branches: %+v %v", branches, err)
	}
	if _, err := s.CreateWorktree(context.Background(), r.ID, CreateRequest{Path: filepath.Join(t.TempDir(), "unborn"), Mode: "new", Branch: "topic", Base: "HEAD"}); errorKind(err) != ErrInvalid {
		t.Fatalf("unborn creation: %v", err)
	}
	item, err := s.CreateSession(context.Background(), SessionInput{Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf UNBORN"}, Workspace: &Reference{r.ID, r.Worktrees[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
	root := initRepo(t)
	bare := filepath.Join(t.TempDir(), "bare.git")
	gitTest(t, root, "clone", "--bare", root, bare)
	br := registerTest(t, s, bare)
	branches, err = s.Branches(context.Background(), br.ID)
	if err != nil || branches.DefaultBase != nil || len(branches.Bases) == 0 || branches.DefaultParent != bare+".worktrees" {
		t.Fatalf("bare branches: %+v %v", branches, err)
	}
	if _, err := s.CreateSession(context.Background(), SessionInput{Preset: "custom", Cmd: []string{"/bin/sh"}, Workspace: &Reference{br.ID, br.Worktrees[0].ID}}); errorKind(err) != ErrConflict {
		t.Fatalf("bare launch: %v", err)
	}
	tree, err := s.CreateWorktree(context.Background(), br.ID, CreateRequest{Path: filepath.Join(t.TempDir(), "bare linked"), Mode: "new", Branch: "topic", Base: "refs/heads/main"})
	if err != nil || tree.Kind != "linked" {
		t.Fatalf("bare linked create: %+v %v", tree, err)
	}
}
func TestCreationContractsAndNoEnvironmentCopy(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	putFile(t, filepath.Join(root, ".env"), "do not copy")
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	gitTest(t, root, "branch", "existing")
	gitTest(t, root, "update-ref", "refs/remotes/origin/local-only", "HEAD")
	branches, err := s.Branches(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if branches.DefaultBase == nil || branches.DefaultParent != root+".worktrees" {
		t.Fatalf("defaults: %+v", branches)
	}
	remote := false
	for _, base := range branches.Bases {
		if base.Name == "refs/remotes/origin/local-only" {
			remote = true
		}
	}
	if !remote {
		t.Fatal("local remote-tracking base missing")
	}
	existingPath := filepath.Join(t.TempDir(), "existing tree")
	existing, err := s.CreateWorktree(context.Background(), r.ID, CreateRequest{Path: existingPath, Mode: "existing", Branch: "existing"})
	if err != nil || existing.Branch == nil || *existing.Branch != "existing" {
		t.Fatalf("existing branch: %+v %v", existing, err)
	}
	newPath := filepath.Join(t.TempDir(), "new\ntree")
	created, err := s.CreateWorktree(context.Background(), r.ID, CreateRequest{Path: newPath, Mode: "new", Branch: "feature/foo", Base: "refs/remotes/origin/local-only"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Path != newPath {
		t.Fatalf("path changed: %q", created.Path)
	}
	if _, err := os.Stat(filepath.Join(newPath, ".env")); !os.IsNotExist(err) {
		t.Fatal("ignored environment file copied")
	}
	occupied := t.TempDir()
	putFile(t, filepath.Join(occupied, "sentinel"), "retain")
	for _, req := range []CreateRequest{
		{Path: filepath.Join(t.TempDir(), "a"), Mode: "new", Branch: "--force", Base: "HEAD"},
		{Path: filepath.Join(t.TempDir(), "b"), Mode: "new", Branch: "valid", Base: "--help"},
		{Path: filepath.Join(t.TempDir(), "c"), Mode: "new", Branch: "feature/foo", Base: "HEAD"},
		{Path: occupied, Mode: "new", Branch: "occupied", Base: "HEAD"},
		{Path: filepath.Join(root, "nested"), Mode: "new", Branch: "nested", Base: "HEAD"},
		{Path: filepath.Join(root, ".git", "nested"), Mode: "new", Branch: "nestedgit", Base: "HEAD"},
		{Path: filepath.Join(t.TempDir(), "d"), Mode: "existing", Branch: "existing"},
		{Path: filepath.Join(t.TempDir(), "e"), Mode: "existing", Branch: "remote-only"},
	} {
		if _, err := s.CreateWorktree(context.Background(), r.ID, req); err == nil {
			t.Fatalf("accepted unsafe creation: %+v", req)
		}
	}
	data, _ := os.ReadFile(filepath.Join(occupied, "sentinel"))
	if string(data) != "retain" {
		t.Fatal("existing directory overwritten")
	}
	if _, err := s.CreateSession(context.Background(), SessionInput{Preset: "custom", Cmd: []string{"/missing/agent"}, Workspace: &Reference{r.ID, created.ID}}); errorKind(err) != ErrInvalid {
		t.Fatalf("agent failure: %v", err)
	}
	item, err := s.CreateSession(context.Background(), SessionInput{Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf RETRY"}, Workspace: &Reference{r.ID, created.ID}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
	if item.Info().Workspace == nil || item.Info().Workspace.WorktreeID != created.ID {
		t.Fatal("retry did not reuse created worktree")
	}
}
func TestFailedGitCreationExposesResidueAndCancellationReapsCommand(t *testing.T) {
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	// Git's real post-checkout hook fails after creating the branch/worktree.
	hook := filepath.Join(root, ".git", "hooks", "post-checkout")
	putFile(t, hook, "#!/bin/sh\nexit 42\n")
	if err := os.Chmod(hook, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "partial")
	if _, err := s.CreateWorktree(context.Background(), r.ID, CreateRequest{Path: path, Mode: "new", Branch: "partial", Base: "HEAD"}); err == nil {
		t.Fatal("hook failure reported success")
	}
	snapshot, err := s.Refresh(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := findTree(snapshot, worktreeID(r.ID, path))
	if err != nil {
		t.Fatal("partial worktree hidden", err)
	}
	gitTest(t, root, "show-ref", "--verify", "refs/heads/partial")
	item, err := s.CreateSession(context.Background(), SessionInput{Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf RESIDUE"}, Workspace: &Reference{r.ID, tree.ID}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
	marker := filepath.Join(t.TempDir(), "hook-ready")
	t.Setenv("WEBMUX_TEST_HOOK_READY", marker)
	putFile(t, hook, "#!/bin/sh\nprintf READY > \"$WEBMUX_TEST_HOOK_READY\"\nexec sleep 30\n")
	if err := os.Chmod(hook, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	cancelledPath := filepath.Join(t.TempDir(), "cancelled")
	go func() {
		_, err := s.CreateWorktree(ctx, r.ID, CreateRequest{Path: cancelledPath, Mode: "new", Branch: "cancelled", Base: "HEAD"})
		finished <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Git hook did not reach cancellation point")
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	cancel()
	select {
	case err := <-finished:
		if errorKind(err) != ErrTimeout || time.Since(start) > 5*time.Second {
			t.Fatalf("Git cancellation not bounded: %v %v", time.Since(start), err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled Git process was not reaped")
	}
	branches, err := s.Branches(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, branch := range branches.Branches {
		if branch.Name == "cancelled" {
			found = true
		}
	}
	if !found {
		t.Fatal("cancelled operation branch residue was rolled back or hidden")
	}
}
func TestGitTrustAndPermissionErrorsNeverBecomeClean(t *testing.T) {
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	// Git itself supplies the dubious-ownership check even under root CI.
	t.Setenv("GIT_TEST_ASSUME_DIFFERENT_OWNER", "1")
	snapshot, err := s.Refresh(context.Background(), r.ID)
	if err != nil || snapshot.Availability != "unavailable" || !strings.Contains(snapshot.Error, "dubious ownership") {
		t.Fatalf("trust failure: %+v %v", snapshot, err)
	}
	if strings.Contains(gitLocalConfig(t, root), "safe.directory") {
		t.Fatal("service modified repository trust")
	}
	item, err := s.CreateSession(context.Background(), SessionInput{Cwd: root, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf TRUST_DIRECT"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
	if item.Info().Workspace != nil {
		t.Fatal("failed Git detection invented association")
	}
}
func gitLocalConfig(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
