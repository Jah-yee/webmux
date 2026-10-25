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
	"math"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
)

// inputModeReset turns off what a session's program may have enabled in the
// native terminal: win32-input-mode, modifyOtherKeys, kitty keyboard flags,
// mouse and focus reporting, bracketed paste and colour scheme reports; and
// shows the cursor.
const inputModeReset = "\x1b[?9001l\x1b[>4;0m\x1b[<99u\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?1004l\x1b[?2004l\x1b[?2031l\x1b[?25h"

// backgroundQuery asks the native terminal for its background colour (OSC 11),
// which nearly every terminal answers, unlike CSI ? 996 n.
const backgroundQuery = "\x1b]11;?\x1b\\"

// DetachKey is Ctrl+], which never reaches the session.
const DetachKey = 0x1d

// TakeControlKey is Enter: while another client controls the session it
// reclaims control and is not forwarded.
const TakeControlKey = '\r'

// ResizeInterval is how often the terminal size is polled.
const ResizeInterval = 250 * time.Millisecond

// Frame types of docs/protocol.md.
const (
	frameOutput   = contract.FrameOutput
	frameSnapshot = contract.FrameSnapshot
	frameExit     = contract.FrameExit
	frameControl  = contract.FrameControl

	sendInput   = contract.FrameInput
	sendResize  = contract.FrameResize
	sendResync  = contract.FrameReplay
	sendControl = contract.FrameClaim
	sendReply   = contract.FrameReply
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
	// unconfirmed claim, the controlling client's label, whether the
	// program asked for colour scheme reports, and unanswered background
	// queries.
	controlMu     sync.Mutex
	controlled    bool
	claiming      bool
	controller    string
	schemeReports bool
	probes        int
}

func (a *attachment) send(ctx context.Context, frame []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.conn.Write(ctx, websocket.MessageBinary, frame)
}

// resize sends the terminal size when it changed, or always when forced.
func (a *attachment) resize(ctx context.Context, force bool) {
	cols, rows, err := a.term.Size()
	if err != nil || cols <= 0 || rows <= 0 || cols > math.MaxUint16 || rows > math.MaxUint16 {
		return
	}
	a.sizeMu.Lock()
	changed := force || cols != a.cols || rows != a.rows
	a.cols, a.rows = cols, rows
	a.sizeMu.Unlock()
	if !changed {
		return
	}
	body, _ := json.Marshal(contract.TerminalResize{Cols: uint16(cols), Rows: uint16(rows)})
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
			chunk, report := a.takeBackground(buf[:n])
			if report != nil {
				if err := a.send(ctx, append([]byte{sendReply}, report...)); err != nil {
					return fmt.Errorf("connection lost: %w", err)
				}
			}
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

// takeBackground removes the answers to this client's background queries
// from chunk. While controlling, it returns the colour scheme report for the
// last one: CSI ? 997 ; 1 n for dark, 2 for light. A reply split across reads
// is not recognised and reaches the session as input.
func (a *attachment) takeBackground(chunk []byte) ([]byte, []byte) {
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	var report []byte
	for a.probes > 0 {
		start, end, dark, ok := backgroundReply(chunk)
		if !ok {
			break
		}
		a.probes--
		chunk = append(chunk[:start:start], chunk[end:]...)
		if a.controlled && !a.claiming && a.schemeReports {
			report = []byte("\x1b[?997;2n")
			if dark {
				report = []byte("\x1b[?997;1n")
			}
		}
	}
	return chunk, report
}

// backgroundReply finds an OSC 11 answer, ESC ] 11 ; rgb:R/G/B ended by BEL
// or ST, in p and reports whether that colour is dark.
func backgroundReply(p []byte) (start, end int, dark, ok bool) {
	const prefix = "\x1b]11;rgb:"
	start = bytes.Index(p, []byte(prefix))
	if start < 0 {
		return 0, 0, false, false
	}
	body := p[start+len(prefix):]
	n := bytes.IndexAny(body, "\a\x1b")
	if n < 0 {
		return 0, 0, false, false
	}
	end = start + len(prefix) + n + 1
	if body[n] == 0x1b {
		if n+1 >= len(body) || body[n+1] != '\\' {
			return 0, 0, false, false
		}
		end++
	}
	parts := bytes.Split(body[:n], []byte("/"))
	if len(parts) != 3 {
		return 0, 0, false, false
	}
	luminance := 0.0
	for i, weight := range []float64{0.2126, 0.7152, 0.0722} {
		digits := len(parts[i])
		v, err := strconv.ParseUint(string(parts[i]), 16, 16)
		if err != nil || digits == 0 || digits > 4 {
			return 0, 0, false, false
		}
		luminance += weight * float64(v) / float64(uint64(1)<<(4*digits)-1)
	}
	return start, end, luminance < 0.5, true
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
// controls the session afterwards, and whether it should query its
// background to announce its colour scheme: as in the browser, when it starts
// answering for a program that wants reports, or the program starts wanting
// them. The program may have asked before this terminal attached, or been
// answered with another device's theme.
func (a *attachment) applyControl(body []byte) (controlling, probe bool) {
	var state contract.ControlState
	if json.Unmarshal(body, &state) != nil {
		return a.isControlling(), false
	}
	a.controlMu.Lock()
	defer a.controlMu.Unlock()
	was := a.controlled && !a.claiming
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
	// The mode belongs to the session, so it is current even mid-claim.
	probe = state.ColorSchemeReports && mine && (!was || !a.schemeReports)
	a.schemeReports = state.ColorSchemeReports
	if probe {
		a.probes++
	}
	return a.controlled, probe
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
			var body contract.ExitFrame
			_ = json.Unmarshal(frame[1:], &body)
			exit = &Result{Exited: true, ExitCode: body.ExitCode}
		case frameControl:
			if a.observe {
				continue
			}
			controlling, probe := a.applyControl(frame[1:])
			if controlling {
				// Sent when this connection becomes the active one again.
				a.resize(ctx, true)
				if !probe {
					continue
				}
				out = []byte(backgroundQuery)
				break
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
