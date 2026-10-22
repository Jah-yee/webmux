package tray

import "testing"

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name         string
		tray, server BuildInfo
		want         Compat
	}{
		{"release match", BuildInfo{"0.3.0", 1}, BuildInfo{"0.3.0", 1}, CompatMatched},
		{"local builds of different commits", BuildInfo{"0.1.0", 1}, BuildInfo{"0.1.0", 1}, CompatMatched},
		{"WSL older", BuildInfo{"0.3.0", 1}, BuildInfo{"0.2.0", 1}, CompatServerOlder},
		{"numeric, not lexical", BuildInfo{"0.2.0", 1}, BuildInfo{"0.10.0", 1}, CompatServerNewer},
		{"WSL newer", BuildInfo{"0.3.0", 1}, BuildInfo{"0.4.0", 1}, CompatServerNewer},
		{"WSL dev", BuildInfo{"0.3.0", 1}, BuildInfo{"dev", 1}, CompatUnknown},
		{"tray dev", BuildInfo{"dev", 1}, BuildInfo{"0.3.0", 1}, CompatUnknown},
		{"both dev", BuildInfo{"dev", 1}, BuildInfo{"dev", 1}, CompatUnknown},
		{"no version reported", BuildInfo{"0.3.0", 1}, BuildInfo{"", 1}, CompatUnknown},
		{"contract too new", BuildInfo{"0.3.0", 1}, BuildInfo{"0.5.0", 2}, CompatIncompatibleNewer},
		{"contract too new beats an older version", BuildInfo{"0.3.0", 1}, BuildInfo{"0.2.0", 2}, CompatIncompatibleNewer},
		{"contract too old", BuildInfo{"0.3.0", 1}, BuildInfo{"0.3.0", 0}, CompatIncompatibleOlder},
	} {
		if got := Classify(tc.tray, tc.server); got != tc.want {
			t.Errorf("%s: Classify(%v, %v) = %s, want %s", tc.name, tc.tray, tc.server, got, tc.want)
		}
	}
}

func TestParseBuildInfo(t *testing.T) {
	for in, want := range map[string]BuildInfo{
		`{"v":1,"version":"0.3.0","contract":1}`: {"0.3.0", 1},
		`{"v":1,"version":"0.5.0","contract":2}`: {"0.5.0", 2},
		// Versions without --json ignore it and print text: contract v1.
		"webmux 0.2.0\n": {"0.2.0", 1},
		"webmux dev":     {"dev", 1},
		"garbage":        {"", 1},
	} {
		if got := ParseBuildInfo(in); got != want {
			t.Errorf("ParseBuildInfo(%q) = %v, want %v", in, got, want)
		}
	}
}
