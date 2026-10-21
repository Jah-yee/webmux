package wslclient

import (
	"reflect"
	"slices"
	"testing"
	"unicode/utf16"
)

func utf16le(s string, bom bool) []byte {
	var out []byte
	if bom {
		out = append(out, 0xff, 0xfe)
	}
	for _, unit := range utf16.Encode([]rune(s)) {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return out
}

func TestParseDistroLists(t *testing.T) {
	quiet := "Ubuntu\r\n测试发行版\r\nDebian\r\n\r\n"
	for name, data := range map[string][]byte{"utf16": utf16le(quiet, false), "utf16 bom": utf16le(quiet, true), "utf8": []byte(quiet)} {
		if got := ParseListQuiet(data); !slices.Equal(got, []string{"Ubuntu", "测试发行版", "Debian"}) {
			t.Errorf("%s: %q", name, got)
		}
	}
	verbose := "  NAME            STATE           VERSION\r\n* Ubuntu          Running         2\r\n  测试发行版      Stopped         2\r\n  Legacy          Stopped         1\r\n"
	want := []Distro{{Name: "Ubuntu", Default: true, Version: 2}, {Name: "测试发行版", Version: 2}, {Name: "Legacy", Version: 1}}
	for _, data := range [][]byte{utf16le(verbose, false), []byte(verbose)} {
		if got := ParseListVerbose(data); !reflect.DeepEqual(got, want) {
			t.Errorf("verbose %+v", got)
		}
	}
}

func TestWebmuxPath(t *testing.T) {
	for in, want := range map[string]string{"~/.local/bin/webmux": "./.local/bin/webmux", "/usr/bin/webmux": "/usr/bin/webmux", "webmux": "webmux"} {
		if got := WebmuxPath(in); got != want {
			t.Errorf("%s: %s", in, got)
		}
	}
}
