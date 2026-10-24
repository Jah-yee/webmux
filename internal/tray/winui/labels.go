//go:build windows

package winui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tray"
	"github.com/rainchan/webmux/internal/wslclient"
)

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

// tooltip is the hover text; it never contains the token.
func tooltip(snap tray.Snapshot) string {
	text := "WebMux · " + tray.StateLabel(snap)
	if tray.RunningState(snap.State) && snap.HasStatus && snap.Status.URLs.Local != "" {
		if !snap.Degraded {
			text += fmt.Sprintf(" · %d 个会话", snap.Sessions)
		}
		text += " · " + snap.Status.URLs.Local
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
