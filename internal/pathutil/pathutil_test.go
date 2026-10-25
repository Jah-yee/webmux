package pathutil

import (
	"path/filepath"
	"testing"
)

func TestExpand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ in, want string }{
		{"~", home},
		{"~/work/repo", filepath.Join(home, "work", "repo")},
		{"/abs/../path", "/path"},
		{"relative", filepath.Join(cwd, "relative")},
		{"~user", filepath.Join(cwd, "~user")},
	} {
		if got, err := Expand(row.in); err != nil || got != row.want {
			t.Errorf("Expand(%q) = %q, %v; want %q", row.in, got, err, row.want)
		}
	}
}
