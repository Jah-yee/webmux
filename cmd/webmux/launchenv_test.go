package main

import "testing"

func TestWithUserBins(t *testing.T) {
	const home = "/home/u"
	all := "/home/u/.local/bin:/home/u/.local/share/pnpm:/home/u/.npm-global/bin"
	for _, row := range []struct{ path, want string }{
		{"/usr/bin:/bin", all + ":/usr/bin:/bin"},
		{"", all},
		// Directories already present keep their position and precedence.
		{"/usr/bin:/home/u/.local/bin", "/home/u/.local/share/pnpm:/home/u/.npm-global/bin:/usr/bin:/home/u/.local/bin"},
		{all + ":/bin", all + ":/bin"},
	} {
		if got := withUserBins(row.path, home); got != row.want {
			t.Errorf("withUserBins(%q) = %q, want %q", row.path, got, row.want)
		}
	}
}
