package session

import "testing"

func TestColorSchemeReportsMode(t *testing.T) {
	var preview Preview
	for _, step := range []struct {
		output string
		want   bool
	}{
		{"\x1b[?2031h", true},
		{"\x1b[?2031l", false},
		// Combined with other modes, and split across reads.
		{"\x1b[?1004;20", false},
		{"31h", true},
		{"\x1b[?1004l\x1b[2J", true},
		{"\x1b[?2031;1049l", false},
		{"\x1b[?2031h", true},
		// RIS restores every mode's default.
		{"\x1bc", false},
	} {
		preview.Feed([]byte(step.output))
		if preview.colorSchemeReports != step.want {
			t.Fatalf("after %q: colorSchemeReports = %v, want %v", step.output, preview.colorSchemeReports, step.want)
		}
	}
	// The observer still applies the other modes in a shared sequence.
	preview.Feed([]byte("\x1b[?1049;2031h"))
	if preview.screen.Buffer() != preview.screen.Buffers().Alt() {
		t.Fatal("alternate screen not entered alongside mode 2031")
	}
}
