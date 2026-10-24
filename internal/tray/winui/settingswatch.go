//go:build windows

package winui

import (
	"os"
	"reflect"
	"slices"
	"time"

	"github.com/rainchan/webmux/internal/tray"
)

// fileStamp detects settings file changes.
type fileStamp struct {
	exists bool
	mod    time.Time
	size   int64
}

func statFile(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{exists: true, mod: info.ModTime(), size: info.Size()}
}

// watchSettings reloads tray.json when it changes. An invalid file keeps
// the tray running with defaults and is never rewritten until a menu
// action saves settings.
func (a *App) watchSettings() {
	ticker := time.NewTicker(settingsPoll)
	defer ticker.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
		}
		stamp := statFile(a.o.SettingsPath)
		a.mu.Lock()
		changed := stamp != a.stamp
		a.stamp = stamp
		a.mu.Unlock()
		if !changed {
			continue
		}
		settings, err := tray.LoadSettings(a.o.SettingsPath)
		a.mu.Lock()
		if err != nil {
			a.log.Printf("tray settings: %v", err)
			a.settingsWarn = invalidSetting
		} else {
			a.settingsWarn = ""
		}
		a.mu.Unlock()
		a.log.Printf("tray settings reloaded")
		a.apply(settings, true)
		a.kickUI()
	}
}

func cloneSettings(s tray.Settings) tray.Settings {
	s.Forward = slices.Clone(s.Forward)
	if s.Forward == nil {
		s.Forward = []string{}
	}
	return s
}

// apply makes next the effective settings. A distribution or mode change
// that stops a managed running instance needs confirmation when sessions
// exist; declining from the menu cancels, declining a file change keeps
// the current distribution and mode. It reports whether next applied.
func (a *App) apply(next tray.Settings, fromFile bool) bool {
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	a.mu.Lock()
	old := cloneSettings(a.settings)
	a.mu.Unlock()
	next = cloneSettings(next)
	if reflect.DeepEqual(old, next) {
		return true
	}
	snap := a.snapshot()
	if a.sup != nil && (old.Distro != next.Distro || old.Mode != next.Mode) && snap.Actions().Stop {
		prefix := "切换启动方式需要停止 WebMux，"
		if old.Distro != next.Distro {
			prefix = "切换发行版需要停止 WebMux，"
		}
		if !confirmSessions(prefix, snap.Sessions) {
			if !fromFile {
				return false
			}
			a.log.Printf("kept distribution and mode: the user declined to stop WebMux")
			next.Distro, next.Mode = old.Distro, old.Mode
		}
	}
	a.mu.Lock()
	a.settings = next
	if old.Distro != next.Distro {
		a.netMode, a.detected = "", false
	}
	a.mu.Unlock()
	if a.sup != nil {
		// The supervisor refreshes the new distribution at once unless it
		// first stops the instance running in the old one.
		now := (old.Distro != next.Distro && !snap.Actions().Stop) || idle(snap)
		a.svc.Retarget(next, now)
		a.sup.SetSettings(next)
	}
	if old.Distro != next.Distro {
		a.probe(true)
	}
	a.kickUI()
	return true
}

// changeSettings applies a menu change and saves it.
func (a *App) changeSettings(edit func(*tray.Settings)) {
	a.mu.Lock()
	next := cloneSettings(a.settings)
	a.mu.Unlock()
	edit(&next)
	if !a.apply(next, false) {
		return
	}
	if err := tray.SaveSettings(a.o.SettingsPath, next); err != nil {
		a.log.Printf("save tray settings: %v", err)
		a.notifier.Show("保存托盘设置失败", err.Error())
		return
	}
	a.mu.Lock()
	a.stamp = statFile(a.o.SettingsPath)
	a.settingsWarn = ""
	a.mu.Unlock()
	a.kickUI()
}
