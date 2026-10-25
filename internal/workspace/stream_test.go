package workspace

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func streamTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func TestStreamGitWritesBlobAndDisablesFilters(t *testing.T) {
	dir := t.TempDir()
	streamTestGit(t, dir, "init", "-q")
	data := bytes.Repeat([]byte("\x00\x01binary"), 50000)
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	oid := streamTestGit(t, dir, "hash-object", "-w", "blob.bin")
	var out bytes.Buffer
	if err := StreamGit(context.Background(), dir, &out, "cat-file", "blob", oid); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatalf("streamed %d bytes, want %d", out.Len(), len(data))
	}
	marker := filepath.Join(t.TempDir(), "marker")
	os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("*.txt filter=evil\n"), 0600)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0600)
	streamTestGit(t, dir, "add", ".gitattributes")
	streamTestGit(t, dir, "-c", "filter.evil.clean=cat", "add", "a.txt")
	streamTestGit(t, dir, "config", "filter.evil.clean", "touch "+marker+"; cat")
	streamTestGit(t, dir, "config", "filter.evil.required", "true")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0600)
	out.Reset()
	if err := StreamGit(context.Background(), dir, &out, "diff", "--no-ext-diff", "--no-textconv"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("clean filter ran during streamed diff", err)
	}
}

type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestStreamGitWriterErrorAndCancellationKillProcess(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile :; do printf 'large-output-large-output'; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	sentinel := errors.New("stop")
	start := time.Now()
	err := StreamGit(context.Background(), dir, failingWriter{sentinel}, "cat-file", "blob", "x")
	if !errors.Is(err, sentinel) || time.Since(start) > 3*time.Second {
		t.Fatalf("writer error: %v after %v", err, time.Since(start))
	}
	if err = os.WriteFile(script, []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start = time.Now()
	err = StreamGit(ctx, dir, &bytes.Buffer{}, "cat-file", "blob", "x")
	if errorKind(err) != ErrTimeout || time.Since(start) > 3*time.Second {
		t.Fatalf("cancellation: %v", err)
	}
}
