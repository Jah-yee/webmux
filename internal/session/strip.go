package session

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/0magnet/xterm-go/vt"
)

const (
	maxPreviewLines = 100
	maxPreviewRunes = 120
)

// Preview observes the PTY's current screen, not its append-only output history.
// It is owned by Session.mu; terminal replies must never reach the real PTY.
// Up to 100 bottom nonempty screen rows allow per-browser preview selection.
type Preview struct {
	screen *vt.Terminal
	last   string // Bottom nonempty screen row, used for status classification.
	text   string
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
	return s.snapshot()
}

// Feed consumes split UTF-8 and terminal control sequences without changing
// the bytes delivered to subscribers. Screen erasure also clears the preview.
func (s *Preview) Feed(p []byte) string {
	if len(p) == 0 {
		return s.text
	}
	if s.screen == nil {
		s.Resize(80, 24)
	}
	s.screen.Write(p)
	return s.snapshot()
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
