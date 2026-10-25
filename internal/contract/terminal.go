package contract

import (
	"encoding/json"
	"time"
)

// Terminal WebSocket (/ws/s/{id}) frames are binary: one type byte, then a
// payload. Output, snapshot, input and reply payloads are raw terminal
// bytes; the others are the JSON types below. The numbering matches
// web/src/hooks/terminal-socket.svelte.ts.
const (
	// Server to client.
	FrameOutput     byte = 0 // raw output bytes
	FrameSnapshot   byte = 1 // raw replay snapshot
	FrameState      byte = 2 // SessionInfo
	FrameExit       byte = 3 // ExitFrame; the server then closes with 4410
	FramePosition   byte = 4 // StreamPosition
	FrameSubmission byte = 5 // SubmissionStatus
	FrameControl    byte = 6 // ControlState

	// Client to server.
	FrameInput  byte = 0 // raw user input
	FrameResize byte = 1 // TerminalResize
	FrameReplay byte = 2 // empty: request a full replay
	FrameSubmit byte = 3 // SubmissionHeader line, then body and submit bytes
	FrameQuery  byte = 4 // SubmissionQuery
	FrameClaim  byte = 5 // empty: take control
	FrameReply  byte = 6 // raw automatic terminal replies
)

// Frame prefixes a payload with its type byte.
func Frame(kind byte, payload []byte) []byte {
	frame := make([]byte, len(payload)+1)
	frame[0] = kind
	copy(frame[1:], payload)
	return frame
}

// ExitFrame reports the exit code of the session's program.
type ExitFrame struct {
	ExitCode int `json:"exitCode"`
}

// StreamPosition precedes replayed bytes. "append" continues a terminal at
// Start; "reset" is followed by a full snapshot covering Start to End.
type StreamPosition struct {
	Generation string `json:"generation"`
	Mode       string `json:"mode"`
	Start      uint64 `json:"start"`
	End        uint64 `json:"end"`
	Truncated  bool   `json:"truncated"`
}

// SubmissionStatus reports a submission: received, written, partial,
// failed or missing.
type SubmissionStatus struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// TerminalController is the client whose input and grid drive the PTY.
type TerminalController struct {
	ClientID string    `json:"clientId"`
	Label    string    `json:"label"`
	Since    time.Time `json:"since"`
}

// ControlState says who controls the PTY, the grid observers render, and
// whether the program asked for color scheme change reports.
type ControlState struct {
	Controller         *TerminalController `json:"controller"`
	Cols               uint16              `json:"cols"`
	Rows               uint16              `json:"rows"`
	ColorSchemeReports bool                `json:"colorSchemeReports"`
}

// TerminalResize is the controller's grid.
type TerminalResize struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// SubmissionHeader is the first line of a submit frame. BodyBytes of the
// remaining bytes are the body; the rest is the submit key sequence.
type SubmissionHeader struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
	BodyBytes  int    `json:"bodyBytes"`
}

// SubmissionQuery asks for the recorded status of submissions.
type SubmissionQuery struct {
	IDs []string `json:"ids"`
}

// SessionEvent is one /ws/sessions text message: a snapshot first, then
// created, updated and removed events.
type SessionEvent struct {
	Type     string        `json:"type"`
	Sessions []SessionInfo `json:"sessions,omitempty"`
	Session  *SessionInfo  `json:"session,omitempty"`
	ID       string        `json:"id,omitempty"`
}

// MarshalJSON always writes the sessions of a snapshot, even none.
func (e SessionEvent) MarshalJSON() ([]byte, error) {
	if e.Type == "snapshot" {
		return json.Marshal(struct {
			Type     string        `json:"type"`
			Sessions []SessionInfo `json:"sessions"`
		}{e.Type, e.Sessions})
	}
	type plain SessionEvent
	return json.Marshal(plain(e))
}
