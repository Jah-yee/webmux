package settings

import (
	"fmt"
	"sync"
	"time"

	"github.com/rainchan/webmux/internal/statefile"
)

type Service struct {
	mu          sync.Mutex
	store       *statefile.Handle[storedFile]
	current     Snapshot
	broken      error
	subscribers map[*Subscription]bool
	commit      func(storedFile) error
}

// A closed queue means reconnect for a fresh snapshot. No publisher blocks.
type Subscription struct {
	Events  <-chan Event
	queue   chan Event
	service *Service
}

func Open(dir string) *Service {
	s := &Service{subscribers: map[*Subscription]bool{}}
	var f storedFile
	var err error
	s.store, f, err = statefile.Open[storedFile](dir, statefile.Settings, validate)
	if err != nil && !statefile.IsKind(err, statefile.Missing) {
		s.broken = err
	} else if err == nil {
		s.current = f.Snapshot
	}
	s.commit = s.store.Commit
	return s
}
func (s *Service) unavailable() error {
	if s.broken == nil {
		s.broken = s.store.Err()
	}
	if s.broken != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, s.broken)
	}
	return nil
}
func (s *Service) Snapshot() (Snapshot, error) {
	if s == nil {
		return Snapshot{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unavailable(); err != nil {
		return Snapshot{}, err
	}
	return clone(s.current), nil
}
func (s *Service) Patch(base int64, changes Changes) (Snapshot, error) {
	if s == nil {
		return Snapshot{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unavailable(); err != nil {
		return Snapshot{}, err
	}
	if base != s.current.Revision {
		return Snapshot{}, ErrConflict
	}
	if base >= 9007199254740991 {
		return Snapshot{}, ErrInvalid
	}
	values, err := apply(s.current.Values, changes)
	if err != nil {
		return Snapshot{}, err
	}
	next := Snapshot{Revision: base + 1, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano), Values: values}
	if err = s.commit(storedFile{V: 1, Snapshot: next}); err != nil {
		s.broken = err
		for sub := range s.subscribers {
			close(sub.queue)
			delete(s.subscribers, sub)
		}
		return Snapshot{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	s.current = next
	for sub := range s.subscribers {
		fields := map[string]bool{}
		for k := range changes.Fields {
			fields[k] = true
		}
		event := Event{Revision: next.Revision, UpdatedAt: next.UpdatedAt, Changes: Changes{Fields: fields, Values: cloneValues(next.Values)}}
		select {
		case sub.queue <- event:
		default:
			close(sub.queue)
			delete(s.subscribers, sub)
		}
	}
	return clone(next), nil
}
func (s *Service) Subscribe() (*Subscription, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.unavailable(); err != nil {
		return nil, err
	}
	q := make(chan Event, 16)
	sub := &Subscription{Events: q, queue: q, service: s}
	snapshot := clone(s.current)
	q <- Event{Snapshot: &snapshot}
	s.subscribers[sub] = true
	return sub, nil
}
func (sub *Subscription) Close() {
	s := sub.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subscribers[sub] {
		delete(s.subscribers, sub)
		close(sub.queue)
	}
}
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store.Close()
	for sub := range s.subscribers {
		close(sub.queue)
		delete(s.subscribers, sub)
	}
}

// Inspect is read-only, including when the document is missing or corrupt.
func Inspect(dir string) (Snapshot, error) {
	f, err := statefile.ReadSnapshot[storedFile](dir, statefile.Settings, validate)
	return f.Snapshot, err
}
