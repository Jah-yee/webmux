package session

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"testing/synctest"
	"time"
)

func TestAlreadyCancelledInputIsNotWritten(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	defer func() { _ = writer.Close() }()
	s := &Session{terminal: writer, inputGate: make(chan struct{}, 1), done: make(chan struct{})}
	s.inputGate <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The gate and cancellation are simultaneously ready. Exercise both select
	// outcomes without depending on terminal echo or child-process scheduling.
	for range 100 {
		if err := s.WriteContext(ctx, []byte("x")); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled input returned %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	written, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 0 {
		t.Fatalf("cancelled input delivered %d bytes", len(written))
	}
}

func TestInputBudgetIncludesGateWait(t *testing.T) {
	for _, detached := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			s := &Session{inputGate: make(chan struct{}, 1), done: make(chan struct{})}
			ctx := context.Background()
			if detached {
				parent, cancel := context.WithCancel(ctx)
				cancel()
				ctx = context.WithoutCancel(parent)
			}
			start := time.Now()
			if _, err := s.writeParts(ctx, inputUser, []byte("body"), []byte("\r")); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("gate wait: %v", err)
			}
			if elapsed := time.Since(start); elapsed != 10*time.Second {
				t.Fatalf("gate wait lasted %v", elapsed)
			}
		})
	}
}

func TestDetachedQueuedSubmissionRecordsTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &Session{
			info:      Info{Generation: "generation"},
			inputGate: make(chan struct{}, 1),
			done:      make(chan struct{}),
		}
		sub := &Subscriber{client: "client", Out: make(chan Message, 16), Slow: make(chan struct{})}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		status := s.Submit(context.WithoutCancel(ctx), sub, "submission", "generation", []byte("body"), []byte("\r"))
		if status.State != SubmissionFailed || status.Error != "write" {
			t.Fatalf("queued submission: %+v", status)
		}
		start := time.Now()
		if again := s.Submit(context.Background(), sub, "submission", "generation", []byte("body"), []byte("\r")); again != status || time.Now() != start {
			t.Fatalf("retry did not reuse final outcome: %+v", again)
		}
	})
}
