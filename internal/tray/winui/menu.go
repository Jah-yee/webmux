//go:build windows

package winui

import (
	"fmt"
	"slices"
	"strings"

	"fyne.io/systray"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tray"
	"github.com/rainchan/webmux/internal/wslclient"
)

// Pool sizes of the dynamic submenus. fyne.io/systray cannot rebuild a menu
// cheaply, so every dynamic entry is pre-created and shown or hidden.
const (
	remotePool    = 8
	forwardPool   = 8
	infoLinkPool  = 8
	candidatePool = 12
	distroPool    = 16
)

// item caches what fyne.io/systray shows, because updating a hidden item
// would insert it again.
type item struct {
	mi      *systray.MenuItem
	title   string
	enabled bool
	checked bool
	shown   bool
}

func newItem(mi *systray.MenuItem, title string) *item {
	return &item{mi: mi, title: title, enabled: true, shown: true}
}

func (it *item) set(title string, enabled, checked bool) {
	if !it.shown {
		it.mi.Show()
		it.shown = true
	}
	if title != it.title {
		it.mi.SetTitle(title)
		it.title = title
	}
	if enabled != it.enabled {
		if enabled {
			it.mi.Enable()
		} else {
			it.mi.Disable()
		}
		it.enabled = enabled
	}
	if checked != it.checked {
		if checked {
			it.mi.Check()
		} else {
			it.mi.Uncheck()
		}
		it.checked = checked
	}
}

func (it *item) hide() {
	if it.shown {
		it.mi.Hide()
		it.shown = false
	}
}

// remoteLink is one entry of the remote links.
type remoteLink struct {
	URL, Label string
}

// candidate is one entry of the forwarding addresses.
type candidate struct {
	Address string
	Title   string
	Enabled bool
	Checked bool
}

// menu holds every item; only App.render changes them.
type menu struct {
	status1, status2, hint1, hint2 *item

	open, copyLocal, remoteWarn, copyRemote, remoteSub, remoteSubWarn *item
	remoteLinks, forwardLinks                                         []*item
	copyToken                                                         *item
	tunnel, tunnelInfo, copyPublic                                    *item

	remote, fwHeadline, fwBlocked, fwInit *item
	fwLinks, fwCandidates                 []*item

	start, stop, restart, install *item

	logs, logService, logTray *item
	openConfig, initConfig    *item
	distro, distroDefault     *item
	distros                   []*item
	mode, modeAuto, modeTray  *item
	modeSystemd               *item
	editSettings              *item
	autostart                 *item
	quit                      *item
}

// bindings are what the dynamic items currently stand for.
type bindings struct {
	remote     []remoteLink // the single item or the two pools, in order
	reported   int          // remote[:reported] fill remoteLinks
	candidates []candidate
	distros    []string
}

func add(title string) *item { return newItem(systray.AddMenuItem(title, ""), title) }

func addSub(parent *item, title string) *item {
	return newItem(parent.mi.AddSubMenuItem(title, ""), title)
}

func pool(parent *item, n int) []*item {
	items := make([]*item, n)
	for i := range items {
		items[i] = addSub(parent, "")
		items[i].hide()
	}
	return items
}

// buildMenu creates the items in display order: fyne.io/systray orders
// items by creation, so hidden pool items reappear in place.
func buildMenu() *menu {
	m := &menu{}
	m.status1 = add("WebMux")
	m.status2 = add("")
	m.hint1 = add("")
	m.hint2 = add("")
	for _, it := range []*item{m.status1, m.status2, m.hint1, m.hint2} {
		it.mi.Disable()
		it.enabled = false
	}
	m.hint1.hide()
	m.hint2.hide()
	systray.AddSeparator()

	m.open = add("打开 WebMux")
	m.copyLocal = add("复制本机链接")
	m.remoteWarn = add(exposedWarning)
	m.copyRemote = add("复制远程链接")
	m.remoteSub = add("复制远程链接")
	m.remoteSubWarn = addSub(m.remoteSub, exposedWarning)
	m.remoteLinks = pool(m.remoteSub, remotePool)
	m.forwardLinks = pool(m.remoteSub, forwardPool)
	m.remoteWarn.hide()
	m.copyRemote.hide()
	m.remoteSub.hide()
	m.copyToken = add("复制 Token")
	m.copyToken.hide()
	m.tunnel = add("公网隧道（Cloudflare）")
	m.tunnelInfo = add("")
	m.tunnelInfo.mi.Disable()
	m.tunnelInfo.enabled = false
	m.copyPublic = add("复制公网链接")
	for _, it := range []*item{m.tunnel, m.tunnelInfo, m.copyPublic} {
		it.hide()
	}
	m.remote = add("远程访问")
	m.fwHeadline = addSub(m.remote, "")
	m.fwBlocked = addSub(m.remote, "")
	m.fwInit = addSub(m.remote, "初始化配置（生成 Token）")
	m.fwLinks = pool(m.remote, infoLinkPool)
	m.remote.mi.AddSeparator()
	m.fwCandidates = pool(m.remote, candidatePool)
	systray.AddSeparator()

	m.start = add("启动")
	m.stop = add("停止")
	m.restart = add("重启")
	m.install = add("安装 WSL 中的 webmux")
	m.install.hide()
	systray.AddSeparator()

	m.logs = add("打开日志")
	m.logService = addSub(m.logs, "打开 WebMux 日志")
	m.logTray = addSub(m.logs, "打开托盘日志")
	m.openConfig = add("打开 WebMux 配置文件")
	m.initConfig = add("初始化配置（生成 Token）")
	m.initConfig.hide()
	m.distro = add("发行版")
	m.distroDefault = addSub(m.distro, "系统默认")
	m.distros = pool(m.distro, distroPool)
	m.mode = add("启动方式")
	m.modeAuto = addSub(m.mode, "自动")
	m.modeTray = addSub(m.mode, "托盘托管")
	m.modeSystemd = addSub(m.mode, "systemd 用户服务")
	m.editSettings = add("编辑托盘设置")
	systray.AddSeparator()

	m.autostart = add("登录时自动启动")
	systray.AddSeparator()

	m.quit = add("退出")
	return m
}

// exposedWarning is shown with remote links when WebMux reports the shell
// exposed without a token.
const exposedWarning = "⚠ 未设置 Token，任何能访问该端口的人都可获得 shell"

// exposed mirrors the server's exposure warning: remote links without a
// token. The prefix matches internal/client.ExposedWarning, which the tray
// cannot import.
func exposed(status contract.StatusV1) bool {
	for _, w := range status.Warnings {
		if strings.HasPrefix(w, "SHELL EXPOSED") {
			return true
		}
	}
	return !status.TokenConfigured && len(status.URLs.Remote) > 0
}

func kindLabel(kind string) string {
	switch kind {
	case "tailscale":
		return "Tailscale"
	case "lan":
		return "局域网"
	}
	return kind
}

func stateLabel(s tray.State) string {
	switch s {
	case tray.StateStarting:
		return "启动中"
	case tray.StateRunning:
		return "运行中"
	case tray.StateUnreachable:
		return "运行中（Windows 侧不可达）"
	case tray.StateExternal:
		return "运行中（外部实例）"
	case tray.StateStopping:
		return "停止中"
	case tray.StateRestarting:
		return "重启中"
	case tray.StateFailed:
		return "异常"
	}
	return "已停止"
}

func runningState(s tray.State) bool {
	return s == tray.StateRunning || s == tray.StateUnreachable || s == tray.StateExternal
}

// tooltip is the hover text; it never contains the token.
func tooltip(snap tray.Snapshot) string {
	text := "WebMux · " + stateLabel(snap.State)
	if runningState(snap.State) && snap.HasStatus && snap.Status.URLs.Local != "" {
		text += fmt.Sprintf(" · %d 个会话 · %s", snap.Sessions, snap.Status.URLs.Local)
	}
	return text
}

func iconFor(s tray.State) []byte {
	switch s {
	case tray.StateStarting, tray.StateStopping, tray.StateRestarting:
		return iconStarting
	case tray.StateRunning, tray.StateExternal:
		return iconRunning
	case tray.StateFailed, tray.StateUnreachable:
		return iconError
	}
	return iconStopped
}

// view is everything render needs besides the snapshot.
type view struct {
	settings     tray.Settings
	settingsWarn string
	noWSL        bool
	distros      []wslclient.Distro
	detection    tray.Detection
	detected     bool
	plan         tray.ForwardPlan
	forwarding   []string // active relay listen addresses
	relayState   func(addr string) (bool, string)
	autostart    bool
	// installSupported: the tray carries release server digests.
	installSupported bool
	installing       bool
}

func defaultDistro(distros []wslclient.Distro) string {
	for _, d := range distros {
		if d.Default {
			return d.Name
		}
	}
	return ""
}

func distroLabel(v view) string {
	if v.settings.Distro != "" {
		return v.settings.Distro
	}
	if name := defaultDistro(v.distros); name != "" {
		return "系统默认（" + name + "）"
	}
	return "系统默认"
}

// autoCurrent is the mode "自动" resolves to, or "".
func autoCurrent(snap tray.Snapshot, v view) string {
	if v.settings.Mode == tray.ModeAuto && (snap.Mode == tray.ModeTray || snap.Mode == tray.ModeSystemd) {
		return tray.ModeLabel(snap.Mode)
	}
	if v.detected {
		mode, _ := tray.Resolve(tray.ModeAuto, v.detection)
		return tray.ModeLabel(mode)
	}
	return ""
}

func modeLabel(snap tray.Snapshot, v view) string {
	if snap.Mode == tray.ModeTray || snap.Mode == tray.ModeSystemd {
		return tray.ModeLabel(snap.Mode)
	}
	if v.settings.Mode != tray.ModeAuto {
		return tray.ModeLabel(v.settings.Mode)
	}
	if current := autoCurrent(snap, v); current != "" {
		return "自动（当前：" + current + "）"
	}
	return "自动"
}

// systemdMode reports whether the service log is the journal.
func systemdMode(snap tray.Snapshot, v view) bool {
	switch {
	case snap.Mode == tray.ModeSystemd:
		return true
	case snap.Mode == tray.ModeTray:
		return false
	case v.settings.Mode == tray.ModeSystemd:
		return true
	case v.settings.Mode == tray.ModeAuto && v.detected:
		return v.detection.Reason() == ""
	}
	return false
}

// remoteLinks lists the reported remote links, then the active relays.
func remoteLinks(snap tray.Snapshot, v view) ([]remoteLink, int) {
	var links []remoteLink
	if snap.HasStatus {
		for _, r := range snap.Status.URLs.Remote {
			links = append(links, remoteLink{URL: r.URL, Label: fmt.Sprintf("%s（%s · %s）", r.URL, r.Interface, kindLabel(r.Kind))})
		}
	}
	reported := len(links)
	names := map[string]string{}
	for _, c := range v.plan.Candidates {
		names[c.Address] = c.Interface
	}
	for _, listen := range v.forwarding {
		host := listen
		if i := strings.LastIndexByte(listen, ':'); i >= 0 {
			host = listen[:i]
		}
		url := "http://" + listen
		label := url + "（经托盘转发）"
		if name := names[host]; name != "" {
			label = fmt.Sprintf("%s（%s · 经托盘转发）", url, name)
		}
		links = append(links, remoteLink{URL: url, Label: label})
	}
	return links, reported
}

// forwardCandidates lists the plan's addresses, then saved addresses that
// are currently missing so they can still be unchecked.
func forwardCandidates(v view) []candidate {
	usable := v.plan.Needed && v.plan.Blocked == ""
	var out []candidate
	seen := map[string]bool{}
	for _, c := range v.plan.Candidates {
		seen[c.Address] = true
		checked := slices.Contains(v.settings.Forward, c.Address)
		title := fmt.Sprintf("%s（%s · %s）", c.Address, kindLabel(c.Kind), c.Interface)
		if checked {
			active, failure := v.relayState(c.Address)
			switch {
			case failure != "":
				title += " — " + failure
			case active:
				title += " — 转发中"
			case !usable:
				title += " — 已暂停"
			}
		}
		out = append(out, candidate{Address: c.Address, Title: title, Enabled: usable, Checked: checked})
	}
	for _, addr := range v.settings.Forward {
		if !seen[addr] {
			out = append(out, candidate{Address: addr, Title: addr + " — 地址当前不可用", Enabled: usable, Checked: true})
		}
	}
	return out
}

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

	line := "状态：" + stateLabel(snap.State)
	if runningState(snap.State) {
		line += fmt.Sprintf(" · %d 个会话", snap.Sessions)
	}
	if snap.HasStatus && status.Version != "" {
		line += " · WebMux " + status.Version
	}
	m.status1.set(line, false, false)
	m.status2.set("发行版："+distroLabel(v)+" · 启动方式："+modeLabel(snap, v), false, false)
	hint := snap.Reason
	if upgrade := tray.UpgradeHint(snap); upgrade != "" {
		if hint != "" {
			hint += "；"
		}
		hint += upgrade
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

	serviceLog := !v.noWSL && (systemdMode(snap, v) || (snap.HasStatus && strings.HasPrefix(status.LogFile, "/")))
	m.logService.set("打开 WebMux 日志", serviceLog, false)
	switch {
	case snap.HasStatus && !status.Config.Exists:
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
