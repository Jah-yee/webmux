package session

import (
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	maxSignalPayload = 4096
	maxTitlePayload  = 512
	maxCwdPath       = 4096
	maxCwdHost       = 255
	warpAgentTitle   = "warp://cli-agent"
)

// SignalHandler receives terminal side-channel signals. Nil fields are ignored.
// Notification text and event bodies are deliberately not passed on: only
// their presence and the structured event name may influence session state.
type SignalHandler struct {
	Title  func(string)
	Bell   func()
	Notify func()
	Event  func(string)
	Cwd    func(path, host string)
}

// OSCScanner is incremental and bounded, including across split ST terminators.
// Besides OSC titles and notifications it tracks DCS/SOS/PM/APC strings so a
// BEL inside them is not mistaken for a bare bell.
type OSCScanner struct {
	state    byte
	buf      []byte
	overflow bool
}

const (
	scanGround byte = iota
	scanEscape
	scanOSC
	scanOSCEscape
	scanString
	scanStringEscape
)

func (s *OSCScanner) Feed(p []byte, h *SignalHandler) {
	for _, b := range p {
		switch s.state {
		case scanGround:
			switch b {
			case 27:
				s.state = scanEscape
			case 7:
				if h.Bell != nil {
					h.Bell()
				}
			}
		case scanEscape:
			switch b {
			case ']':
				s.state = scanOSC
				s.buf = s.buf[:0]
				s.overflow = false
			case 'P', 'X', '^', '_':
				s.state = scanString
			case 27:
			case 7:
				s.state = scanGround
				if h.Bell != nil {
					h.Bell()
				}
			default:
				s.state = scanGround
			}
		case scanOSC:
			switch b {
			case 7:
				s.finish(h)
			case 27:
				s.state = scanOSCEscape
			default:
				s.append(b)
			}
		case scanOSCEscape:
			if b == '\\' {
				s.finish(h)
			} else {
				s.append(27)
				s.append(b)
				s.state = scanOSC
			}
		case scanString:
			if b == 27 {
				s.state = scanStringEscape
			}
		case scanStringEscape:
			switch b {
			case '\\':
				s.state = scanGround
			case 27:
			default:
				s.state = scanString
			}
		}
	}
}
func (s *OSCScanner) append(b byte) {
	if len(s.buf) < maxSignalPayload {
		s.buf = append(s.buf, b)
	} else {
		s.overflow = true
	}
}
func (s *OSCScanner) finish(h *SignalHandler) {
	s.state = scanGround
	if s.overflow {
		return
	}
	text := string(s.buf)
	switch {
	case strings.HasPrefix(text, "0;") || strings.HasPrefix(text, "2;"):
		if len(text) <= maxTitlePayload && h.Title != nil {
			h.Title(strings.TrimSpace(text[2:]))
		}
	case strings.HasPrefix(text, "7;"):
		if path, host, ok := parseOSC7(text[2:]); ok && h.Cwd != nil {
			h.Cwd(path, host)
		}
	case strings.HasPrefix(text, "9;"):
		if !numericSubcommand(text[2:]) && h.Notify != nil {
			h.Notify()
		}
	case strings.HasPrefix(text, "777;notify;"):
		title, body, _ := strings.Cut(text[len("777;notify;"):], ";")
		if title != warpAgentTitle {
			if h.Notify != nil {
				h.Notify()
			}
			return
		}
		if event, ok := agentEvent(body); ok && h.Event != nil {
			h.Event(event)
		}
	}
}

// parseOSC7 accepts the file URL shape used by shell integrations. Only the
// path and host are retained; credentials, ports, queries and fragments are
// rejected so a report cannot smuggle a different location into the UI.
func parseOSC7(raw string) (path, host string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" ||
		u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", "", false
	}
	path, host = u.Path, u.Hostname()
	if len(path) > maxCwdPath || len(host) > maxCwdHost || !strings.HasPrefix(path, "/") {
		return "", "", false
	}
	if !utf8.ValidString(path) || !utf8.ValidString(host) || containsControl(path) || containsControl(host) {
		return "", "", false
	}
	return path, host, true
}

func containsControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}

// reportedHostIsLocal treats loopback names and the service host's own name as
// local. A host with a domain only matches a local hostname with the same
// domain; a bare name may match the local short name.
func reportedHostIsLocal(host, hostname string) bool {
	h := normalizeHost(host)
	if h == "" || h == "localhost" || h == "localhost.localdomain" || h == "ip6-localhost" {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	local := normalizeHost(hostname)
	if local == "" {
		return false
	}
	if h == local {
		return true
	}
	if !strings.Contains(h, ".") {
		return h == shortHost(local)
	}
	if !strings.Contains(local, ".") {
		return shortHost(h) == local
	}
	return false
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
}

func shortHost(host string) string {
	if i := strings.IndexByte(host, '.'); i > 0 {
		return host[:i]
	}
	return host
}

// numericSubcommand reports ConEmu-style OSC 9 subcommands such as 9;4 progress.
func numericSubcommand(text string) bool {
	digits := 0
	for digits < len(text) && text[digits] >= '0' && text[digits] <= '9' {
		digits++
	}
	return digits > 0 && digits < len(text) && text[digits] == ';'
}

// agentEvent accepts only a JSON object with an integer v >= 1 and a string
// event. Every other field is ignored and never retained.
func agentEvent(body string) (string, bool) {
	var envelope struct {
		V     *int64  `json:"v"`
		Event *string `json:"event"`
	}
	if !strings.HasPrefix(strings.TrimSpace(body), "{") {
		return "", false
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return "", false
	}
	if envelope.V == nil || *envelope.V < 1 || envelope.Event == nil {
		return "", false
	}
	return *envelope.Event, true
}
