package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/wire"
)

var (
	clientPattern     = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	generationPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// AttachOptions reads the optional client identity and resume point from the
// terminal WebSocket query. Malformed values are dropped, never rejected:
// an anonymous client simply observes or replays in full.
func AttachOptions(q url.Values) session.AttachOptions {
	var o session.AttachOptions
	if client := q.Get("client"); clientPattern.MatchString(client) {
		o.Client = client
	}
	label := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return -1
		}
		return r
	}, q.Get("label"))
	if runes := []rune(strings.TrimSpace(label)); len(runes) > 40 {
		o.Label = string(runes[:40])
	} else {
		o.Label = string(runes)
	}
	generation := q.Get("generation")
	offset, err := strconv.ParseUint(q.Get("offset"), 10, 64)
	if err == nil && generationPattern.MatchString(generation) {
		o.Resume = &session.ResumePoint{Generation: generation, Offset: offset}
	}
	return o
}

type submission struct {
	contract.SubmissionHeader
	body   []byte
	submit []byte
}

// parseSubmission splits a 0x03 payload: a JSON header line, then the body
// bytes followed by the submit key bytes. Terminal bytes never enter JSON.
func parseSubmission(p []byte) (*submission, bool) {
	header, rest, found := bytes.Cut(p, []byte{'\n'})
	var item submission
	if !found || json.Unmarshal(header, &item.SubmissionHeader) != nil || !clientPattern.MatchString(item.ID) {
		return nil, false
	}
	if item.BodyBytes < 0 || item.BodyBytes > len(rest) || len(rest) == 0 {
		return &item, false
	}
	item.body, item.submit = rest[:item.BodyBytes], rest[item.BodyBytes:]
	return &item, true
}

type input struct {
	data   []byte
	reply  bool
	submit *submission
}

// jsonFrame encodes a JSON control frame. Its value is a contract type,
// which archtest rule R8 checks at every call.
func jsonFrame(kind byte, v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return contract.Frame(kind, data), nil
}

var errInvalidMessage = errors.New("invalid session message")

// encode rejects malformed payloads instead of dropping them or choosing one
// arbitrarily. Raw bytes remain shared; only typed control payloads are encoded.
func encode(m session.Message) (frame []byte, exit bool, err error) {
	if !m.Valid() {
		return nil, false, errInvalidMessage
	}
	switch {
	case m.Frame != nil:
		return m.Frame, false, nil
	case m.Info != nil:
		frame, err = jsonFrame(contract.FrameState, wire.Session(*m.Info))
	case m.Exit != nil:
		frame, err = jsonFrame(contract.FrameExit, contract.ExitFrame{ExitCode: *m.Exit})
		exit = true
	case m.Position != nil:
		frame, err = jsonFrame(contract.FramePosition, wire.StreamPosition(*m.Position))
	case m.Submission != nil:
		frame, err = jsonFrame(contract.FrameSubmission, wire.SubmissionStatus(*m.Submission))
	case m.Control != nil:
		frame, err = jsonFrame(contract.FrameControl, wire.ControlState(*m.Control))
	}
	return frame, exit, err
}

func write(ctx context.Context, c *websocket.Conn, kind websocket.MessageType, p []byte) error {
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.Write(deadline, kind, p)
}

// Attach owns the only data writer. Its reader is cancelled and joined on all exits.
func Attach(ctx context.Context, s *session.Session, c *websocket.Conn, o session.AttachOptions) {
	ctx, cancel := context.WithCancel(ctx)
	sub := s.AttachClient(o)
	defer s.Detach(sub)
	c.SetReadLimit(1024 * 1024)
	readerDone := make(chan struct{})
	inputDone := make(chan struct{})
	inputs := make(chan input, 16)
	go func() {
		defer close(inputDone)
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case item := <-inputs:
				if item.submit != nil {
					// A received submission finishes even if this connection
					// drops: its status stays queryable, and the PTY write
					// remains bounded by the write deadline and session exit.
					submit := item.submit
					s.Submit(context.WithoutCancel(ctx), sub, submit.ID, submit.Generation, submit.body, submit.submit)
					if ctx.Err() != nil {
						return
					}
				} else if s.WriteInput(ctx, item.data, item.reply) != nil {
					return
				}
			}
		}
	}()
	enqueue := func(item input) bool {
		select {
		case inputs <- item:
			return true
		case <-ctx.Done():
		default:
			cancel()
		}
		return false
	}
	go func() {
		defer close(readerDone)
		defer cancel()
		for {
			kind, p, err := c.Read(ctx)
			if err != nil {
				return
			}
			if kind != websocket.MessageBinary || len(p) == 0 {
				continue
			}
			switch p[0] {
			case contract.FrameInput:
				// User input takes control.
				s.Claim(sub)
				if !enqueue(input{data: p[1:]}) {
					return
				}
			case contract.FrameReply:
				// Every attached xterm answers the same device queries; only
				// the responsible connection's answers reach the PTY, and
				// answering never takes control.
				if s.Responds(sub) && !enqueue(input{data: p[1:], reply: true}) {
					return
				}
			case contract.FrameResize:
				var size contract.TerminalResize
				// Observers render the controller's grid and never resize it.
				if json.Unmarshal(p[1:], &size) == nil {
					_ = s.ResizeFrom(sub, size.Cols, size.Rows)
				}
			case contract.FrameReplay:
				s.Replay(sub)
			case contract.FrameSubmit:
				item, ok := parseSubmission(p[1:])
				if !ok {
					if item != nil {
						s.SendSubmissionStatus(sub, session.SubmissionStatus{ID: item.ID, State: session.SubmissionFailed, Error: "invalid"})
					}
					continue
				}
				s.Claim(sub)
				if !enqueue(input{submit: item}) {
					return
				}
			case contract.FrameQuery:
				var query contract.SubmissionQuery
				if json.Unmarshal(p[1:], &query) != nil || len(query.IDs) > 64 {
					continue
				}
				ids := query.IDs[:0]
				for _, id := range query.IDs {
					if clientPattern.MatchString(id) {
						ids = append(ids, id)
					}
				}
				s.QuerySubmissions(sub, ids)
			case contract.FrameClaim:
				s.Claim(sub)
			}
		}
	}()
	defer func() {
		cancel()
		_ = c.CloseNow()
		<-readerDone
		<-inputDone
	}()
	writeMessages(ctx, c, sub)
}

// writeMessages is the sole terminal data writer, including aggregation.
func writeMessages(ctx context.Context, c *websocket.Conn, sub *session.Subscriber) {
	var pending session.Message
	var hasPending bool
	var combinedBuffer []byte
	for {
		var m session.Message
		if hasPending {
			m = pending
			pending = session.Message{}
			hasPending = false
		} else {
			select {
			case <-ctx.Done():
				return
			case <-sub.Slow:
				_ = c.Close(4408, "slow client")
				return
			case m = <-sub.Out:
			}
		}
		frame, exit, err := encode(m)
		if err != nil {
			_ = c.Close(websocket.StatusInternalError, "invalid session message")
			return
		}
		if m.Frame != nil && frame[0] == contract.FrameOutput {
			if combinedBuffer == nil {
				combinedBuffer = make([]byte, 0, 65537)
			}
			combined := append(combinedBuffer[:0], frame...)
			timer := time.NewTimer(50 * time.Millisecond)
		aggregate:
			for len(combined) < 65537 {
				select {
				case next := <-sub.Out:
					if !next.Valid() || next.Frame == nil || next.Frame[0] != contract.FrameOutput || len(combined)+len(next.Frame)-1 > 65537 {
						pending, hasPending = next, true
						break aggregate
					}
					combined = append(combined, next.Frame[1:]...)
				case <-timer.C:
					break aggregate
				case <-ctx.Done():
					timer.Stop()
					return
				case <-sub.Slow:
					timer.Stop()
					_ = c.Close(4408, "slow client")
					return
				}
			}
			timer.Stop()
			frame = combined
		}
		if write(ctx, c, websocket.MessageBinary, frame) != nil {
			_ = c.Close(4408, "write timeout")
			return
		}
		if exit {
			_ = c.Close(4410, "session exited")
			return
		}
	}
}
func List(ctx context.Context, m *session.Manager, c *websocket.Conn) {
	ctx, cancel := context.WithCancel(ctx)
	sub := m.Subscribe()
	defer m.Unsubscribe(sub)
	reader := c.CloseRead(ctx)
	defer func() {
		cancel()
		_ = c.CloseNow()
	}()
	for {
		select {
		case <-reader.Done():
			return
		case <-sub.Slow:
			_ = c.Close(4408, "slow client")
			return
		case event := <-sub.Out:
			data, err := json.Marshal(wire.SessionEvent(event))
			if err != nil || write(ctx, c, websocket.MessageText, data) != nil {
				_ = c.Close(4408, "write timeout")
				return
			}
		}
	}
}
