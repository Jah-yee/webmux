package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNULRecordsAcrossChunksAndCallbackFailure(t *testing.T) {
	var records []string
	canceled := false
	writer := nulRecords{cancel: func() { canceled = true }, visit: func(record string) error { records = append(records, record); return nil }}
	for _, chunk := range []string{"a\n", "b\x00\xff", "name\x00last\x00"} {
		if _, err := writer.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(records, "|") != "a\nb|\xffname|last" || len(writer.pending) != 0 {
		t.Fatal(records)
	}
	sentinel := errors.New("stop")
	writer.visit = func(string) error { return sentinel }
	if _, err := writer.Write([]byte("stop\x00")); !errors.Is(err, sentinel) || !canceled {
		t.Fatal(err, canceled)
	}
}

func TestStreamedGitHasNoTotalOutputLimitAndCancels(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	t.Setenv("PATH", dir)
	// Shell builtins only: PATH intentionally has no real Git or helpers.
	source := "#!/bin/sh\ni=0\nwhile [ $i -lt 30000 ]; do printf '" + strings.Repeat("x", 100) + "\\0'; i=$((i+1)); done\n"
	if err := os.WriteFile(script, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	count := 0
	err := ReadGitRecords(context.Background(), dir, func(record string) error {
		count++
		if len(record) != 100 {
			return fmt.Errorf("partial record")
		}
		return nil
	}, "ls-files", "-z")
	if err != nil || count != 30000 {
		t.Fatal("stream output", count, err)
	}
	sentinel := errors.New("consumer stopped")
	if err = ReadGitRecords(context.Background(), dir, func(string) error { return sentinel }, "ls-files", "-z"); !errors.Is(err, sentinel) {
		t.Fatal("consumer failure", err)
	}
	if err = os.WriteFile(script, []byte("#!/bin/sh\nprintf 'incomplete'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = ReadGitRecords(context.Background(), dir, func(string) error { return nil }, "ls-files"); err == nil {
		t.Fatal("accepted incomplete NUL output")
	}
	if err = os.WriteFile(script, []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err = ReadGitRecords(ctx, dir, func(string) error { return nil }, "ls-files"); errorKind(err) != ErrTimeout || time.Since(start) > 2*time.Second {
		t.Fatal("cancellation", err)
	}
}
