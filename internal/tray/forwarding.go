package tray

import (
	"net"
	"sort"
	"strings"

	"github.com/rainchan/webmux/internal/contract"
)

// WinInterface is one Windows network interface.
type WinInterface struct {
	Name     string
	Up       bool
	Loopback bool
	Addrs    []net.IP
}

// Candidate is a Windows address the relay may listen on.
type Candidate struct {
	Address   string
	Interface string
	// Kind is "tailscale" or "lan".
	Kind string
}

// WindowsCandidates lists enabled IPv4 addresses, excluding loopback,
// link-local and the WSL virtual switch, with Tailscale first.
func WindowsCandidates(list []WinInterface) []Candidate {
	var out []Candidate
	for _, iface := range list {
		if !iface.Up || iface.Loopback || strings.HasPrefix(iface.Name, "vEthernet (WSL") {
			continue
		}
		for _, ip := range iface.Addrs {
			v4, ok := contract.RemoteIPv4(ip)
			if !ok {
				continue
			}
			out = append(out, Candidate{Address: v4.String(), Interface: iface.Name, Kind: contract.AddressKind(v4)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Kind == "tailscale" && out[j].Kind != "tailscale" })
	return out
}

// ForwardPlan drives the "远程访问" submenu.
type ForwardPlan struct {
	// Needed is false when WSL is already reachable from other devices.
	Needed bool
	// Headline is the recommendation shown first.
	Headline string
	// Blocked explains why forwarding cannot be enabled now, or "".
	Blocked string
	// OfferInit offers `webmux config init` when no config file exists.
	OfferInit  bool
	Candidates []Candidate
}

// PlanForward applies windows-tray-forwarding: mirrored mode or reported
// remote links make forwarding unnecessary; otherwise forwarding needs a
// healthy WebMux with a token that Windows can reach.
func PlanForward(networkMode string, snap Snapshot, list []WinInterface) ForwardPlan {
	status := snap.Status
	if strings.EqualFold(networkMode, "mirrored") {
		return ForwardPlan{Headline: "无需转发（镜像网络模式）", Blocked: "镜像网络模式下其他设备可直接访问 WSL"}
	}
	if snap.HasStatus && len(status.URLs.Remote) > 0 {
		return ForwardPlan{Headline: "无需转发：WebMux 已提供远程链接", Blocked: "WebMux 已在可访问的地址上监听"}
	}
	plan := ForwardPlan{Needed: true, Headline: "可通过托盘转发", Candidates: WindowsCandidates(list)}
	running := snap.HasStatus && status.State == contract.StateRunning && status.Health.Status == contract.HealthOK
	switch {
	case snap.Degraded:
		plan.Blocked = "WSL 中的 webmux 与托盘不兼容，无法转发"
	case !running:
		plan.Blocked = "WebMux 未运行或不健康"
	case !status.TokenConfigured:
		plan.Blocked = "转发会把 shell 暴露给网络，请先设置 Token"
		if status.Config.Exists {
			plan.Blocked += "（在配置中设置 token 并重启 WebMux）"
		} else {
			plan.OfferInit = true
		}
	case !snap.Reachable:
		plan.Blocked = "Windows 无法访问 WebMux 本机链接"
	}
	return plan
}

// ForwardTarget is the authority of the local link the relay targets.
func ForwardTarget(status contract.StatusV1) (host, port string, ok bool) {
	authority := strings.TrimPrefix(status.URLs.Local, "http://")
	host, port, err := net.SplitHostPort(authority)
	return host, port, err == nil && port != ""
}
