package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPublishedRegistryFailureDisablesFurtherWrites(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory read permissions")
	}
	isolateGit(t)
	root := initRepo(t)
	state := t.TempDir()
	s, _ := testService(t, state, root)
	repository := registerTest(t, s, root)
	// Write+execute permits CreateTemp and rename, but opening the directory for
	// fsync fails after the new registry has already been published.
	if err := os.Chmod(state, 0300); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(state, 0700) })
	err := s.Unregister(repository.ID)
	if err == nil {
		t.Fatal("uncertain durability reported success")
	}
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	var persisted registry
	data, err := os.ReadFile(filepath.Join(state, "repositories.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Repositories) != 0 {
		t.Fatal("did not reach post-publication failure")
	}
	if s.Capability().RegistryAvailable {
		t.Fatal("registry still allows writes from stale memory after publication failure")
	}
	if _, _, err := s.Register(context.Background(), root); errorKind(err) != ErrUnavailable {
		t.Fatalf("stale registry accepted another write: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(state, "repositories.json"))
	if err != nil || string(after) != string(data) {
		t.Fatalf("failure state overwrote published data: %v", err)
	}
	item, err := s.CreateSession(context.Background(), SessionInput{Cwd: root, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf DIRECT_AFTER_STORAGE_FAILURE"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
	if !strings.Contains(string(item.Buffer()), "DIRECT_AFTER_STORAGE_FAILURE") {
		t.Fatal("storage failure blocked direct session")
	}
}

func TestCancelledGitCleansDescendantAfterLeaderExit(t *testing.T) {
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	marker := filepath.Join(t.TempDir(), "background-pid")
	t.Setenv("WEBMUX_TEST_BACKGROUND_PID", marker)
	hook := filepath.Join(root, ".git", "hooks", "post-checkout")
	putFile(t, hook, "#!/bin/sh\nsleep 30 &\nprintf '%s' \"$!\" > \"$WEBMUX_TEST_BACKGROUND_PID\"\nexit 0\n")
	if err := os.Chmod(hook, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	target := filepath.Join(t.TempDir(), "tree")
	go func() { _, err := s.git(ctx, root, "worktree", "add", "-b", "background-hook", target); result <- err }()
	deadline := time.Now().Add(5 * time.Second)
	var pid int
	for pid == 0 {
		if data, err := os.ReadFile(marker); err == nil {
			pid, _ = strconv.Atoi(string(data))
		}
		if time.Now().After(deadline) {
			t.Fatal("hook did not start descendant")
		}
		time.Sleep(time.Millisecond)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	group, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	// The group leader must have exited before cancellation; otherwise this only
	// exercises exec.Cmd.Cancel's already-covered running-leader path.
	for !errors.Is(syscall.Kill(group, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatal("Git leader did not exit")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if errorKind(err) != ErrTimeout {
			t.Fatalf("cancellation error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Git pipe wait exceeded deadline")
	}
	deadline = time.Now().Add(time.Second)
	for {
		// ps reports nothing once the process is gone; Z means it already died.
		out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		state := strings.TrimSpace(string(out))
		if state == "" || state[0] == 'Z' {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled Git left its background hook descendant running")
		}
		time.Sleep(time.Millisecond)
	}
}
