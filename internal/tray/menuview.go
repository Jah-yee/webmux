package tray

import (
	"github.com/rainchan/webmux/internal/contract"
)

// InstallItem describes the install / upgrade menu entry.
type InstallItem struct {
	Show, Enabled bool
	Title         string
}

// InstallEntry derives the install entry. supported is false for a tray
// without server digests; installing is true while an install runs.
func InstallEntry(snap Snapshot, supported, installing bool) InstallItem {
	if !snap.Installable() {
		return InstallItem{}
	}
	title := "安装 WSL 中的 webmux"
	switch {
	case snap.Blocker == BlockerIncompatible:
		title = "升级 WSL 中的 webmux"
	case snap.UpgradeTo != "":
		title = "升级 WSL 中的 webmux（" + snap.WebmuxVersion + " → " + snap.UpgradeTo + "）"
	}
	switch {
	case !supported:
		return InstallItem{Show: true, Title: title + "（" + ErrInstallUnsupported.Error() + "）"}
	case installing:
		return InstallItem{Show: true, Title: "正在安装 WSL 中的 webmux…"}
	}
	busy := snap.State == StateStarting || snap.State == StateStopping || snap.State == StateRestarting
	return InstallItem{Show: true, Enabled: !busy, Title: title}
}

// UpgradeHint is the status line for an older installed server.
func UpgradeHint(snap Snapshot) string {
	if snap.UpgradeTo == "" {
		return ""
	}
	return "WSL 中的 webmux " + snap.WebmuxVersion + " 可升级到 " + snap.UpgradeTo
}

// TunnelItem describes the public tunnel entries.
type TunnelItem struct {
	// Show is false when no instance runs or it cannot control its tunnel.
	Show, Checked bool
	// Info is the state line; PublicURL is set only when ready.
	Info, PublicURL string
}

// TunnelEntry derives the tunnel entries from the running instance.
func TunnelEntry(snap Snapshot) TunnelItem {
	t := snap.Status.Tunnel
	if !snap.HasStatus || snap.Status.State != contract.StateRunning || t == nil || !snap.Actions().Access {
		return TunnelItem{}
	}
	item := TunnelItem{Show: true, Checked: snap.TunnelActive()}
	switch t.State {
	case contract.TunnelStarting:
		item.Info = "公网隧道：正在连接"
	case contract.TunnelReady:
		item.Info = "公网隧道：" + t.PublicURL
		item.PublicURL = t.PublicURL
	case contract.TunnelFailed:
		item.Info = "公网隧道失败：" + t.Error
	}
	return item
}

// TunnelWarning is the confirmation shown before every enable.
const TunnelWarning = "开启公网隧道后，WebMux 会通过 Cloudflare 暴露到公网。\n\n任何获得链接和 Token 的人都能以你的身份执行命令。Quick Tunnel 没有额外的身份验证，Token 是唯一的屏障。\n\n确定开启？"
