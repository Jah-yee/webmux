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

// inputModeReset turns off what a session's program may have enabled in the
// native terminal: win32-input-mode, modifyOtherKeys, kitty keyboard flags,
// mouse and focus reporting and bracketed paste; and shows the cursor.
const inputModeReset = "\x1b[?9001l\x1b[>4;0m\x1b[<99u\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1004l\x1b[?2004l\x1b[?25h"

// DetachKey is Ctrl+], which never reaches the session.
const DetachKey = 0x1d

// TakeControlKey is Enter: while another client controls the session it
// reclaims control and is not forwarded.
const TakeControlKey = '\r'

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
	sendResync  = 0x02
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
	// Runs after the connection closes: the session's program may have left
	// the native terminal reporting keys, mouse, focus or pastes in its own
	// encodings, which the shell behind it cannot read.
	defer func() { _, _ = term.Write([]byte(inputModeReset)) }()
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(64 << 20)
	a := &attachment{conn: conn, term: term, observe: o.Observe, client: o.Client}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !o.Observe {
		// Until the server confirms this claim, control frames from the
		// replay describe the previous controller.
		a.controlled, a.claiming = true, true
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
	client  string

	writeMu    sync.Mutex
	sizeMu     sync.Mutex
	cols, rows int

	// controlMu guards whether this terminal controls the session, an
	// unconfirmed claim, and the controlling client's label.
	controlMu  sync.Mutex
	controlled bool
	claiming   bool
	controller string
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
// detach. Input never reaches the session while observing. While another
// client controls the session, input is dropped: the terminal's automatic
// replies (focus, device and colour reports) would otherwise claim control.
// Only TakeControlKey reclaims it.
func (a *attachment) pumpInput(ctx context.Context) error {
	buf := make([]byte, 32*1024)
	for {
		n, err := a.term.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			at := detachIndex(chunk)
			if at >= 0 {
				chunk = chunk[:at]
			}
			if len(chunk) > 0 && !a.observe {
				if !a.isControlling() {
					if takeControlIndex(chunk) >= 0 {
						if err := a.takeControl(ctx); err != nil {
							return fmt.Errorf("connection lost: %w", err)
						}
					}
				} else if err := a.send(ctx, append([]byte{sendInput}, chunk...)); err != nil {
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

func (a *attachment) isControlling() bool {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	return a.controlled
}

// takeControl claims the session, applies this terminal's size and asks for
// a replay, which repaints over the banner and confirms the claim.
func (a *attachment) takeControl(ctx context.Context) error {
	a.controlMu.Lock()
	a.controlled, a.claiming = true, true
	a.controlMu.Unlock()
	if err := a.send(ctx, []byte{sendControl}); err != nil {
		return err
	}
	a.resize(ctx, true)
	return a.send(ctx, []byte{sendResync})
}

// applyControl records a control frame and reports whether this terminal
// controls the session afterwards.
func (a *attachment) applyControl(body []byte) bool {
	var state struct {
		Controller *struct {
			ClientID string `json:"clientId"`
			Label    string `json:"label"`
		} `json:"controller"`
	}
	if json.Unmarshal(body, &state) != nil {
		return a.isControlling()
	}
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	mine := state.Controller != nil && state.Controller.ClientID == a.client
	switch {
	case mine:
		a.controlled, a.claiming = true, false
	case a.claiming:
		// A frame queued before the claim reached the server.
	default:
		a.controlled = false
		a.controller = ""
		if state.Controller != nil {
			a.controller = state.Controller.Label
		}
	}
	return a.controlled
}

// banner is drawn over the top-left corner while another client controls
// the session; cursor and attributes are saved and restored around it.
func (a *attachment) banner() []byte {
	a.controlMu.Lock()
	who := a.controller
	a.controlMu.Unlock()
	text := " Observing · Enter to take control · Ctrl+] to detach "
	if who != "" {
		text = " Controlled by " + who + " · Enter to take control · Ctrl+] to detach "
	}
	a.sizeMu.Lock()
	cols := a.cols
	a.sizeMu.Unlock()
	if runes := []rune(text); cols > 0 && len(runes) > cols {
		text = string(runes[:cols])
	}
	return []byte("\x1b7\x1b[1;1H\x1b[0;7m" + text + "\x1b[0m\x1b8")
}

// key describes one key in each encoding a native terminal may use.
type key struct {
	raw  byte   // legacy byte
	code string // kitty and modifyOtherKeys key code
	mods int    // exact modifier bits (Ctrl is 4)
	vk   int    // win32-input-mode virtual-key code
	char int    // win32-input-mode character
}

var (
	detachKey      = key{raw: DetachKey, code: "93", mods: 4, vk: 0xdd, char: DetachKey}
	takeControlKey = key{raw: TakeControlKey, code: "13", vk: 0x0d, char: TakeControlKey}
)

// detachIndex returns where the first Ctrl+] starts in chunk, or -1.
func detachIndex(chunk []byte) int { return keyIndex(chunk, detachKey) }

// takeControlIndex returns where the first Enter starts in chunk, or -1.
func takeControlIndex(chunk []byte) int { return keyIndex(chunk, takeControlKey) }

// keyIndex returns where the first press of k starts in chunk, or -1.
// Besides the raw byte it recognises the key as terminals encode it once a
// session's program enables xterm modifyOtherKeys (CSI 27;mods;code~), the
// kitty keyboard protocol (CSI code[:alt][;mods[:event][;text]]u) or Windows
// Terminal's win32-input-mode (CSI Vk;Sc;Uc;Kd;Cs;Rc_); those modes reach
// the native terminal with the session output.
func keyIndex(chunk []byte, k key) int {
	for i := 0; i < len(chunk); i++ {
		switch {
		case chunk[i] == k.raw:
			return i
		case chunk[i] == 0x1b && i+1 < len(chunk) && chunk[i+1] == '[':
			if isEncodedKey(chunk[i+2:], k) {
				return i
			}
		}
	}
	return -1
}

// isEncodedKey reports whether the CSI parameters and final byte at the
// start of p are a press (not a release) of k with exactly its modifiers.
func isEncodedKey(p []byte, k key) bool {
	end := 0
	for end < len(p) && (p[end] >= '0' && p[end] <= '9' || p[end] == ';' || p[end] == ':') {
		end++
	}
	if end == len(p) {
		return false
	}
	fields := bytes.Split(p[:end], []byte(";"))
	var code, modifiers []byte
	switch p[end] {
	case 'u':
		code = fields[0]
		if len(fields) > 1 {
			modifiers = fields[1]
		}
	case '~':
		if len(fields) != 3 || string(fields[0]) != "27" {
			return false
		}
		code, modifiers = fields[2], fields[1]
	case '_':
		return isWin32Key(fields, k)
	default:
		return false
	}
	code, _, _ = bytes.Cut(code, []byte(":"))
	modifiers, event, _ := bytes.Cut(modifiers, []byte(":"))
	m := 1
	if len(modifiers) > 0 {
		var ok bool
		if m, ok = atoi(modifiers); !ok {
			return false
		}
	}
	// Modifiers are 1+bits; ignore Caps Lock (64) and Num Lock (128).
	return string(code) == k.code && (m-1)&^(64|128) == k.mods && string(event) != "3"
}

// Control key state bits of a win32-input-mode event.
const (
	win32Alt   = 0x01 | 0x02
	win32Ctrl  = 0x04 | 0x08
	win32Shift = 0x10
)

// isWin32Key matches a win32-input-mode key-down event by virtual key or
// character; omitted parameters default to 0, except repeat count.
func isWin32Key(fields [][]byte, k key) bool {
	var v [6]int
	for i, f := range fields {
		if i == len(v) {
			return false
		}
		if len(f) == 0 {
			continue
		}
		n, ok := atoi(f)
		if !ok {
			return false
		}
		v[i] = n
	}
	vk, char, down, state := v[0], v[2], v[3], v[4]
	wantCtrl := k.mods&4 != 0
	if down != 1 || state&(win32Alt|win32Shift) != 0 || (state&win32Ctrl != 0) != wantCtrl {
		return false
	}
	return vk == k.vk || char == k.char
}

func atoi(b []byte) (int, bool) {
	if len(b) == 0 || len(b) > 4 {
		return 0, false
	}
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
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
		var out []byte
		switch frame[0] {
		case frameOutput:
			out = frame[1:]
		case frameSnapshot:
			// A full replay: reset the terminal first, as xterm.js does.
			out = append([]byte("\x1bc"), frame[1:]...)
		case frameExit:
			var body struct {
				ExitCode int `json:"exitCode"`
			}
			_ = json.Unmarshal(frame[1:], &body)
			exit = &Result{Exited: true, ExitCode: body.ExitCode}
		case frameControl:
			if a.observe {
				continue
			}
			if a.applyControl(frame[1:]) {
				// Sent when this connection becomes the active one again.
				a.resize(ctx, true)
				continue
			}
			out = []byte{}
		}
		if out == nil {
			continue
		}
		// Session output overwrites the banner; draw it again on top.
		if !a.observe && !a.isControlling() {
			out = append(out, a.banner()...)
		}
		if _, err := a.term.Write(out); err != nil {
			return readResult{err: err}
		}
	}
}
