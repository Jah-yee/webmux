//go:build windows

package winui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rainchan/webmux/internal/tray"
)

// render applies a snapshot to the menu and returns the new bindings.
func (m *menu) render(snap tray.Snapshot, v view) bindings {
	var b bindings
	acts := snap.Actions()
	if v.noWSL {
		acts = tray.Actions{}
	}
	status := snap.Status
	local := ""
	if snap.HasStatus {
		local = status.URLs.Local
	}
	busy := snap.State == tray.StateStarting || snap.State == tray.StateStopping

	m.status1.set(tray.StatusLine(snap), false, false)
	m.status2.set("发行版："+distroLabel(v)+" · 启动方式："+modeLabel(snap, v), false, false)
	hint := snap.Reason
	if version := tray.VersionHint(snap); version != "" {
		if hint != "" {
			hint += "；"
		}
		hint += version
	}
	if hint != "" {
		m.hint1.set(hint, false, false)
	} else {
		m.hint1.hide()
	}
	if v.settingsWarn != "" {
		m.hint2.set(v.settingsWarn, false, false)
	} else {
		m.hint2.hide()
	}

	access := acts.Access && local != ""
	m.open.set("打开 WebMux", access, false)
	m.copyLocal.set("复制本机链接", access, false)
	b.remote, b.reported = remoteLinks(snap, v)
	warn := snap.HasStatus && exposed(status)
	switch {
	case len(b.remote) == 0:
		m.remoteWarn.hide()
		m.copyRemote.hide()
		m.remoteSub.hide()
	case len(b.remote) == 1:
		if warn {
			m.remoteWarn.set(exposedWarning, false, false)
		} else {
			m.remoteWarn.hide()
		}
		m.copyRemote.set("复制远程链接", acts.Access, false)
		m.remoteSub.hide()
	default:
		m.remoteWarn.hide()
		m.copyRemote.hide()
		m.remoteSub.set("复制远程链接", acts.Access, false)
		if warn {
			m.remoteSubWarn.set(exposedWarning, false, false)
		} else {
			m.remoteSubWarn.hide()
		}
		for i, it := range m.remoteLinks {
			if i < b.reported {
				it.set(b.remote[i].Label, true, false)
			} else {
				it.hide()
			}
		}
		forwarded := b.remote[b.reported:]
		for i, it := range m.forwardLinks {
			if i < len(forwarded) {
				it.set(forwarded[i].Label, true, false)
			} else {
				it.hide()
			}
		}
	}
	if snap.HasStatus && status.TokenConfigured {
		m.copyToken.set("复制 Token", acts.Access, false)
	} else {
		m.copyToken.hide()
	}
	tunnel := tray.TunnelEntry(snap)
	if tunnel.Show && !v.noWSL {
		m.tunnel.set("公网隧道（Cloudflare）", !busy, tunnel.Checked)
		if tunnel.Info != "" {
			m.tunnelInfo.set(tunnel.Info, false, false)
		} else {
			m.tunnelInfo.hide()
		}
		if tunnel.PublicURL != "" {
			m.copyPublic.set("复制公网链接", true, false)
		} else {
			m.copyPublic.hide()
		}
	} else {
		m.tunnel.hide()
		m.tunnelInfo.hide()
		m.copyPublic.hide()
	}
	if push := tray.PushEntry(snap); push.Show && !v.noWSL {
		m.pushInfo.set(push.Info, false, false)
		m.pushGuide.set("手机推送设置说明…", true, false)
	} else {
		m.pushInfo.hide()
		m.pushGuide.hide()
	}

	m.remote.set("远程访问", !v.noWSL, false)
	headline := v.plan.Headline
	if headline == "" {
		headline = "正在检测网络模式"
	}
	m.fwHeadline.set(headline, false, false)
	if v.plan.Blocked != "" {
		m.fwBlocked.set(v.plan.Blocked, false, false)
	} else {
		m.fwBlocked.hide()
	}
	if v.plan.OfferInit && !v.noWSL {
		m.fwInit.set("初始化配置（生成 Token）", true, false)
	} else {
		m.fwInit.hide()
	}
	var info []string
	if !v.plan.Needed && snap.HasStatus {
		for _, r := range status.URLs.Remote {
			info = append(info, fmt.Sprintf("%s（%s · %s）", r.URL, r.Interface, kindLabel(r.Kind)))
		}
	}
	for i, it := range m.fwLinks {
		if i < len(info) {
			it.set(info[i], false, false)
		} else {
			it.hide()
		}
	}
	b.candidates = forwardCandidates(v)
	if len(b.candidates) > len(m.fwCandidates) {
		b.candidates = b.candidates[:len(m.fwCandidates)]
	}
	for i, it := range m.fwCandidates {
		if i < len(b.candidates) {
			c := b.candidates[i]
			it.set(c.Title, c.Enabled && !v.noWSL, c.Checked)
		} else {
			it.hide()
		}
	}

	m.start.set("启动", acts.Start, false)
	m.stop.set("停止", acts.Stop, false)
	m.restart.set("重启", acts.Restart, false)
	if install := tray.InstallEntry(snap, v.installSupported, v.installing); install.Show && !v.noWSL {
		m.install.set(install.Title, install.Enabled, false)
	} else {
		m.install.hide()
	}
	if download := tray.DownloadEntry(snap); download.Show && !v.noWSL {
		m.download.set(download.Title, true, false)
	} else {
		m.download.hide()
	}

	serviceLog := !v.noWSL && (systemdMode(snap, v) || (snap.HasStatus && strings.HasPrefix(status.LogFile, "/")))
	m.logService.set("打开 WebMux 日志", serviceLog, false)
	switch {
	case snap.HasStatus && !snap.Degraded && !status.Config.Exists:
		m.openConfig.hide()
		m.initConfig.set("初始化配置（生成 Token）", !v.noWSL, false)
	default:
		m.initConfig.hide()
		m.openConfig.set("打开 WebMux 配置文件", !v.noWSL && snap.HasStatus && strings.HasPrefix(status.Config.Path, "/"), false)
	}

	selectable := !v.noWSL && !busy
	m.distro.set("发行版", selectable, false)
	name := defaultDistro(v.distros)
	defaultTitle := "系统默认"
	if name != "" {
		defaultTitle += "（" + name + "）"
	}
	m.distroDefault.set(defaultTitle, true, v.settings.Distro == "")
	for _, d := range v.distros {
		b.distros = append(b.distros, d.Name)
	}
	if v.settings.Distro != "" && !slices.Contains(b.distros, v.settings.Distro) {
		b.distros = append(b.distros, v.settings.Distro)
	}
	if len(b.distros) > len(m.distros) {
		b.distros = b.distros[:len(m.distros)]
	}
	for i, it := range m.distros {
		if i < len(b.distros) {
			it.set(b.distros[i], true, b.distros[i] == v.settings.Distro)
		} else {
			it.hide()
		}
	}

	m.mode.set("启动方式", selectable, false)
	autoTitle := "自动"
	if current := autoCurrent(snap, v); current != "" {
		autoTitle = "自动（当前：" + current + "）"
	}
	m.modeAuto.set(autoTitle, true, v.settings.Mode == tray.ModeAuto)
	m.modeTray.set("托盘托管", true, v.settings.Mode == tray.ModeTray)
	systemdTitle, systemdOK := "systemd 用户服务", true
	if v.detected {
		if reason := v.detection.Reason(); reason != "" {
			systemdTitle += "（" + reason + "）"
			systemdOK = false
		}
	}
	m.modeSystemd.set(systemdTitle, systemdOK, v.settings.Mode == tray.ModeSystemd)

	m.editSettings.set("编辑托盘设置", !v.noWSL, false)
	m.autostart.set("登录时自动启动", !v.noWSL, v.autostart)
	m.quit.set("退出", true, false)
	return b
}
