//go:build windows

package attach

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procCancelSynchronousIo   = kernel32.NewProc("CancelSynchronousIo")
	procGetConsoleOutputCP    = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
)

const (
	ctrlBreakEvent = 1
	ctrlCloseEvent = 2
	cpUTF8         = 65001
)

// Console is the Windows console in raw VT mode: keyboard input arrives as
// UTF-8 and escape sequences (Ctrl+C as 0x03), output is interpreted as VT
// sequences with the UTF-8 code page.
type Console struct {
	in, out   windows.Handle
	inState   *term.State
	outMode   uint32
	codePage  uintptr
	restoreMu sync.Mutex
	restored  bool
	handler   uintptr
}

var _ Terminal = (*Console)(nil)

// OpenConsole enters raw VT mode and installs a console control handler
// that restores the console on Ctrl+Break and window close.
func OpenConsole() (*Console, error) {
	c := &Console{in: windows.Handle(os.Stdin.Fd()), out: windows.Handle(os.Stdout.Fd())}
	var mode uint32
	if windows.GetConsoleMode(c.in, &mode) != nil || windows.GetConsoleMode(c.out, &c.outMode) != nil {
		return nil, ErrNotTerminal
	}
	state, err := term.MakeRaw(int(c.in))
	if err != nil {
		return nil, err
	}
	c.inState = state
	c.codePage, _, _ = procGetConsoleOutputCP.Call()
	if err := windows.SetConsoleMode(c.out, c.outMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN); err != nil {
		_ = term.Restore(int(c.in), state)
		return nil, err
	}
	_ = windows.SetConsoleOutputCP(cpUTF8)
	c.handler = windows.NewCallback(func(event uint32) uintptr {
		if event == ctrlBreakEvent || event == ctrlCloseEvent {
			_ = c.Restore()
			os.Exit(1)
		}
		return 0
	})
	_, _, _ = procSetConsoleCtrlHandler.Call(c.handler, 1)
	return c, nil
}

// ReadContext pins the synchronous console read to a thread so cancellation
// interrupts only this read, leaving stdin open for the shell or next attach.
func (c *Console) ReadContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var thread windows.Handle
	process := windows.CurrentProcess()
	if err := windows.DuplicateHandle(process, windows.CurrentThread(), process,
		&thread, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(thread) }()
	finished := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-finished:
			return
		case <-ctx.Done():
		}
		// Cancellation can race just before ReadFile enters the kernel.
		// Retry ERROR_NOT_FOUND until the read has actually returned.
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			_, _, _ = procCancelSynchronousIo.Call(uintptr(thread))
			select {
			case <-finished:
				return
			case <-ticker.C:
			}
		}
	}()
	var n uint32
	err := windows.ReadFile(c.in, p, &n, nil)
	close(finished)
	<-joined
	if ctx.Err() != nil {
		return int(n), ctx.Err()
	}
	return int(n), err
}

// Write passes UTF-8 bytes straight to the console, which uses code page
// 65001 while attached.
func (c *Console) Write(p []byte) (int, error) {
	var written uint32
	total := 0
	for total < len(p) {
		if err := windows.WriteFile(c.out, p[total:], &written, nil); err != nil {
			return total, err
		}
		total += int(written)
	}
	return total, nil
}

// Size is the visible window, not the scroll buffer.
func (c *Console) Size() (int, int, error) { return term.GetSize(int(c.out)) }

// Restore returns modes and code page to their original values; it is safe
// to call more than once.
func (c *Console) Restore() error {
	c.restoreMu.Lock()
	defer c.restoreMu.Unlock()
	if c.restored {
		return nil
	}
	c.restored = true
	_, _, _ = procSetConsoleCtrlHandler.Call(c.handler, 0)
	_ = windows.SetConsoleMode(c.out, c.outMode)
	if c.codePage != 0 {
		_ = windows.SetConsoleOutputCP(uint32(c.codePage))
	}
	return term.Restore(int(c.in), c.inState)
}
