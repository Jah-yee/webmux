package wslclient

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// WSL is the Windows launcher of WSL commands.
const WSL = "wsl.exe"

// WebmuxPath turns "~/x" into "./x" because `wsl.exe --exec` does not expand
// "~"; with `--cd ~` the path is relative to the WSL home directory.
func WebmuxPath(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return "./" + rest
	}
	return path
}

// DecodeOutput handles wsl.exe's UTF-16LE output (with or without BOM)
// and the UTF-8 output selected by WSL_UTF8=1.
func DecodeOutput(data []byte) string {
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
		return decodeUTF16(data[2:])
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return string(data[3:])
	}
	if len(data) >= 2 && len(data)%2 == 0 && looksUTF16(data) {
		return decodeUTF16(data)
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return decodeUTF16(data)
}

// looksUTF16 detects ASCII-heavy UTF-16LE: most odd bytes are zero.
func looksUTF16(data []byte) bool {
	zeros := 0
	for i := 1; i < len(data); i += 2 {
		if data[i] == 0 {
			zeros++
		}
	}
	return zeros*2 >= len(data)/2
}

func decodeUTF16(data []byte) string {
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[2*i:])
	}
	return string(utf16.Decode(units))
}

// Distro is one installed distribution.
type Distro struct {
	Name    string
	Default bool
	Version int
}

// ParseListQuiet parses `wsl.exe --list --quiet`.
func ParseListQuiet(data []byte) []string {
	var names []string
	for line := range strings.SplitSeq(DecodeOutput(data), "\n") {
		if name := strings.Trim(line, " \t\r\x00"); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ParseListVerbose parses `wsl.exe --list --verbose`: a header, then rows
// "[*] NAME STATE VERSION" with '*' marking the default distribution.
func ParseListVerbose(data []byte) []Distro {
	var distros []Distro
	lines := strings.Split(DecodeOutput(data), "\n")
	for i, line := range lines {
		line = strings.Trim(line, " \t\r\x00")
		if i == 0 || line == "" {
			continue
		}
		d := Distro{}
		if rest, ok := strings.CutPrefix(line, "*"); ok {
			d.Default = true
			line = strings.TrimSpace(rest)
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		d.Name = strings.Join(fields[:len(fields)-2], " ")
		switch fields[len(fields)-1] {
		case "1":
			d.Version = 1
		case "2":
			d.Version = 2
		}
		distros = append(distros, d)
	}
	return distros
}
