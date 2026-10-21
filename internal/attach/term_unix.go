//go:build linux || darwin

package attach

import (
	"os"

	"golang.org/x/term"
)

// Console is the process's terminal in raw mode.
type Console struct {
	state *term.State
}

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

func (c *Console) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (c *Console) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

func (c *Console) Size() (int, int, error) { return term.GetSize(int(os.Stdout.Fd())) }

// Restore returns the terminal to its original mode.
func (c *Console) Restore() error { return term.Restore(int(os.Stdin.Fd()), c.state) }
