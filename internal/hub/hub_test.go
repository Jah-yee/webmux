package hub

import (
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/session"
)

func TestEncodeWritesEachMessageAsItsFrame(t *testing.T) {
	code := 7
	since := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	output := []byte{0, 'h', 'i'}
	for _, row := range []struct {
		name string
		in   session.Message
		want string
		exit bool
	}{
		{"output", session.Message{Frame: output}, "\x00hi", false},
		{"snapshot", session.Message{Frame: []byte{1, 'x'}}, "\x01x", false},
		{"exit", session.Message{Exit: &code}, "\x03{\"exitCode\":7}", true},
		{"position", session.Message{Position: &session.Position{Generation: "g", Mode: "reset", End: 3}}, "\x04{\"generation\":\"g\",\"mode\":\"reset\",\"start\":0,\"end\":3,\"truncated\":false}", false},
		{"submission", session.Message{Submission: &session.SubmissionStatus{ID: "id", State: session.SubmissionWritten}}, "\x05{\"id\":\"id\",\"state\":\"written\"}", false},
		{"control", session.Message{Control: &session.ControlState{Controller: &session.Controller{ClientID: "c", Label: "L", Since: since}, Cols: 80, Rows: 24}}, "\x06{\"controller\":{\"clientId\":\"c\",\"label\":\"L\",\"since\":\"2026-01-02T03:04:05Z\"},\"cols\":80,\"rows\":24,\"colorSchemeReports\":false}", false},
		{"uncontrolled", session.Message{Control: &session.ControlState{Cols: 1, Rows: 2}}, "\x06{\"controller\":null,\"cols\":1,\"rows\":2,\"colorSchemeReports\":false}", false},
	} {
		frame, exit := encode(row.in)
		if string(frame) != row.want || exit != row.exit {
			t.Errorf("%s: %q exit=%v, want %q exit=%v", row.name, frame, exit, row.want, row.exit)
		}
	}
	// Output frames are shared between subscribers: never copied or changed.
	if frame, _ := encode(session.Message{Frame: output}); &frame[0] != &output[0] {
		t.Error("output frame was copied")
	}
}
