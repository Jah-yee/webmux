package client

import (
	"net"
	"sort"
	"strings"

	"github.com/rainchan/webmux/internal/contract"
)

// Interface is one host network interface considered for remote links.
type Interface struct {
	Name     string
	Up       bool
	Loopback bool
	Addrs    []net.IP
	// WSLVirtual marks the WSL NAT-mode virtual NIC, which LAN devices
	// cannot reach directly.
	WSLVirtual bool
}

// ExposedWarning is reported for a wildcard listener without a token.
const ExposedWarning = "SHELL EXPOSED: listening on all interfaces without a token; isolate with Tailscale/WireGuard or set a token"

// Links derives token-free http links for the bound address listen. The
// server's /local/v1/status and the CLI fallback both call it.
func Links(listen string, tokenConfigured bool, interfaces func() ([]Interface, error)) (contract.URLs, []string) {
	urls := contract.URLs{Remote: []contract.RemoteURL{}}
	var warnings []string
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return urls, warnings
	}
	link := func(h string) string { return "http://" + net.JoinHostPort(h, port) }
	ip := net.ParseIP(host)
	switch {
	case strings.EqualFold(host, "localhost") || ip != nil && ip.IsLoopback():
		if ip != nil && ip.To4() == nil {
			urls.Local = link("::1")
		} else {
			urls.Local = link("127.0.0.1")
		}
	case host == "" || ip != nil && ip.IsUnspecified():
		urls.Local = link("127.0.0.1")
		if !tokenConfigured {
			warnings = append(warnings, ExposedWarning)
		}
		list, _ := interfaces()
		urls.Remote = remoteCandidates(list, port)
	default:
		urls.Local = link(host)
		if ip != nil {
			host = ip.String()
		}
		urls.Remote = append(urls.Remote, contract.RemoteURL{URL: link(host), Interface: interfaceOf(ip, interfaces), Kind: contract.AddressKind(ip)})
	}
	return urls, warnings
}

func remoteCandidates(list []Interface, port string) []contract.RemoteURL {
	remote := []contract.RemoteURL{}
	for _, iface := range list {
		if !iface.Up || iface.Loopback || iface.WSLVirtual {
			continue
		}
		for _, ip := range iface.Addrs {
			v4, ok := contract.RemoteIPv4(ip)
			if !ok {
				continue
			}
			remote = append(remote, contract.RemoteURL{
				URL:       "http://" + net.JoinHostPort(v4.String(), port),
				Interface: iface.Name,
				Kind:      contract.AddressKind(v4),
			})
		}
	}
	// Tailscale first; otherwise keep interface order.
	sort.SliceStable(remote, func(i, j int) bool {
		return remote[i].Kind == "tailscale" && remote[j].Kind != "tailscale"
	})
	return remote
}

func interfaceOf(ip net.IP, interfaces func() ([]Interface, error)) string {
	if ip == nil {
		return ""
	}
	list, _ := interfaces()
	for _, iface := range list {
		for _, addr := range iface.Addrs {
			if addr.Equal(ip) {
				return iface.Name
			}
		}
	}
	return ""
}

// NetInterfaces lists the host's interfaces with their unicast addresses.
func NetInterfaces() ([]Interface, error) {
	list, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	virtual := wslVirtualInterfaces()
	result := make([]Interface, 0, len(list))
	for _, iface := range list {
		item := Interface{
			Name:       iface.Name,
			Up:         iface.Flags&net.FlagUp != 0,
			Loopback:   iface.Flags&net.FlagLoopback != 0,
			WSLVirtual: virtual[iface.Name],
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if prefix, ok := addr.(*net.IPNet); ok {
				item.Addrs = append(item.Addrs, prefix.IP)
			}
		}
		result = append(result, item)
	}
	return result, nil
}

// LocalURL is the loopback-or-bound link used for TCP health checks.
func LocalURL(listen string) string {
	urls, _ := Links(listen, true, func() ([]Interface, error) { return nil, nil })
	return urls.Local
}
