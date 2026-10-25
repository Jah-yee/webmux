package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceGitBoundsBothOutputStreams(t *testing.T) {
	for _, stream := range []string{"stdout", "stderr"} {
		t.Run(stream, func(t *testing.T) {
			dir := t.TempDir()
			redirect := ""
			if stream == "stderr" {
				redirect = " >&2"
			}
			// Bound the fixture too: after overflowing the output budget, wait
			// so the test requires cancellation rather than silent truncation.
			count := "576"
			if stream == "stderr" {
				count = "2"
			}
			script := "#!/bin/sh\ni=0\nwhile [ \"$i\" -lt " + count + " ]; do printf '%65536s' x" + redirect + "; i=$((i+1)); done\nexec /bin/sleep 10\n"
			path := filepath.Join(dir, "git")
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			s := &Service{gitPath: path}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			started := time.Now()
			out, err := s.git(ctx, dir, "status")
			if errorKind(err) != ErrTooLarge || len(out) != 0 {
				t.Fatalf("unbounded %s: bytes=%d err=%v", stream, len(out), err)
			}
			if ctx.Err() != nil || time.Since(started) > 3*time.Second {
				t.Fatalf("output overflow waited for request timeout: %v", err)
			}
			if len(err.Error()) > 1024 || !strings.Contains(err.Error(), "输出") {
				t.Fatalf("missing bounded, contextual error: %d bytes", len(err.Error()))
			}
		})
	}
}
