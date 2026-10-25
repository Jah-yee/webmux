package session

// Valid reports whether the message has exactly one payload. Raw frames carry
// only output (0) or snapshot (1); their prefix is required even when empty.
func (m Message) Valid() bool {
	count := 0
	if m.Frame != nil {
		if len(m.Frame) == 0 || m.Frame[0] > 1 {
			return false
		}
		count++
	}
	if m.Info != nil {
		count++
	}
	if m.Exit != nil {
		count++
	}
	if m.Position != nil {
		count++
	}
	if m.Submission != nil {
		count++
	}
	if m.Control != nil {
		count++
	}
	return count == 1
}

// outputMessage copies the reusable PTY buffer once. Every subscriber then
// shares the resulting immutable output frame.
func outputMessage(p []byte) Message {
	frame := make([]byte, len(p)+1)
	copy(frame[1:], p)
	return frameMessage(frame)
}

// frameMessage takes ownership of an already-prefixed, immutable raw frame.
// It neither copies the bytes nor allocates a second frame for replay.
func frameMessage(frame []byte) Message                 { return Message{Frame: frame} }
func infoMessage(info Info) Message                     { return Message{Info: &info} }
func exitMessage(code int) Message                      { return Message{Exit: &code} }
func positionMessage(position Position) Message         { return Message{Position: &position} }
func submissionMessage(status SubmissionStatus) Message { return Message{Submission: &status} }
func controlMessage(state ControlState) Message         { return Message{Control: &state} }
