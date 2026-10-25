//go:build windows

package winui

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tray"
)

var errNotRunning = errors.New("WebMux 未运行")

func (a *App) openLocal() {
	snap := a.snapshot()
	if !snap.Actions().Access || !snap.HasStatus || snap.Status.URLs.Local == "" {
		a.notifier.Show("WebMux", tooltip(snap))
		return
	}
	if err := OpenURL(snap.Status.URLs.Local); err != nil {
		a.fail("无法打开浏览器", err)
	}
}

func (a *App) openTunnelSettings() {
	if url := tray.TunnelEntry(a.snapshot()).SettingsURL; url != "" {
		if err := OpenURL(url); err != nil {
			a.fail("无法打开公网访问设置", err)
		}
	}
}

// copyLink copies url with the token fragment, so the link opens signed
// in. The token is fetched for this click only and the clipboard entry is
// handled like "复制 Token": kept out of history and cleared after 60 s.
func (a *App) copyLink(url string) {
	if url == "" {
		a.fail("复制失败", errNotRunning)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token, err := a.svc.Current().Token(ctx)
	if err != nil {
		a.fail("复制失败", err)
		return
	}
	if token == "" {
		if err := a.clip.Write(url, false); err != nil {
			a.fail("复制失败", err)
			return
		}
		a.notifier.Show("WebMux", "已复制 "+url)
		return
	}
	release := a.log.Redact(token)
	err = a.clip.Write(contract.AccessURL(url, token), true)
	release()
	if err != nil {
		a.fail("复制失败", err)
		return
	}
	a.notifier.Show("WebMux", "已复制 "+url+"（含 Token，60 秒后自动清除）")
}

// copyToken fetches the token for this click only; it is redacted from the
// log while held and never cached, shown or written to disk.
func (a *App) copyToken() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token, err := a.svc.Current().Token(ctx)
	if err != nil {
		a.fail("复制 Token 失败", err)
		return
	}
	release := a.log.Redact(token)
	err = a.clip.Write(token, true)
	release()
	if err != nil {
		a.fail("复制 Token 失败", err)
		return
	}
	a.notifier.Show("WebMux", "已复制 Token（60 秒后自动清除）")
}

// toggleTunnel turns the public tunnel off, or on after the risk is
// confirmed. The server owns persistence; the tray never stores this desire.
func (a *App) toggleTunnel() {
	enable := !a.snapshot().TunnelEnabled()
	if enable && !confirm(tray.TunnelWarning) {
		a.kickUI()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	_, err := a.svc.Current().SetTunnel(ctx, enable)
	cancel()
	a.sup.Refresh()
	if err == nil {
		return
	}
	if !enable {
		a.fail("关闭公网隧道失败", err)
		return
	}
	snap := a.snapshot()
	if tray.TunnelEntry(snap).SettingsURL != "" {
		messageBox("开启公网隧道失败："+err.Error()+"\n\n请打开“公网访问设置”查看状态。WebMux 登录 Token 需要至少 43 个字符，登录 Token 仍由配置文件或配置命令管理。", 0)
		return
	}
	a.fail("开启公网隧道失败", err)
}

func (a *App) toggleForward(addr string) {
	a.changeSettings(func(s *tray.Settings) {
		if i := slices.Index(s.Forward, addr); i >= 0 {
			s.Forward = slices.Delete(s.Forward, i, i+1)
		} else {
			s.Forward = append(s.Forward, addr)
		}
	})
}
