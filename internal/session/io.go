package session

import (
	"context"
	"os"
	"syscall"
	"time"
)

// A nonblocking descriptor handed to NewFile participates in Go's poller.
// Duplicating transfers ownership without retaining two File finalizers on one fd.
func pollablePTY(original *os.File) (*os.File, error) {
	syscall.ForkLock.RLock()
	fd, err := syscall.Dup(int(original.Fd()))
	if err == nil {
		syscall.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	if err = syscall.SetNonblock(fd, true); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	name := original.Name()
	if err = original.Close(); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	if err = f.SetReadDeadline(time.Time{}); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// WriteContext serializes input, but a disconnected client can abandon both
// the serialization wait and a full kernel input queue without ending the PTY.
func (s *Session) WriteContext(ctx context.Context, p []byte) error {
	_, err := s.writeParts(ctx, inputUnknown, p)
	return err
}

// inputOrigin says who produced input. Only the browser knows whether its
// xterm emitted bytes for a keypress or as an automatic reply to a query:
// the same bytes (e.g. "ESC [ 1 ; 2 R") can be either.
type inputOrigin int

const (
	// inputUnknown classifies by byte pattern; attention ignores replies.
	inputUnknown inputOrigin = iota
	inputUser
	inputReply
)

// WriteInput writes bytes whose origin the client reported.
func (s *Session) WriteInput(ctx context.Context, p []byte, reply bool) error {
	origin := inputUser
	if reply {
		origin = inputReply
	}
	_, err := s.writeParts(ctx, origin, p)
	return err
}

// writeParts holds the input gate across every part, so no other input lands
// between them. One deadline covers both the gate wait and every part, even
// when a submission outlives its connection. It returns the bytes delivered.
func (s *Session) writeParts(ctx context.Context, origin inputOrigin, parts ...[]byte) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-s.done:
		return 0, os.ErrClosed
	case <-s.inputGate:
	}
	defer func() { s.inputGate <- struct{}{} }()
	// Replies queued before this input's sender could have seen their
	// queries go first, so a reply always precedes later client input.
	if err := s.flushRepliesGated(ctx); err != nil {
		return 0, err
	}
	written := 0
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		n, err := s.writeGated(ctx, origin, p)
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

// writeGated performs one PTY write while the caller holds the input gate.
func (s *Session) writeGated(ctx context.Context, origin inputOrigin, p []byte) (int, error) {
	// Cancellation and an available gate can both be selected. Never deliver
	// input after cancellation merely because the gate happened to win.
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	deadline, _ := ctx.Deadline() // writeParts owns the entire input budget.
	if err := s.terminal.SetWriteDeadline(deadline); err != nil {
		return 0, err
	}
	cancelled := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = s.terminal.SetWriteDeadline(time.Now())
		close(cancelled)
	})
	n, err := s.terminal.Write(p)
	if !stop() {
		<-cancelled
	}
	_ = s.terminal.SetWriteDeadline(time.Time{})
	if ctx.Err() != nil {
		return n, ctx.Err()
	}
	if err == nil && origin != inputReply {
		s.mu.Lock()
		if origin == inputUser {
			s.recordInputLocked(time.Now())
		} else {
			s.userInputLocked(p, time.Now())
		}
		s.mu.Unlock()
	}
	return n, err
}

// maxPendingReplies bounds session-written query replies awaiting the gate;
// queries beyond it get no reply.
const maxPendingReplies = 4

// programStatusReply answers the OSC 7501 capability query.
const programStatusReply = "\x1b]7501;?:states=idle,working,done,blocked,error:kinds=permission,question,auth\x1b\\"

// queueProbeReplyLocked queues the OSC 7501 reply. It runs in the read path
// before the query is published, so every client input sent in response to
// that output is written after the reply.
func (s *Session) queueProbeReplyLocked() {
	if len(s.replies) >= maxPendingReplies {
		return
	}
	s.replies = append(s.replies, []byte(programStatusReply))
	select {
	case s.replyWake <- struct{}{}:
	default:
	}
}

// flushRepliesGated writes queued replies while the caller holds the input
// gate. A reply none of whose bytes was written goes back to the queue for
// the reply loop, so a cancelled client writer cannot discard it; after a
// partial write the rest is dropped, since a query is never answered twice.
func (s *Session) flushRepliesGated(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	replies := s.replies
	s.replies = nil
	s.mu.Unlock()
	for i, reply := range replies {
		n, err := s.writeGated(ctx, inputReply, reply)
		if err == nil {
			continue
		}
		if n == 0 {
			s.mu.Lock()
			s.replies = append(replies[i:len(replies):len(replies)], s.replies...)
			// Replies queued meanwhile must not push the queue past its bound.
			if len(s.replies) > maxPendingReplies {
				s.replies = s.replies[:maxPendingReplies]
			}
			s.mu.Unlock()
			select {
			case s.replyWake <- struct{}{}:
			default:
			}
		}
		return err
	}
	return nil
}

// replyLoop writes queued replies when no client input arrives to carry
// them, for example while no browser is attached. It ends with the process.
func (s *Session) replyLoop() {
	defer close(s.repliesDone)
	for {
		select {
		case <-s.lifetime.Done():
			return
		case <-s.replyWake:
			_, _ = s.writeParts(s.lifetime, inputReply)
		}
	}
}
