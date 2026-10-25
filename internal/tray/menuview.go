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
	case snap.Blocker == BlockerIncompatible, snap.Compat == CompatIncompatibleOlder:
		title = "升级 WSL 中的 webmux"
	case snap.Compat == CompatServerOlder:
		title = "升级 WSL 中的 webmux（" + snap.Installed.Version + " → " + snap.Tray.Version + "）"
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

// DownloadItem describes the entry opening a newer tray's release page.
type DownloadItem struct {
	Show       bool
	Title, URL string
}

// DownloadEntry offers the tray matching a newer installed server; only
// release versions have a release page.
func DownloadEntry(snap Snapshot) DownloadItem {
	version := snap.Installed.Version
	if !snap.Compat.Newer() || !releaseVersion(version) {
		return DownloadItem{}
	}
	return DownloadItem{Show: true, Title: "下载托盘 " + version, URL: ReleasePage(version)}
}

// ReleasePage is the GitHub release page of a version.
func ReleasePage(version string) string {
	return "https://github.com/" + ReleaseRepo + "/releases/tag/v" + version
}

// VersionHint is the status line of the version check: the older side when
// both are compatible but differ, the direction when incompatible, and a
// running instance that differs from the installed file. It is "" when
// they match or cannot be compared.
func VersionHint(snap Snapshot) string {
	var hints []string
	installed := snap.Installed.Version
	wsl := "WSL 中的 webmux"
	if installed != "" {
		wsl += " " + installed
	}
	switch snap.Compat {
	case CompatServerOlder:
		hints = append(hints, "托盘 "+snap.Tray.Version+" · WSL "+installed+"（WSL 较旧）")
	case CompatServerNewer:
		hints = append(hints, "托盘 "+snap.Tray.Version+" · WSL "+installed+"（托盘较旧）")
	case CompatIncompatibleOlder:
		hints = append(hints, "WSL 中的 webmux 版本过旧，请升级")
	case CompatIncompatibleNewer:
		hints = append(hints, wsl+" 比托盘新，请升级托盘")
	}
	if r := snap.Running; r != nil && snap.Compat != "" && installed != "" && r.Version != installed {
		hints = append(hints, "WSL 中已安装 "+installed+"，重启 WebMux 后生效，重启会结束所有会话")
	}
	return strings.Join(hints, "；")
}

// RunningState reports the states with a running instance.
func RunningState(s State) bool {
	return s == StateRunning || s == StateUnreachable || s == StateExternal
}

// StateLabel names the state; a degraded instance is marked limited.
func StateLabel(snap Snapshot) string {
	switch snap.State {
	case StateStarting:
		return "启动中"
	case StateRunning:
		if snap.Degraded {
			return "运行中（不兼容，功能受限）"
		}
		return "运行中"
	case StateUnreachable:
		return "运行中（Windows 侧不可达）"
	case StateExternal:
		return "运行中（外部实例）"
	case StateStopping:
		return "停止中"
	case StateRestarting:
		return "重启中"
	case StateFailed:
		return "异常"
	}
	return "已停止"
}

// StatusLine is the first menu line. A degraded instance reports no
// session count.
func StatusLine(snap Snapshot) string {
	line := "状态：" + StateLabel(snap)
	if RunningState(snap.State) {
		if snap.Degraded {
			line += " · 会话数未知"
		} else {
			line += fmt.Sprintf(" · %d 个会话", snap.Sessions)
		}
	}
	if snap.HasStatus && snap.Status.Version != "" {
		line += " · WebMux " + snap.Status.Version
	}
	return line
}

// TunnelItem describes the public tunnel entries.
type TunnelItem struct {
	// Show is false when no instance runs or it cannot control its tunnel.
	Show, Checked bool
	SettingsURL   string
	// Info is the state line; PublicURL is set only when ready.
	Info, PublicURL string
}

// TunnelEntry derives the tunnel entries from the running instance.
func TunnelEntry(snap Snapshot) TunnelItem {
	t := snap.Status.Tunnel
	if !snap.HasStatus || snap.Status.State != contract.StateRunning || t == nil || !snap.Actions().Access {
		return TunnelItem{}
	}
	item := TunnelItem{Show: true, Checked: snap.TunnelEnabled()}
	if t.SettingsAvailable && snap.Status.URLs.Local != "" {
		item.SettingsURL = strings.TrimRight(snap.Status.URLs.Local, "/") + "/settings/tunnel"
	}
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
	item.Guide = pushGuide(p, snap.Status.URLs.Local, snap.Status.Tunnel.SettingsAvailable)
	return item
}

const pushPhoneSteps = "在手机上开启推送：\n" +
	"1. 点击托盘“复制公网链接”，在手机浏览器中打开；\n" +
	"2. iPhone / iPad（iOS 16.4 以上）：在 Safari 中点“分享 → 添加到主屏幕”，从主屏幕图标打开 WebMux 并输入 Token；\n" +
	"3. 进入 设置 → 通知推送，打开开关。"

func pushGuide(p *contract.PushStatus, local string, webSettings bool) string {
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
			"1. 在 Cloudflare 控制台创建 Tunnel，把公共主机名指向" + target + "；\n")
		if webSettings {
			b.WriteString("2. 在 WebMux 设置 → 公网访问中选择 Named Tunnel，填写隧道凭据与 hostname；\n" +
				"3. 保存并启用公网访问，无需重启 WebMux。\n\n")
		} else {
			b.WriteString("2. 当前服务端尚不支持网页公网访问设置，请升级服务端，或在配置文件的 [tunnel] 中写入 token（tunnel.token）与 hostname（tunnel.hostname）；\n" +
				"3. 修改旧版服务端配置后重启 WebMux，再开启“公网隧道（Cloudflare）”。\n\n")
		}
	} else {
		b.WriteString("公网地址：" + p.PublicOrigin + "\n\n")
	}
	b.WriteString(pushPhoneSteps)
	return b.String()
}

// TunnelWarning is the confirmation shown before every enable.
const TunnelWarning = "开启公网隧道后，WebMux 会通过 Cloudflare 暴露到公网。\n\n任何获得链接和 Token 的人都能以你的身份执行命令。Quick Tunnel 没有额外的身份验证，Token 是唯一的屏障。\n\n确定开启？"
