//go:build linux || darwin

package attach

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Console is the process's terminal in raw mode.
type Console struct {
	state *term.State
}

var _ Terminal = (*Console)(nil)

// OpenConsole switches standard input to raw mode; Restore undoes it.
func OpenConsole() (*Console, error) {
	in, out := int(os.Stdin.Fd()), int(os.Stdout.Fd())
	if !term.IsTerminal(in) || !term.IsTerminal(out) {
		return nil, ErrNotTerminal
	}
	state, err := term.MakeRaw(in)
	if err != nil {
		return nil, err
	}
	return &Console{state: state}, nil
}

func (c *Console) ReadContext(ctx context.Context, p []byte) (int, error) {
	return readTerminal(ctx, int(os.Stdin.Fd()), p)
}

// Poll bounds cancellation latency without closing stdin or changing its flags.
// The raw terminal has a single reader and VMIN=1, so a ready read cannot wait
// for a complete line. Pipes are supported too (for adapter regression tests).
func readTerminal(ctx context.Context, fd int, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	const inputPollMilliseconds = 50
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, err := unix.Poll(fds, inputPollMilliseconds)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, err = unix.Read(fd, p)
		if n == 0 && err == nil {
			err = io.EOF
		}
		return n, err
	}
}

func (c *Console) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

func (c *Console) Size() (int, int, error) { return term.GetSize(int(os.Stdout.Fd())) }

// Restore returns the terminal to its original mode.
func (c *Console) Restore() error { return term.Restore(int(os.Stdin.Fd()), c.state) }
