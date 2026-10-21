// Package wslclient connects Windows clients to WebMux inside WSL: it runs
// `wsl.exe --exec webmux bridge` and presents the process's stdio as one
// net.Conn for internal/localclient. The dialer itself is platform
// independent so it is tested on Linux with `webmux bridge` directly.
package wslclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
)

// ErrNotRunning is localclient.ErrNotRunning, returned when the bridge exits
// with status 3 before producing any output.
var ErrNotRunning = localclient.ErrNotRunning

// DefaultCloseTimeout is how long Close waits for the bridge to exit after
// its stdin is closed before killing it.
const DefaultCloseTimeout = 2 * time.Second

// BridgeDialer starts one bridge process per connection.
type BridgeDialer struct {
	// Command returns an unstarted bridge process; each call is a new one.
	Command func() *exec.Cmd
	// Stderr receives the bridge's diagnostics (never terminal data).
	Stderr io.Writer
	// CloseTimeout defaults to DefaultCloseTimeout.
	CloseTimeout time.Duration
}

func (d *BridgeDialer) DialContext(ctx context.Context) (net.Conn, error) {
	cmd := d.Command()
	// Own the pipes: exec.Cmd.Wait closes pipes it created, which would
	// race with reads still draining the response.
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		_ = stdinRead.Close()
		_ = stdinWrite.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout = stdinRead, stdoutWrite
	conn := &bridgeConn{cmd: cmd, stdin: stdinWrite, stdout: stdoutRead, exited: make(chan struct{}), timeout: d.CloseTimeout}
	if conn.timeout == 0 {
		conn.timeout = DefaultCloseTimeout
	}
	conn.stderr.limit = 4096
	cmd.Stderr = io.MultiWriter(&conn.stderr, writerOrDiscard(d.Stderr))
	err = cmd.Start()
	_ = stdinRead.Close()
	_ = stdoutWrite.Close()
	if err != nil {
		_ = stdinWrite.Close()
		_ = stdoutRead.Close()
		return nil, fmt.Errorf("start bridge: %w", err)
	}
	go func() {
		conn.waitErr = cmd.Wait()
		close(conn.exited)
	}()
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func writerOrDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// boundedBuffer keeps the first bytes of stderr for error messages.
type boundedBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.limit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}

type bridgeConn struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  boundedBuffer
	exited  chan struct{}
	waitErr error
	timeout time.Duration

	mu        sync.Mutex
	received  bool
	closeOnce sync.Once
}

// Read maps an empty stream from a bridge that exited with status 3 to
// ErrNotRunning, and any other silent failure to the bridge's stderr.
func (c *bridgeConn) Read(p []byte) (int, error) {
	n, err := c.stdout.Read(p)
	if n > 0 {
		c.mu.Lock()
		c.received = true
		c.mu.Unlock()
	}
	if err == nil {
		return n, nil
	}
	c.mu.Lock()
	received := c.received
	c.mu.Unlock()
	if received {
		return n, err
	}
	select {
	case <-c.exited:
	case <-time.After(c.timeout):
		return n, err
	}
	return n, c.exitError(err)
}

func (c *bridgeConn) exitError(fallback error) error {
	var exit *exec.ExitError
	if errors.As(c.waitErr, &exit) {
		message := c.stderr.String()
		if exit.ExitCode() == contract.ExitNotRunning {
			return fmt.Errorf("%w: %s", ErrNotRunning, message)
		}
		return fmt.Errorf("webmux bridge exited with status %d: %s", exit.ExitCode(), message)
	}
	if c.waitErr != nil {
		return fmt.Errorf("webmux bridge: %w", c.waitErr)
	}
	return fallback
}

func (c *bridgeConn) Write(p []byte) (int, error) {
	n, err := c.stdin.Write(p)
	if err != nil {
		select {
		case <-c.exited:
			return n, c.exitError(err)
		default:
		}
	}
	return n, err
}

// CloseWrite ends the request stream; the bridge half-closes the socket.
func (c *bridgeConn) CloseWrite() error { return c.stdin.Close() }

// Close ends stdin, waits for the bridge to exit and kills it after the
// timeout.
func (c *bridgeConn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		select {
		case <-c.exited:
		case <-time.After(c.timeout):
			_ = c.cmd.Process.Kill()
			<-c.exited
		}
		_ = c.stdout.Close()
	})
	return nil
}

type bridgeAddr struct{}

func (bridgeAddr) Network() string { return "bridge" }
func (bridgeAddr) String() string  { return "webmux-bridge" }

func (c *bridgeConn) LocalAddr() net.Addr  { return bridgeAddr{} }
func (c *bridgeConn) RemoteAddr() net.Addr { return bridgeAddr{} }

// Deadlines are enforced through contexts by the HTTP and WebSocket clients;
// pipes to a child process have no deadline support on every platform.
func (c *bridgeConn) SetDeadline(time.Time) error      { return nil }
func (c *bridgeConn) SetReadDeadline(time.Time) error  { return nil }
func (c *bridgeConn) SetWriteDeadline(time.Time) error { return nil }
