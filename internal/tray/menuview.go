package tray

import (
	"fmt"
	"strings"

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

// PushItem describes the phone push hint under the tunnel entries.
type PushItem struct {
	// Show is false when the tunnel entries are hidden or the server
	// predates push.
	Show bool
	// Info is the hint line; Guide is the setup dialog text. Neither
	// carries a token, tunnel credential or subscription endpoint.
	Info, Guide string
}

// PushEntry derives the push hint. Phones can only subscribe from a Named
// Tunnel origin, so the hint names that prerequisite first.
func PushEntry(snap Snapshot) PushItem {
	p := snap.Status.Push
	if p == nil || !TunnelEntry(snap).Show {
		return PushItem{}
	}
	item := PushItem{Show: true}
	switch {
	case !p.Enabled:
		item.Info = "手机推送：已在配置中停用"
	case !p.Available:
		item.Info = "手机推送不可用：" + p.Error
	case p.PublicOrigin == "":
		item.Info = "手机推送：需要 Named Tunnel"
	case p.Subscriptions == 0:
		item.Info = "手机推送：尚未有设备开启"
	default:
		item.Info = fmt.Sprintf("手机推送：%d 台设备已开启", p.Subscriptions)
	}
	item.Guide = pushGuide(p, snap.Status.URLs.Local)
	return item
}

const pushPhoneSteps = "在手机上开启推送：\n" +
	"1. 点击托盘“复制公网链接”，在手机浏览器中打开；\n" +
	"2. iPhone / iPad（iOS 16.4 以上）：在 Safari 中点“分享 → 添加到主屏幕”，从主屏幕图标打开 WebMux 并输入 Token；\n" +
	"3. 进入 设置 → 通知推送，打开开关。"

func pushGuide(p *contract.PushStatus, local string) string {
	var b strings.Builder
	if !p.Enabled {
		b.WriteString("推送已在配置中停用：在 WebMux 配置文件的 [push] 中设置 enabled = true，再通过托盘重启 WebMux。\n\n")
	}
	if p.PublicOrigin == "" {
		target := "WebMux 本地地址"
		if local != "" {
			target += "（" + local + "）"
		}
		b.WriteString("Quick Tunnel 的地址每次启动都会变化，旧订阅点开即失效，无法用于手机推送。请先配置 Cloudflare Named Tunnel：\n" +
			"1. 在 Cloudflare 控制台创建 Tunnel，把公共主机名指向" + target + "；\n" +
			"2. 在 WebMux 配置文件的 [tunnel] 中写入 token（tunnel.token）与 hostname（tunnel.hostname）；\n" +
			"3. 通过托盘重启 WebMux，再开启“公网隧道（Cloudflare）”。\n\n")
	} else {
		b.WriteString("公网地址：" + p.PublicOrigin + "\n\n")
	}
	b.WriteString(pushPhoneSteps)
	return b.String()
}

// TunnelWarning is the confirmation shown before every enable.
const TunnelWarning = "开启公网隧道后，WebMux 会通过 Cloudflare 暴露到公网。\n\n任何获得链接和 Token 的人都能以你的身份执行命令。Quick Tunnel 没有额外的身份验证，Token 是唯一的屏障。\n\n确定开启？"
