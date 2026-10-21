// Package attach connects a native terminal to a WebMux session over
// /ws/s/{id}: raw input and output, control and resize, a detach key and
// the session's exit code. It depends only on localclient and the standard
// library; terminals are adapted in term_unix.go and term_windows.go.
package attach

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/localclient"
)

// DetachKey is Ctrl+], which never reaches the session.
const DetachKey = 0x1d

// ResizeInterval is how often the terminal size is polled.
const ResizeInterval = 250 * time.Millisecond

// Frame types of docs/protocol.md.
const (
	frameOutput   = 0x00
	frameSnapshot = 0x01
	frameExit     = 0x03
	frameControl  = 0x06

	sendInput   = 0x00
	sendResize  = 0x01
	sendControl = 0x05
)

// Close codes of /ws/s/{id}.
const (
	closeNotFound = 4404
	closeSlow     = 4408
	closeExited   = 4410
)

// ErrNotFound means the session does not exist.
var ErrNotFound = errors.New("session not found")

// Terminal is a native terminal in raw mode.
type Terminal interface {
	io.Reader
	io.Writer
	// Size reports columns and rows.
	Size() (cols, rows int, err error)
}

type Options struct {
	// Observe neither sends input nor resizes, and does not take control.
	Observe bool
	// Client identifies this terminal across reconnects; random if empty.
	Client string
	// Label names the client in the browser's control indicator.
	Label string
}

// Result is how an attachment ended without error.
type Result struct {
	// Detached reports the detach key; the session keeps running.
	Detached bool
	// Exited reports that the session's process ended with ExitCode.
	Exited   bool
	ExitCode int
}

// Run attaches until detach, session exit, connection loss or ctx ends.
func Run(ctx context.Context, client *localclient.Client, id string, term Terminal, o Options) (Result, error) {
	if o.Client == "" {
		var raw [12]byte
		_, _ = rand.Read(raw[:])
		o.Client = "native-" + hex.EncodeToString(raw[:])
	}
	query := url.Values{"client": {o.Client}}
	if o.Label != "" {
		query.Set("label", o.Label)
	}
	conn, err := client.DialWebSocket(ctx, "/ws/s/"+url.PathEscape(id)+"?"+query.Encode())
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(64 << 20)
	a := &attachment{conn: conn, term: term, observe: o.Observe}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !o.Observe {
		if err := a.send(ctx, []byte{sendControl}); err != nil {
			return Result{}, err
		}
		a.resize(ctx, true)
		go a.watchSize(ctx)
	}
	detached := make(chan error, 1)
	go func() { detached <- a.pumpInput(ctx) }()
	output := make(chan readResult, 1)
	go func() { output <- a.pumpOutput(ctx) }()
	select {
	case err := <-detached:
		if err != nil {
			return Result{}, err
		}
		_ = conn.Close(websocket.StatusNormalClosure, "detached")
		return Result{Detached: true}, nil
	case r := <-output:
		return r.result, r.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

type readResult struct {
	result Result
	err    error
}

type attachment struct {
	conn    *websocket.Conn
	term    Terminal
	observe bool

	writeMu    sync.Mutex
	sizeMu     sync.Mutex
	cols, rows int
}

func (a *attachment) send(ctx context.Context, frame []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.conn.Write(ctx, websocket.MessageBinary, frame)
}

// resize sends the terminal size when it changed, or always when forced.
func (a *attachment) resize(ctx context.Context, force bool) {
	cols, rows, err := a.term.Size()
	if err != nil || cols <= 0 || rows <= 0 {
		return
	}
	a.sizeMu.Lock()
	changed := force || cols != a.cols || rows != a.rows
	a.cols, a.rows = cols, rows
	a.sizeMu.Unlock()
	if !changed {
		return
	}
	body, _ := json.Marshal(map[string]int{"cols": cols, "rows": rows})
	_ = a.send(ctx, append([]byte{sendResize}, body...))
}

func (a *attachment) watchSize(ctx context.Context) {
	ticker := time.NewTicker(ResizeInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.resize(ctx, false)
		}
	}
}

// pumpInput forwards raw input until the detach key; it returns nil on
// detach. Input never reaches the session while observing.
func (a *attachment) pumpInput(ctx context.Context) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := a.term.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			at := bytes.IndexByte(chunk, DetachKey)
			if at >= 0 {
				chunk = chunk[:at]
			}
			if len(chunk) > 0 && !a.observe {
				if err := a.send(ctx, append([]byte{sendInput}, chunk...)); err != nil {
					return fmt.Errorf("connection lost: %w", err)
				}
			}
			if at >= 0 {
				return nil
			}
		}
		if err != nil {
			// The terminal closed: behave as a detach, the session continues.
			return nil
		}
	}
}

func (a *attachment) pumpOutput(ctx context.Context) readResult {
	var exit *Result
	for {
		_, frame, err := a.conn.Read(ctx)
		if err != nil {
			switch websocket.CloseStatus(err) {
			case closeExited:
				if exit != nil {
					return readResult{result: *exit}
				}
				return readResult{result: Result{Exited: true}}
			case closeNotFound:
				return readResult{err: ErrNotFound}
			case closeSlow:
				return readResult{err: errors.New("connection lost: the terminal could not keep up with the session output")}
			}
			if exit != nil {
				return readResult{result: *exit}
			}
			return readResult{err: fmt.Errorf("connection lost: %w", err)}
		}
		if len(frame) == 0 {
			continue
		}
		switch frame[0] {
		case frameOutput:
			if _, err := a.term.Write(frame[1:]); err != nil {
				return readResult{err: err}
			}
		case frameSnapshot:
			// A full replay: reset the terminal first, as xterm.js does.
			if _, err := a.term.Write(append([]byte("\x1bc"), frame[1:]...)); err != nil {
				return readResult{err: err}
			}
		case frameExit:
			var body struct {
				ExitCode int `json:"exitCode"`
			}
			_ = json.Unmarshal(frame[1:], &body)
			exit = &Result{Exited: true, ExitCode: body.ExitCode}
		case frameControl:
			// Sent when this connection becomes the active one again.
			if !a.observe {
				a.resize(ctx, true)
			}
		}
	}
}
