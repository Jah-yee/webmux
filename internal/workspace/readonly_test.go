package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadGitBoundsAndCancellation(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do printf 'large-output-large-output'; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := ReadGit(ctx, dir, 1024, "status")
	if errorKind(err) != ErrTooLarge {
		t.Fatalf("output cap: %v", err)
	}
	if err = os.WriteFile(script, []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel2()
	start := time.Now()
	_, err = ReadGit(ctx2, dir, 1024, "status")
	if errorKind(err) != ErrTimeout || time.Since(start) > 2*time.Second {
		t.Fatalf("cancellation %v", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), "git") {
		t.Fatal("runner wrote files", err)
	}
}
