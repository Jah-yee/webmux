package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// macOS places TMPDIR behind the /var -> /private/var symlink, while sessions
// and worktrees report canonical paths. Compare against the canonical form.
// WEBMUX_TEST_EXEC runs the test binary as the webmux executable with the
// JSON-encoded arguments, so subprocess tests need no separate build.
func TestMain(m *testing.M) {
	if encoded := os.Getenv("WEBMUX_TEST_EXEC"); encoded != "" {
		var args []string
		if err := json.Unmarshal([]byte(encoded), &args); err != nil {
			panic(err)
		}
		_ = os.Unsetenv("WEBMUX_TEST_EXEC")
		os.Exit(dispatch(args, os.Stdout, os.Stderr))
	}
	if dir, err := filepath.EvalSymlinks(os.TempDir()); err == nil {
		_ = os.Setenv("TMPDIR", dir)
	}
	os.Exit(m.Run())
}
