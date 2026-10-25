package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUnknownStatusAndExecutablePermissionAreNotClean(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	tree := linkedTest(t, s, r, "unknown")
	realGit := s.gitPath
	wrapper := filepath.Join(t.TempDir(), "git")
	putFile(t, wrapper, fmt.Sprintf("#!/bin/sh\nif [ \"$3\" = status ]; then echo 'permission denied reading index' >&2; exit 1; fi\nexec %q \"$@\"\n", realGit))
	if err := os.Chmod(wrapper, 0700); err != nil {
		t.Fatal(err)
	}
	s.gitPath = wrapper
	snapshot, err := s.Refresh(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := findTree(snapshot, tree.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Dirty != nil || !strings.Contains(unknown.Error, "permission denied") {
		t.Fatalf("unknown status shown clean: %+v", unknown)
	}
	preview := previewTest(t, s, r, tree)
	if preview.ForceAllowed {
		t.Fatal("unreadable status allowed force")
	}
	if err := os.Chmod(wrapper, 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.Refresh(context.Background(), r.ID)
	if err != nil || snapshot.Availability != "unavailable" || snapshot.Error == "" {
		t.Fatalf("executable permission failure: %+v %v", snapshot, err)
	}
	item, err := s.CreateSession(context.Background(), SessionInput{Cwd: root, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf PERMISSION_DIRECT"}})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, item)
}
func TestConcurrentSnapshotsCoalesceAndCancelOnlyLastWaiter(t *testing.T) {
	t.Parallel()
	isolateGit(t)
	root := initRepo(t)
	s, _ := testService(t, t.TempDir(), root)
	r := registerTest(t, s, root)
	repository, err := s.repository(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Hold the repository gate before any Git command starts. Two real snapshot
	// requests must share one flight; cancelling one must not cancel the other.
	if err := acquire(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { _, err := s.snapshot(firstCtx, repository); first <- err }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		repository.flightMu.Lock()
		flight := repository.flight
		repository.flightMu.Unlock()
		if flight != nil {
			break
		}
		if time.Now().After(deadline) {
			release(repository)
			t.Fatal("first snapshot not started")
		}
		time.Sleep(time.Millisecond)
	}
	go func() {
		value, err := s.snapshot(context.Background(), repository)
		if err == nil && value.Availability != "available" {
			err = fmt.Errorf("snapshot unavailable: %s", value.Error)
		}
		second <- err
	}()
	for {
		repository.flightMu.Lock()
		flight := repository.flight
		joined := flight != nil && flight.waiters == 2
		repository.flightMu.Unlock()
		if joined {
			break
		}
		if time.Now().After(deadline) {
			cancelFirst()
			release(repository)
			t.Fatal("concurrent snapshot was not coalesced")
		}
		time.Sleep(time.Millisecond)
	}
	cancelFirst()
	if err := <-first; errorKind(err) != ErrTimeout {
		release(repository)
		t.Fatalf("cancelled reader: %v", err)
	}
	release(repository)
	if err := <-second; err != nil {
		t.Fatal("one cancelled reader cancelled shared read", err)
	}
	// The sole waiter cancellation also releases an in-flight Git operation gate.
	if err := acquire(context.Background(), repository); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	final := make(chan error, 1)
	go func() { _, err := s.snapshot(ctx, repository); final <- err }()
	cancel()
	if err := <-final; errorKind(err) != ErrTimeout {
		release(repository)
		t.Fatalf("sole cancelled reader: %v", err)
	}
	release(repository)
	if _, err := s.Refresh(context.Background(), r.ID); err != nil {
		t.Fatal("cancelled read retained repository gate", err)
	}
}
