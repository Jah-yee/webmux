package session

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/0magnet/xterm-go/vt"
)

const (
	maxPreviewLines = 100
	maxPreviewRunes = 120
	// previewRefreshInterval bounds how often a burst of PTY output rebuilds
	// the preview string; the manager flush below catches the final chunk.
	previewRefreshInterval = 200 * time.Millisecond
)

// Preview observes the PTY's current screen, not its append-only output history.
// It is owned by Session.mu; terminal replies must never reach the real PTY.
// Up to 100 bottom nonempty screen rows allow per-browser preview selection.
type Preview struct {
	screen *vt.Terminal
	last   string // Bottom nonempty screen row, used for status classification.
	text   string
	// dirty marks screen changes not yet folded into text and last.
	dirty       bool
	refreshedAt time.Time
	// colorSchemeReports is DEC mode 2031 (color scheme change reports).
	colorSchemeReports bool
}

// Resize follows successful PTY resizes, including before the first output.
func (s *Preview) Resize(cols, rows uint16) string {
	if s.screen == nil {
		options := vt.NewOptions()
		options.Cols, options.Rows = int(cols), int(rows)
		options.Scrollback = 0
		// Leave OnData unset: this observer must not answer device queries or
		// inject input. Only the browser terminal may write back to the PTY.
		s.screen = vt.NewTerminal(options)
		s.boundRepeats()
		s.boundCombining()
		s.ignoreLinks()
		s.trackColorScheme()
	} else if s.screen.Cols() != int(cols) || s.screen.Rows() != int(rows) {
		s.screen.Resize(int(cols), int(rows))
	} else {
		return s.text
	}
	// With zero scrollback, retained normal-buffer rows are all viewport rows.
	// VT reflow can leave a phantom YBase after narrowing; the next height
	// increase then pulls nonexistent history and leaves a nil viewport row.
	normal := s.screen.Buffers().Normal()
	normal.YBase, normal.YDisp = 0, 0
	s.dirty = false
	s.refreshedAt = time.Now()
	return s.snapshot()
}

// Feed consumes split UTF-8 and terminal control sequences without changing
// the bytes delivered to subscribers. Screen erasure also clears the preview.
// It refreshes immediately; the session read path uses FeedThrottled.
func (s *Preview) Feed(p []byte) string {
	s.write(p)
	return s.refresh(true)
}

// FeedThrottled is Feed for the PTY hot path: it records the screen change and
// rebuilds text at most once per previewRefreshInterval.
func (s *Preview) FeedThrottled(p []byte) string {
	s.write(p)
	return s.refresh(false)
}

func (s *Preview) write(p []byte) {
	if len(p) == 0 {
		return
	}
	created := false
	if s.screen == nil {
		s.Resize(80, 24)
		created = true
	}
	s.screen.Write(p)
	s.dirty = true
	if created {
		// Resize stamped refreshedAt; the first chunk must still render now.
		s.refreshedAt = time.Time{}
	}
}

// refresh rebuilds text and last from a dirty screen, at most once per
// previewRefreshInterval unless forced. It is called with Session.mu held.
func (s *Preview) refresh(force bool) string {
	if !s.dirty {
		return s.text
	}
	now := time.Now()
	if !force && now.Sub(s.refreshedAt) < previewRefreshInterval {
		return s.text
	}
	s.dirty = false
	s.refreshedAt = now
	return s.snapshot()
}

// recoveryFrame restores a small set of persistent terminal modes on a fresh,
// empty browser terminal. It never copies retained screen text or parser input.
// Cursor position, attributes, margins, charsets and in-flight parser/synchronized
// output state are deliberately not serialized; the program must redraw.
func (s *Preview) recoveryFrame() []byte {
	if s.screen == nil {
		return []byte{1}
	}
	frame := make([]byte, 1, 128)
	frame[0] = 1
	if s.screen.Buffer() == s.screen.Buffers().Alt() {
		frame = append(frame, "\x1b[?1049h"...)
	}
	core := s.screen.CoreService()
	modes := &core.DecPrivateModes
	if core.Modes.InsertMode {
		frame = append(frame, "\x1b[4h"...)
	}
	if modes.ApplicationCursorKeys {
		frame = append(frame, "\x1b[?1h"...)
	}
	if modes.ApplicationKeypad {
		frame = append(frame, "\x1b[?66h"...)
	}
	if modes.BracketedPasteMode {
		frame = append(frame, "\x1b[?2004h"...)
	}
	if modes.SendFocus {
		frame = append(frame, "\x1b[?1004h"...)
	}
	if modes.Origin {
		frame = append(frame, "\x1b[?6h"...)
	}
	if !modes.Wraparound {
		frame = append(frame, "\x1b[?7l"...)
	}
	if modes.ReverseWraparound {
		frame = append(frame, "\x1b[?45h"...)
	}
	if core.IsCursorHidden {
		frame = append(frame, "\x1b[?25l"...)
	}
	switch s.screen.MouseService().ActiveProtocol() {
	case "X10":
		frame = append(frame, "\x1b[?9h"...)
	case "VT200":
		frame = append(frame, "\x1b[?1000h"...)
	case "DRAG":
		frame = append(frame, "\x1b[?1002h"...)
	case "ANY":
		frame = append(frame, "\x1b[?1003h"...)
	}
	switch s.screen.MouseService().ActiveEncoding() {
	case "SGR":
		frame = append(frame, "\x1b[?1006h"...)
	case "SGR_PIXELS":
		frame = append(frame, "\x1b[?1016h"...)
	}
	if s.colorSchemeReports {
		frame = append(frame, "\x1b[?2031h"...)
	}
	return frame
}

func (s *Preview) snapshot() string {
	var lines [maxPreviewLines]string
	n := maxPreviewLines
	for y := s.screen.Rows() - 1; y >= 0 && n > 0; y-- {
		if line := s.row(y); line != "" {
			n--
			lines[n] = line
		}
	}
	s.last = ""
	if n < maxPreviewLines {
		s.last = strings.TrimSpace(lines[maxPreviewLines-1])
	}
	s.text = strings.Join(lines[n:], "\n")
	return s.text
}

// row preserves indentation and omits wide-character continuation cells.
// Truncation never splits a UTF-8 character or the contents of a combined cell.
func (s *Preview) row(y int) string {
	buffer := s.screen.Buffer()
	line := buffer.Lines.Get(buffer.YBase + y)
	if line == nil {
		return ""
	}
	end := line.Length - 1
	for end >= 0 {
		if line.HasContent(end) && !unicode.IsSpace(rune(line.GetCodePoint(end))) {
			break
		}
		end--
	}
	if end < 0 {
		return ""
	}
	var buf [maxPreviewRunes * utf8.UTFMax]byte
	text := buf[:0]
	runes := 0
	for x := 0; x <= end && runes < maxPreviewRunes; {
		if !line.HasContent(x) {
			text = append(text, ' ')
			runes++
			x++
			continue
		}
		if line.IsCombined(x) {
			content := line.GetString(x)
			n := utf8.RuneCountInString(content)
			if runes+n > maxPreviewRunes {
				break
			}
			text = append(text, content...)
			runes += n
		} else {
			text = utf8.AppendRune(text, rune(line.GetCodePoint(x)))
			runes++
		}
		x += max(1, line.GetWidth(x))
	}
	return string(text)
}
