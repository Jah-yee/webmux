package workspace

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/session"
)

func gitTest(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("requires real Git")
	}
}
func initRepo(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo with space\nand newline")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "init", "-b", "main")
	putFile(t, filepath.Join(root, "tracked"), "original\n")
	putFile(t, filepath.Join(root, ".gitignore"), ".env\nbuild/\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-m", "initial")
	return root
}
func putFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func testService(t *testing.T, state, cwd string) (*Service, *session.Manager) {
	t.Helper()
	m := session.NewManager(session.Options{DefaultCwd: cwd})
	s := New(state, m, nil, cwd)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		m.Shutdown(ctx)
		s.Close()
	})
	return s, m
}
func registerTest(t *testing.T, s *Service, path string) Repository {
	t.Helper()
	repository, created, err := s.Register(context.Background(), path)
	if err != nil || !created {
		t.Fatalf("register: created=%v err=%v", created, err)
	}
	if repository.Availability != "available" {
		t.Fatalf("repository unavailable: %+v", repository)
	}
	return repository
}
func linkedTest(t *testing.T, s *Service, r Repository, branch string) Worktree {
	t.Helper()
	tree, err := s.CreateWorktree(context.Background(), r.ID, CreateRequest{Path: filepath.Join(t.TempDir(), "linked "+branch), Mode: "new", Branch: branch, Base: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
func waitDone(t *testing.T, item *session.Session) {
	t.Helper()
	select {
	case <-item.Done():
	case <-time.After(8 * time.Second):
		t.Fatal("PTY did not finish cleanup and drain")
	}
}
func waitOutput(t *testing.T, item *session.Session, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(string(item.Buffer()), needle) {
		if time.Now().After(deadline) {
			t.Fatalf("missing %q in PTY buffer %q", needle, item.Buffer())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRegistryIdentityPersistenceAndRemoval(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	state := t.TempDir()
	s, manager := testService(t, state, root)
	item, err := s.CreateSession(context.Background(), SessionInput{Cwd: root, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf BEFORE; read value; printf AFTER"}})
	if err != nil {
		t.Fatal(err)
	}
	waitOutput(t, item, "BEFORE")
	id := item.Info().ID
	repository := registerTest(t, s, root)
	if item.Info().Workspace == nil || item.Info().Workspace.RepositoryID != repository.ID {
		t.Fatal("registration did not associate running session")
	}
	linked := linkedTest(t, s, repository, "feature")
	child := filepath.Join(linked.Path, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, child, alias} {
		duplicate, created, err := s.Register(context.Background(), path)
		if err != nil || created || duplicate.ID != repository.ID {
			t.Fatalf("duplicate %q: %+v %v %v", path, duplicate, created, err)
		}
	}
	for path, mode := range map[string]os.FileMode{state: 0700, filepath.Join(state, "repositories.json"): 0600} {
		stat, err := os.Stat(path)
		if err != nil || stat.Mode().Perm() != mode {
			t.Fatalf("permissions %q: %v %v", path, stat, err)
		}
	}
	s.Close()
	restarted := New(state, manager, nil, root)
	defer restarted.Close()
	repositories, err := restarted.List(context.Background())
	if err != nil || len(repositories) != 1 || repositories[0].ID != repository.ID || len(repositories[0].Worktrees) != 2 {
		t.Fatalf("restart: %+v %v", repositories, err)
	}
	if err := restarted.Unregister(repository.ID); err != nil {
		t.Fatal(err)
	}
	if item.Info().ID != id || item.Info().Status == session.StatusExited || item.Info().Workspace != nil || !strings.Contains(string(item.Buffer()), "BEFORE") {
		t.Fatal("unregister changed PTY identity, lifecycle, buffer or retained association")
	}
	if _, err := os.Stat(linked.Path); err != nil {
		t.Fatal(err)
	}
	gitTest(t, root, "show-ref", "--verify", "refs/heads/feature")
	restarted.Close()
	third := New(state, manager, nil, root)
	defer third.Close()
	repositories, err = third.List(context.Background())
	if err != nil || len(repositories) != 0 {
		t.Fatalf("removed registration persisted: %+v %v", repositories, err)
	}
}
func TestRegistryFaultsDoNotPublishOrOverwrite(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	state := t.TempDir()
	s, m := testService(t, state, root)
	repository := registerTest(t, s, root)
	original, err := os.ReadFile(filepath.Join(state, "repositories.json"))
	if err != nil {
		t.Fatal(err)
	}
	second := New(state, m, nil, root)
	defer second.Close()
	if second.Capability().RegistryAvailable {
		t.Fatal("second instance acquired same registry")
	}
	if _, _, err := second.Register(context.Background(), root); errorKind(err) != ErrUnavailable {
		t.Fatalf("second writer error: %v", err)
	}
	// Replace the destination with a directory: deterministic write failure even
	// when tests run as root; original bytes remain saved for the corruption check.
	if err := os.Rename(filepath.Join(state, "repositories.json"), filepath.Join(state, "saved.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(state, "repositories.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.Unregister(repository.ID); err == nil {
		t.Fatal("write failure reported success")
	}
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("write failure published memory: %+v %v", list, err)
	}
	if err := os.Remove(filepath.Join(state, "repositories.json")); err != nil {
		t.Fatal(err)
	}
	putFile(t, filepath.Join(state, "repositories.json"), "{broken")
	s.Close()
	broken := New(state, m, nil, root)
	defer broken.Close()
	if broken.Capability().RegistryAvailable {
		t.Fatal("corrupt registry accepted")
	}
	if _, _, err := broken.Register(context.Background(), root); errorKind(err) != ErrUnavailable {
		t.Fatalf("corrupt writer error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(state, "repositories.json"))
	if string(data) != "{broken" {
		t.Fatal("corrupt file overwritten")
	}
	var stored registry
	if err := json.Unmarshal(original, &stored); err != nil || len(stored.Repositories) != 1 {
		t.Fatalf("original registry: %v", err)
	}
	item, err := broken.CreateSession(context.Background(), SessionInput{Cwd: root, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf DEGRADED"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
	if !strings.Contains(string(item.Buffer()), "DEGRADED") {
		t.Fatal("storage fault blocked direct PTY")
	}
}
func TestNoGitAndNonRepositoryRemainDirect(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	s, _ := testService(t, t.TempDir(), root)
	if s.Capability().GitAvailable {
		t.Fatal("unexpected Git capability")
	}
	if _, _, err := s.Register(context.Background(), root); errorKind(err) != ErrUnavailable {
		t.Fatalf("missing Git error: %v", err)
	}
	item, err := s.CreateSession(context.Background(), SessionInput{Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf DIRECT"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
	if item.Info().Cwd != root || item.Info().Workspace != nil || !strings.Contains(string(item.Buffer()), "DIRECT") {
		t.Fatalf("degraded direct session: %+v", item.Info())
	}
}
func TestNonRepositoryDoesNotInitializeAndUnavailableEntryCanBeRemoved(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	ordinary := t.TempDir()
	if _, _, err := s.Register(context.Background(), ordinary); errorKind(err) != ErrInvalid {
		t.Fatalf("ordinary directory error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ordinary, ".git")); !os.IsNotExist(err) {
		t.Fatal("registration initialized repository")
	}
	r := registerTest(t, s, root)
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 || list[0].Availability != "unavailable" || list[0].Error == "" {
		t.Fatalf("missing repository hidden: %+v %v", list, err)
	}
	if err := s.Unregister(r.ID); err != nil {
		t.Fatal(err)
	}
}
