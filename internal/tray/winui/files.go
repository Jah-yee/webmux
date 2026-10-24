//go:build windows

package winui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rainchan/webmux/internal/tray"
	"github.com/rainchan/webmux/internal/wslclient"
)

func (a *App) openServiceLog() {
	snap := a.snapshot()
	a.mu.Lock()
	v := view{settings: a.settings, detection: a.detection, detected: a.detected}
	a.mu.Unlock()
	if systemdMode(snap, v) {
		a.openJournal()
		return
	}
	path, err := WSLPath(a.targetDistro(), snap.Status.LogFile)
	if err == nil {
		err = OpenInNotepad(path)
	}
	if err != nil {
		a.fail("无法打开 WebMux 日志", err)
	}
}

// openJournal exports the unit's recent journal to a temporary file.
func (a *App) openJournal() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inv := a.svc.Current().Target.Journal()
	res, err := a.o.Runner.Run(ctx, inv.Argv, inv.Env)
	if err == nil && res.Code != 0 {
		err = fmt.Errorf("journalctl: exit %d: %s", res.Code, wslclient.DecodeOutput(res.Stderr))
	}
	if err != nil {
		a.fail("无法读取 systemd 日志", err)
		return
	}
	f, err := os.CreateTemp("", "webmux-journal-*.log")
	if err != nil {
		a.fail("无法读取 systemd 日志", err)
		return
	}
	_, err = f.WriteString(wslclient.DecodeOutput(res.Stdout))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		a.fail("无法读取 systemd 日志", err)
		return
	}
	a.mu.Lock()
	if a.journal != "" {
		_ = os.Remove(a.journal)
	}
	a.journal = f.Name()
	a.mu.Unlock()
	if err := OpenInNotepad(f.Name()); err != nil {
		a.fail("无法打开 systemd 日志", err)
	}
}

func (a *App) openConfig() {
	snap := a.snapshot()
	path, err := WSLPath(a.targetDistro(), snap.Status.Config.Path)
	if err == nil {
		err = OpenInNotepad(path)
	}
	if err != nil {
		a.fail("无法打开 WebMux 配置文件", err)
	}
}

// editSettings opens tray.json, creating it with defaults only if missing.
func (a *App) editSettings() {
	path := a.o.SettingsPath
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := tray.SaveSettings(path, tray.DefaultSettings()); err != nil {
			a.fail("无法创建托盘设置", err)
			return
		}
		a.mu.Lock()
		a.stamp = statFile(path)
		a.mu.Unlock()
	}
	if err := OpenInNotepad(path); err != nil {
		a.fail("无法打开托盘设置", err)
	}
}

func (a *App) toggleAutostart() {
	on := !AutostartEnabled(a.o.Executable)
	if err := SetAutostart(on, a.o.Executable); err != nil {
		a.fail("无法修改登录启动项", err)
	}
	a.kickUI()
}
