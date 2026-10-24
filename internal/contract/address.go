package contract

import (
	"net"
	"net/netip"
)

var tailscaleRange = netip.MustParsePrefix("100.64.0.0/10")

// RemoteIPv4 returns the 4-byte form of ip when another device could use it
// to reach this host: IPv4 (including IPv4-mapped IPv6) that is not loopback,
// link-local or unspecified. ok is false for every other address.
func RemoteIPv4(ip net.IP) (v4 net.IP, ok bool) {
	v4 = ip.To4()
	if v4 == nil || v4.IsLoopback() || v4.IsLinkLocalUnicast() || v4.IsUnspecified() {
		return nil, false
	}
	return v4, true
}

// AddressKind is the RemoteURL.Kind of ip: "tailscale" inside 100.64.0.0/10,
// otherwise "lan" (including nil and non-IPv4 addresses).
func AddressKind(ip net.IP) string {
	if addr, ok := netip.AddrFromSlice(ip.To4()); ok && tailscaleRange.Contains(addr) {
		return "tailscale"
	}
	return "lan"
}
