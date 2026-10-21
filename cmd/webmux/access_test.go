package main

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"rsc.io/qr"
)

func TestLocalOrigin(t *testing.T) {
	for _, row := range []struct{ listen, want string }{
		{"127.0.0.1:0", "http://127.0.0.1:"},
		{"0.0.0.0:0", "http://127.0.0.1:"},
		{"[::1]:0", "http://[::1]:"},
	} {
		listener, err := net.Listen("tcp", row.listen)
		if err != nil {
			t.Logf("skip %s: %v", row.listen, err)
			continue
		}
		port := listener.Addr().(*net.TCPAddr).Port
		got := localOrigin(listener.Addr())
		_ = listener.Close()
		if port == 0 || got != row.want+strconv.Itoa(port) {
			t.Errorf("localOrigin(%s) = %q, want %q", row.listen, got, row.want+strconv.Itoa(port))
		}
	}
}

func TestGenerateToken(t *testing.T) {
	a, b := generateToken(), generateToken()
	if len(a) != 43 || a == b || url.PathEscape(a) != a {
		t.Fatalf("tokens %q %q", a, b)
	}
}

func TestRenderQRRoundTrips(t *testing.T) {
	text := "https://abc-def.trycloudflare.com/#token=" + strings.Repeat("x", 43)
	rendered, err := renderQR(text)
	if err != nil {
		t.Fatal(err)
	}
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(rendered, "\n"), "\n")
	side := code.Size + 2*qrQuiet
	if len(lines) != (side+1)/2 {
		t.Fatalf("%d lines for %d modules", len(lines), side)
	}
	for row, line := range lines {
		cells := []rune(line)
		if len(cells) != side {
			t.Fatalf("line %d has %d cells, want %d", row, len(cells), side)
		}
		for col, cell := range cells {
			top := cell == '█' || cell == '▀'
			bottom := cell == '█' || cell == '▄'
			x, y := col-qrQuiet, 2*row-qrQuiet
			if top != light(code, x, y) || (y+1 < code.Size+qrQuiet && bottom != light(code, x, y+1)) {
				t.Fatalf("module mismatch at x=%d y=%d: %q", x, y, cell)
			}
		}
	}
}

func light(code *qr.Code, x, y int) bool { return !code.Black(x, y) }
