//go:build windows

package winui

import (
	"golang.org/x/sys/windows"

	"github.com/rainchan/webmux/internal/tray"
)

func (a *App) bindings() bindings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bound
}

// on runs f for each click of it. Clicks arriving while f runs (a dialog
// is open, for example) are dropped by fyne.io/systray.
func on(it *item, f func()) {
	go func() {
		for range it.mi.ClickedCh {
			f()
		}
	}()
}

// listen connects the menu items to their actions.
func (a *App) listen() {
	m := a.menu
	on(m.logService, a.openServiceLog)
	on(m.logTray, func() {
		if err := OpenInNotepad(a.log.Path()); err != nil {
			a.fail("无法打开托盘日志", err)
		}
	})
	on(m.quit, a.quit)
	if a.sup == nil {
		return
	}
	on(m.open, a.openLocal)
	on(m.copyLocal, func() { a.copyLink(a.snapshot().Status.URLs.Local) })
	on(m.copyRemote, func() {
		if b := a.bindings(); len(b.remote) > 0 {
			a.copyLink(b.remote[0].URL)
		}
	})
	for i, it := range m.remoteLinks {
		on(it, func() {
			if b := a.bindings(); i < b.reported {
				a.copyLink(b.remote[i].URL)
			}
		})
	}
	for i, it := range m.forwardLinks {
		on(it, func() {
			if b := a.bindings(); b.reported+i < len(b.remote) {
				a.copyLink(b.remote[b.reported+i].URL)
			}
		})
	}
	on(m.copyToken, a.copyToken)
	on(m.tunnel, a.toggleTunnel)
	on(m.copyPublic, func() {
		a.copyLink(tray.TunnelEntry(a.snapshot()).PublicURL)
	})
	on(m.pushGuide, func() {
		if guide := tray.PushEntry(a.snapshot()).Guide; guide != "" {
			messageBox(guide, windows.MB_OK|windows.MB_ICONINFORMATION)
		}
	})
	on(m.install, a.installWebmux)
	on(m.download, a.openRelease)
	on(m.fwInit, a.initConfig)
	for i, it := range m.fwCandidates {
		on(it, func() {
			if b := a.bindings(); i < len(b.candidates) {
				a.toggleForward(b.candidates[i].Address)
			}
		})
	}
	on(m.start, a.sup.Start)
	on(m.stop, a.stopWebMux)
	on(m.restart, a.restartWebMux)
	on(m.openConfig, a.openConfig)
	on(m.initConfig, a.initConfig)
	on(m.distroDefault, func() { a.changeSettings(func(s *tray.Settings) { s.Distro = "" }) })
	for i, it := range m.distros {
		on(it, func() {
			if b := a.bindings(); i < len(b.distros) {
				name := b.distros[i]
				a.changeSettings(func(s *tray.Settings) { s.Distro = name })
			}
		})
	}
	on(m.modeAuto, func() { a.changeSettings(func(s *tray.Settings) { s.Mode = tray.ModeAuto }) })
	on(m.modeTray, func() { a.changeSettings(func(s *tray.Settings) { s.Mode = tray.ModeTray }) })
	on(m.modeSystemd, func() { a.changeSettings(func(s *tray.Settings) { s.Mode = tray.ModeSystemd }) })
	on(m.editSettings, a.editSettings)
	on(m.autostart, a.toggleAutostart)
}
