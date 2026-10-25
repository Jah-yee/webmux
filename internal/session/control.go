package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var ErrNotController = errors.New("connection does not control the terminal")

// Controller is the browser client whose input and grid drive the PTY. It is
// identified by a client ID that survives that client's reconnects.
type Controller struct {
	ClientID string
	Label    string
	Since    time.Time
}

// ControlState is the 0x06 frame: who controls the PTY, the grid that
// observers render without resizing it, and whether the program asked for
// color scheme change reports (DEC mode 2031).
type ControlState struct {
	Controller         *Controller
	Cols               uint16
	Rows               uint16
	ColorSchemeReports bool
}

func randomClientID() string {
	var raw [12]byte
	_, _ = rand.Read(raw[:])
	return "anon-" + hex.EncodeToString(raw[:])
}

func (s *Session) controlLocked() Message {
	state := ControlState{Cols: s.info.Cols, Rows: s.info.Rows, ColorSchemeReports: s.preview.colorSchemeReports}
	if s.control != nil {
		controller := *s.control
		state.Controller = &controller
	}
	return controlMessage(state)
}

func (s *Session) setControlLocked(sub *Subscriber) {
	s.control = &Controller{ClientID: sub.client, Label: sub.label, Since: time.Now()}
	s.controlSub = sub
	s.broadcastLocked(s.controlLocked())
}

// Claim makes the subscriber the controlling connection and reports whether
// control moved to another client.
func (s *Session) Claim(sub *Subscriber) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, attached := s.subs[sub]; !attached || s.controlSub == sub {
		return false
	}
	if s.control != nil && s.control.ClientID == sub.client {
		// Its earlier resizes were refused; the control frame makes it send
		// its grid again.
		s.controlSub = sub
		s.sendLocked(sub, s.controlLocked())
		return false
	}
	s.setControlLocked(sub)
	return true
}

// Responds reports whether automatic terminal replies from this subscriber
// reach the PTY: the controlling connection's, or without one the oldest
// connection's. Every attached xterm answers the same queries; only one
// answer is written.
func (s *Session) Responds(sub *Subscriber) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, attached := s.subs[sub]; !attached {
		return false
	}
	if s.control != nil {
		return s.controlSub == sub
	}
	for other := range s.subs {
		if other.seq < sub.seq {
			return false
		}
	}
	return true
}

type SubmissionState string

const (
	SubmissionReceived SubmissionState = "received"
	SubmissionWritten  SubmissionState = "written"
	// Partial means some, but not all, submission bytes reached the PTY.
	SubmissionPartial SubmissionState = "partial"
	SubmissionFailed  SubmissionState = "failed"
	// Missing answers a query for an ID this run has no record of: it never
	// arrived, or its record expired.
	SubmissionMissing SubmissionState = "missing"
)

// SubmissionStatus is the 0x05 frame.
type SubmissionStatus struct {
	ID    string
	State SubmissionState
	Error string
}

const (
	submissionRetention = 30 * time.Minute
	submissionLimit     = 256
)

type submissionRecord struct {
	status SubmissionStatus
	client string
	// Clients that asked about the submission while it was still being
	// written, e.g. a restored draft in a new tab; they get the outcome too.
	watchers map[string]struct{}
	at       time.Time
}

// submissionLog is a bounded, mu-owned record of recent submissions so that a
// client whose acknowledgement was lost can ask what happened, and so that
// resending the same ID never writes it twice.
type submissionLog struct {
	records map[string]*submissionRecord
	order   []string
}

func (l *submissionLog) get(id string, now time.Time) *submissionRecord {
	l.expire(now)
	return l.records[id]
}

func (l *submissionLog) add(id, client string, now time.Time) *submissionRecord {
	l.expire(now)
	if l.records == nil {
		l.records = make(map[string]*submissionRecord)
	}
	for len(l.order) >= submissionLimit {
		delete(l.records, l.order[0])
		l.order = l.order[1:]
	}
	record := &submissionRecord{status: SubmissionStatus{ID: id, State: SubmissionReceived}, client: client, at: now}
	l.records[id] = record
	l.order = append(l.order, id)
	return record
}

func (l *submissionLog) expire(now time.Time) {
	for len(l.order) > 0 {
		record := l.records[l.order[0]]
		if record != nil && now.Sub(record.at) < submissionRetention {
			return
		}
		delete(l.records, l.order[0])
		l.order = l.order[1:]
	}
}

// forgetWatcher drops a client that no longer has any connection: a
// reconnect queries again and re-registers.
func (l *submissionLog) forgetWatcher(client string) {
	for _, record := range l.records {
		delete(record.watchers, client)
	}
}

// Submit writes a body and its submit key as one input critical section, so
// input from other connections cannot interleave between them. A repeated ID
// is never written again; the caller receives the recorded status instead.
// Status frames go to every connection of the submitting client, including
// one that reconnected while the write was in progress.
func (s *Session) Submit(ctx context.Context, sub *Subscriber, id, generation string, body, submit []byte) SubmissionStatus {
	now := time.Now()
	s.mu.Lock()
	if record := s.submissions.get(id, now); record != nil {
		status := record.status
		s.sendLocked(sub, submissionMessage(status))
		s.mu.Unlock()
		return status
	}
	if generation != s.info.Generation {
		status := SubmissionStatus{ID: id, State: SubmissionFailed, Error: "generation"}
		s.sendLocked(sub, submissionMessage(status))
		s.mu.Unlock()
		return status
	}
	record := s.submissions.add(id, sub.client, now)
	s.notifyClientLocked(record)
	s.mu.Unlock()

	written, err := s.writeParts(ctx, inputUser, body, submit)
	status := SubmissionStatus{ID: id, State: SubmissionWritten}
	if err != nil {
		status.State, status.Error = SubmissionFailed, "write"
		if written > 0 {
			status.State = SubmissionPartial
		}
	}
	s.mu.Lock()
	record.status = status
	s.notifyClientLocked(record)
	// The outcome is final; later queries read it from the record.
	record.watchers = nil
	s.mu.Unlock()
	return status
}

func (s *Session) notifyClientLocked(record *submissionRecord) {
	frame := submissionMessage(record.status)
	for other := range s.subs {
		if _, watching := record.watchers[other.client]; watching || other.client == record.client {
			s.sendLocked(other, frame)
		}
	}
}

// SendSubmissionStatus answers one subscriber without recording anything.
func (s *Session) SendSubmissionStatus(sub *Subscriber, status SubmissionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendLocked(sub, submissionMessage(status))
}

// QuerySubmissions answers the subscriber with the recorded status of each ID.
func (s *Session) QuerySubmissions(sub *Subscriber, ids []string) {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		status := SubmissionStatus{ID: id, State: SubmissionMissing}
		if record := s.submissions.get(id, now); record != nil {
			status = record.status
			if status.State == SubmissionReceived && sub.client != record.client {
				if record.watchers == nil {
					record.watchers = make(map[string]struct{})
				}
				record.watchers[sub.client] = struct{}{}
			}
		}
		s.sendLocked(sub, submissionMessage(status))
	}
}
