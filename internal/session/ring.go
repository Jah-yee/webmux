package session

import (
	"errors"
	"sync"
)

// RingBuffer retains the newest capacity bytes without interpreting terminal output.
// Offsets count every byte ever written, so a reader that knows how far it got
// can ask for exactly the bytes that followed while they are still retained.
type RingBuffer struct {
	mu         sync.RWMutex
	data       []byte
	next, size int
	total      uint64
}

// ErrRingCapacity rejects a ring that cannot retain any output.
var ErrRingCapacity = errors.New("ring capacity must be positive")

func NewRingBuffer(capacity int) (*RingBuffer, error) {
	if capacity < 1 {
		return nil, ErrRingCapacity
	}
	return &RingBuffer{data: make([]byte, capacity)}, nil
}
func (r *RingBuffer) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total += uint64(len(p))
	if len(p) >= len(r.data) {
		p = p[len(p)-len(r.data):]
		copy(r.data, p)
		r.next = 0
		r.size = len(p)
		return
	}
	n := copy(r.data[r.next:], p)
	copy(r.data, p[n:])
	r.next = (r.next + len(p)) % len(r.data)
	r.size = min(len(r.data), r.size+len(p))
}
func (r *RingBuffer) Snapshot() []byte {
	return r.snapshotWithPrefix(nil)
}

// Range reports the absolute offsets of the oldest retained byte and of the
// end of output. A start above zero means older history was overwritten.
func (r *RingBuffer) Range() (start, end uint64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.total - uint64(r.size), r.total
}

// snapshotWithPrefix assembles the protocol header and history in one allocation.
func (r *RingBuffer) snapshotWithPrefix(prefix []byte) []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.copyLocked(prefix, 0)
}

// sinceWithPrefix returns the retained bytes from offset onwards. An expired
// or future offset returns nil without allocating a snapshot that is discarded.
func (r *RingBuffer) sinceWithPrefix(prefix []byte, offset uint64) (out []byte, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	start := r.total - uint64(r.size)
	if offset < start || offset > r.total {
		return nil, false
	}
	return r.copyLocked(prefix, int(offset-start)), true
}

func (r *RingBuffer) copyLocked(prefix []byte, skip int) []byte {
	out := make([]byte, len(prefix)+r.size-skip)
	copy(out, prefix)
	first := (r.next - r.size + skip + len(r.data)) % len(r.data)
	if first+r.size-skip <= len(r.data) {
		copy(out[len(prefix):], r.data[first:first+r.size-skip])
	} else {
		n := copy(out[len(prefix):], r.data[first:])
		copy(out[len(prefix)+n:], r.data[:r.next])
	}
	return out
}
