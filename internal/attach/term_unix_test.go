//go:build linux || darwin

package attach

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func TestTerminalReadCancellationKeepsDescriptorUsable(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = master.Close(); _ = slave.Close() }()
	if _, err := term.MakeRaw(int(slave.Fd())); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := readTerminal(ctx, int(slave.Fd()), make([]byte, 32)); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("idle PTY read did not cancel")
	}
	if _, err := master.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	buf := make([]byte, 1)
	if n, err := readTerminal(ctx, int(slave.Fd()), buf); err != nil || n != 1 || buf[0] != 'x' {
		t.Fatalf("reuse: %d %q %v", n, buf, err)
	}
}

func TestTerminalReadPipeEOF(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := readTerminal(context.Background(), int(r.Fd()), make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("EOF: %d %v", n, err)
	}
}
