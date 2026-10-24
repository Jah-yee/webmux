package main

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"strconv"
	"strings"

	"rsc.io/qr"
)

// localOrigin is the http origin for the bound address; wildcard listeners
// are reached through loopback.
func localOrigin(addr net.Addr) string {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return "http://" + addr.String()
	}
	host := "127.0.0.1"
	if !tcp.IP.IsUnspecified() {
		host = tcp.IP.String()
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(tcp.Port))
}

// loopbackListen reports whether listen binds only a loopback address;
// wildcards, interface addresses and other host names reach the network.
func loopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// minTunnelTokenLength matches the 43 URL-safe characters of generateToken;
// a tunnel exposed to the internet must not use a shorter configured token.
const minTunnelTokenLength = 43

// generateToken returns 256 random bits, URL-safe so links need no escaping.
func generateToken() string {
	key := make([]byte, 32)
	_, _ = rand.Read(key) // crypto/rand.Read never fails; it aborts the process instead.
	return base64.RawURLEncoding.EncodeToString(key)
}

// qrQuiet is the four-module light border the QR specification requires.
const qrQuiet = 4

// renderQR draws text as a QR code using half-block characters, two module
// rows per line. Light modules are filled so the code reads on dark terminals.
func renderQR(text string) (string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", err
	}
	light := func(x, y int) bool {
		inside := x >= -qrQuiet && x < code.Size+qrQuiet && y >= -qrQuiet && y < code.Size+qrQuiet
		return inside && !code.Black(x, y)
	}
	var b strings.Builder
	for y := -qrQuiet; y < code.Size+qrQuiet; y += 2 {
		for x := -qrQuiet; x < code.Size+qrQuiet; x++ {
			switch top, bottom := light(x, y), light(x, y+1); {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}
