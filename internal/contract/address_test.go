package contract

import (
	"net"
	"testing"
)

func TestRemoteIPv4(t *testing.T) {
	for _, row := range []struct {
		ip   string
		want string // "" when rejected
	}{
		{"192.168.1.20", "192.168.1.20"},
		{"100.64.0.1", "100.64.0.1"},
		{"::ffff:10.0.0.7", "10.0.0.7"},
		{"127.0.0.1", ""},
		{"127.8.9.10", ""},
		{"169.254.0.1", ""},
		{"0.0.0.0", ""},
		{"::1", ""},
		{"fe80::1", ""},
		{"2001:db8::1", ""},
	} {
		got, ok := RemoteIPv4(net.ParseIP(row.ip))
		if ok != (row.want != "") {
			t.Errorf("RemoteIPv4(%s) ok = %v", row.ip, ok)
			continue
		}
		if ok && (len(got) != net.IPv4len || got.String() != row.want) {
			t.Errorf("RemoteIPv4(%s) = %v (len %d), want 4-byte %s", row.ip, got, len(got), row.want)
		}
	}
	if _, ok := RemoteIPv4(nil); ok {
		t.Error("RemoteIPv4(nil) accepted")
	}
}

func TestAddressKind(t *testing.T) {
	for _, row := range []struct{ ip, want string }{
		{"100.64.0.0", "tailscale"},
		{"100.101.102.103", "tailscale"},
		{"100.127.255.255", "tailscale"},
		{"::ffff:100.64.0.1", "tailscale"},
		{"100.63.255.255", "lan"},
		{"100.128.0.0", "lan"},
		{"192.168.1.20", "lan"},
		{"fd7a:115c:a1e0::1", "lan"},
	} {
		if got := AddressKind(net.ParseIP(row.ip)); got != row.want {
			t.Errorf("AddressKind(%s) = %q, want %q", row.ip, got, row.want)
		}
	}
	if got := AddressKind(nil); got != "lan" {
		t.Errorf("AddressKind(nil) = %q, want lan", got)
	}
}
