//go:build windows

package winui

import (
	"context"
	"fmt"
	"os"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tray"
)

// shutdown stops the supervisor (and WebMux when stop is set), forwarding
// and pending clipboard secrets. It runs once.
func (a *App) shutdown(stop bool) {
	a.quitOnce.Do(func() {
		a.log.Printf("tray exiting (stop WebMux: %v)", stop)
		if a.sup != nil {
			a.sup.Quit(stop)
		}
		close(a.done)
		a.relays.closeAll()
		a.svc.Close()
		a.clip.Flush()
		a.mu.Lock()
		if a.journal != "" {
			_ = os.Remove(a.journal)
		}
		a.mu.Unlock()
	})
}

func (a *App) quit() {
	stop := false
	if a.sup != nil {
		if snap := a.sup.Snapshot(); snap.Managed() {
			switch askQuit(snap.Sessions, a.relays.any()) {
			case quitCancel:
				return
			case quitStop:
				stop = true
			}
		}
	}
	a.shutdown(stop)
	systray.Quit()
}

func (a *App) fail(title string, err error) {
	a.log.Printf("%s: %v", title, err)
	a.notifier.Show(title, err.Error())
}

// targetDistro is the distribution name of the instance being managed, for
// \\wsl.localhost paths.
func (a *App) targetDistro() string {
	if d := a.svc.Current().Target.Distro; d != "" {
		return d
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return defaultDistro(a.distros)
}

func (a *App) initConfig() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := a.svc.Current().ConfigInit(ctx); err != nil {
		a.fail("初始化配置失败", err)
		return
	}
	a.log.Printf("webmux config init succeeded")
	a.notifier.Show("WebMux", "已生成配置与 Token，重启 WebMux 后生效")
	if a.sup == nil {
		return
	}
	a.sup.Refresh()
	snap := a.sup.Snapshot()
	if !snap.Actions().Restart {
		return
	}
	text := "已生成配置与 Token，重启 WebMux 后生效。\n\n现在重启 WebMux 吗？"
	if snap.Sessions > 0 {
		text += fmt.Sprintf("重启将结束 %d 个终端会话，且不可恢复。", snap.Sessions)
	}
	if confirm(text) {
		a.sup.Restart()
	}
}

// installWebmux downloads and installs the release server into WSL, then
// rechecks; a running server keeps its old binary until restarted.
func (a *App) installWebmux() {
	if !a.installing.CompareAndSwap(false, true) {
		return
	}
	a.kickUI()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	err := a.svc.Current().Install(ctx)
	cancel()
	a.installing.Store(false)
	a.kickUI()
	if err != nil {
		a.fail("安装 webmux 失败", err)
		return
	}
	a.notifier.Show("WebMux", "已安装 webmux "+a.o.Version)
	a.sup.Recheck()
	snap := a.sup.Snapshot()
	if !snap.HasStatus || snap.Status.State != contract.StateRunning {
		return
	}
	text := "新版本在重启 WebMux 后生效，重启会结束所有终端会话。"
	if !snap.Actions().Restart {
		messageBox(text, windows.MB_OK|windows.MB_ICONINFORMATION)
		return
	}
	if snap.Sessions > 0 {
		text += fmt.Sprintf("\n\n当前有 %d 个终端会话。", snap.Sessions)
	}
	if confirm(text + "\n\n现在重启 WebMux 吗？") {
		a.sup.Restart()
	}
}

// openRelease opens the release page of the tray matching a newer server.
// The tray never updates itself.
func (a *App) openRelease() {
	if download := tray.DownloadEntry(a.snapshot()); download.Show {
		if err := OpenURL(download.URL); err != nil {
			a.fail("无法打开浏览器", err)
		}
	}
}

func (a *App) stopWebMux() {
	snap := a.sup.Snapshot()
	if confirmSessions("停止 WebMux ", snap.Sessions) {
		a.sup.Stop()
	}
}

func (a *App) restartWebMux() {
	snap := a.sup.Snapshot()
	if confirmSessions("重启 WebMux ", snap.Sessions) {
		a.sup.Restart()
	}
}
