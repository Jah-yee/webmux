package attach

import "bytes"

// The session answers the OSC 7501 capability query itself. A native
// terminal that understands the protocol would answer it a second time, so
// the query is withheld from it; status reports still pass through.
var programStatusProbes = [][]byte{[]byte("\x1b]7501;?\a"), []byte("\x1b]7501;?\x1b\\")}

// probeFilter removes complete probes from the output stream. A chunk that
// ends in a possible probe prefix holds that prefix back until the next one.
type probeFilter struct {
	held []byte
}

func (f *probeFilter) reset() { f.held = nil }

func (f *probeFilter) filter(p []byte) []byte {
	if len(f.held) == 0 && bytes.IndexByte(p, 0x1b) < 0 {
		return p
	}
	in := append(f.held, p...)
	f.held = nil
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); {
		if in[i] != 0x1b {
			next := bytes.IndexByte(in[i:], 0x1b)
			if next < 0 {
				out = append(out, in[i:]...)
				break
			}
			out = append(out, in[i:i+next]...)
			i += next
			continue
		}
		rest := in[i:]
		matched, partial := 0, false
		for _, probe := range programStatusProbes {
			if bytes.HasPrefix(rest, probe) {
				matched = len(probe)
				break
			}
			if len(rest) < len(probe) && bytes.HasPrefix(probe, rest) {
				partial = true
			}
		}
		switch {
		case matched > 0:
			i += matched
		case partial:
			f.held = append([]byte(nil), rest...)
			i = len(in)
		default:
			out = append(out, 0x1b)
			i++
		}
	}
	return out
}
