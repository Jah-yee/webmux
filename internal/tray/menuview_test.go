package tray

import (
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/contract"
)

func TestInstallEntry(t *testing.T) {
	missing := Snapshot{State: StateFailed, Blocker: BlockerMissing}
	tray := BuildInfo{Version: "0.2.0", Contract: 1}
	older := Snapshot{State: StateRunning, Tray: tray, Installed: BuildInfo{Version: "0.1.0", Contract: 1}, Compat: CompatServerOlder}
	for _, tc := range []struct {
		name               string
		snap               Snapshot
		supported, running bool
		show, enabled      bool
		title              string
	}{
		{"usable", Snapshot{State: StateStopped, Tray: tray, Installed: tray, Compat: CompatMatched}, true, false, false, false, ""},
		{"newer", Snapshot{State: StateStopped, Tray: tray, Installed: BuildInfo{Version: "0.3.0", Contract: 1}, Compat: CompatServerNewer}, true, false, false, false, ""},
		{"missing", missing, true, false, true, true, "安装 WSL 中的 webmux"},
		{"no local API", Snapshot{State: StateFailed, Blocker: BlockerIncompatible}, true, false, true, true, "升级 WSL 中的 webmux"},
		{"contract too old", Snapshot{State: StateStopped, Compat: CompatIncompatibleOlder}, true, false, true, true, "升级 WSL 中的 webmux"},
		{"older while running", older, true, false, true, true, "升级 WSL 中的 webmux（0.1.0 → 0.2.0）"},
		{"development tray", missing, false, false, true, false, ErrInstallUnsupported.Error()},
		{"installing", missing, true, true, true, false, "正在安装"},
		{"while starting", Snapshot{State: StateStarting, Tray: tray, Installed: older.Installed, Compat: CompatServerOlder}, true, false, true, false, "升级"},
	} {
		got := InstallEntry(tc.snap, tc.supported, tc.running)
		if got.Show != tc.show || got.Enabled != tc.enabled || !strings.Contains(got.Title, tc.title) {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
}

// Every classification yields its status line and update entry; none of
// them is a notification, so the menu is the only surface.
func TestVersionHintAndDownloadEntry(t *testing.T) {
	tray := BuildInfo{Version: "0.3.0", Contract: 1}
	snap := func(installed BuildInfo) Snapshot {
		return Snapshot{State: StateStopped, Tray: tray, Installed: installed, Compat: Classify(tray, installed)}
	}
	for _, tc := range []struct {
		name     string
		snap     Snapshot
		hint     string
		download string
		install  bool
	}{
		{"matched", snap(tray), "", "", false},
		{"dev", snap(BuildInfo{Version: "dev", Contract: 1}), "", "", false},
		{"WSL older", snap(BuildInfo{Version: "0.2.0", Contract: 1}), "托盘 0.3.0 · WSL 0.2.0（WSL 较旧）", "", true},
		{"WSL newer", snap(BuildInfo{Version: "0.4.0", Contract: 1}), "托盘 0.3.0 · WSL 0.4.0（托盘较旧）", "0.4.0", false},
		{"contract newer", snap(BuildInfo{Version: "0.5.0", Contract: 2}), "WSL 中的 webmux 0.5.0 比托盘新，请升级托盘", "0.5.0", false},
		{"contract newer dev", snap(BuildInfo{Version: "dev", Contract: 2}), "WSL 中的 webmux dev 比托盘新，请升级托盘", "", false},
		{"contract older", snap(BuildInfo{Version: "0.0.1", Contract: 0}), "WSL 中的 webmux 版本过旧，请升级", "", true},
	} {
		if got := VersionHint(tc.snap); got != tc.hint {
			t.Errorf("%s: hint %q, want %q", tc.name, got, tc.hint)
		}
		got := DownloadEntry(tc.snap)
		if got.Show != (tc.download != "") || (got.Show && (got.Title != "下载托盘 "+tc.download || got.URL != "https://github.com/"+ReleaseRepo+"/releases/tag/v"+tc.download)) {
			t.Errorf("%s: download %+v", tc.name, got)
		}
		if got := tc.snap.Installable(); got != tc.install {
			t.Errorf("%s: installable %v", tc.name, got)
		}
	}
	running := snap(tray)
	running.State, running.Running = StateExternal, &BuildInfo{Version: "0.2.0", Contract: 1}
	if got := VersionHint(running); got != "WSL 中已安装 0.3.0，重启 WebMux 后生效，重启会结束所有会话" {
		t.Errorf("restart hint %q", got)
	}
	running.Running = &BuildInfo{Version: "0.3.0", Contract: 1}
	if got := VersionHint(running); got != "" {
		t.Errorf("same running build hint %q", got)
	}
}

func TestStatusLine(t *testing.T) {
	running := Snapshot{State: StateRunning, Sessions: 2, HasStatus: true, Status: contract.StatusV1{State: contract.StateRunning, Version: "0.3.0"}}
	if got := StatusLine(running); got != "状态：运行中 · 2 个会话 · WebMux 0.3.0" {
		t.Errorf("running %q", got)
	}
	running.Degraded, running.Status.Version = true, "0.5.0"
	if got := StatusLine(running); got != "状态：运行中（不兼容，功能受限） · 会话数未知 · WebMux 0.5.0" {
		t.Errorf("degraded %q", got)
	}
	if got := StatusLine(Snapshot{State: StateStopped}); got != "状态：已停止" {
		t.Errorf("stopped %q", got)
	}
}

func TestTunnelEntry(t *testing.T) {
	running := func(tunnel *contract.TunnelStatus) Snapshot {
		return Snapshot{State: StateRunning, HasStatus: true, Mode: ModeTray, Status: contract.StatusV1{
			State: contract.StateRunning, URLs: contract.URLs{Local: "http://127.0.0.1:7681"}, Tunnel: tunnel,
		}}
	}
	ready := running(&contract.TunnelStatus{State: contract.TunnelReady, PublicURL: "https://abc.trycloudflare.com"})
	if got := TunnelEntry(ready); !got.Show || !got.Checked || got.PublicURL != "https://abc.trycloudflare.com" {
		t.Fatalf("ready %+v", got)
	}
	if got := TunnelEntry(running(&contract.TunnelStatus{State: contract.TunnelStarting})); !got.Checked || got.PublicURL != "" || got.Info != "公网隧道：正在连接" {
		t.Fatalf("starting %+v", got)
	}
	failed := TunnelEntry(running(&contract.TunnelStatus{State: contract.TunnelFailed, Error: "cloudflared exited"}))
	if !failed.Show || failed.Checked || !strings.Contains(failed.Info, "cloudflared exited") {
		t.Fatalf("failed %+v", failed)
	}
	if got := TunnelEntry(running(nil)); got.Show {
		t.Fatalf("old server shows the tunnel: %+v", got)
	}
	stopped := ready
	stopped.State, stopped.Status.State = StateStopped, contract.StateStopped
	if got := TunnelEntry(stopped); got.Show {
		t.Fatalf("stopped instance shows the tunnel: %+v", got)
	}
}

func TestPushEntry(t *testing.T) {
	const named = "https://term.example.com"
	snap := func(push *contract.PushStatus) Snapshot {
		return Snapshot{State: StateRunning, HasStatus: true, Mode: ModeTray, Status: contract.StatusV1{
			State: contract.StateRunning, URLs: contract.URLs{Local: "http://127.0.0.1:7681"}, TokenConfigured: true, Token: "SECRET-TOKEN",
			Tunnel: &contract.TunnelStatus{State: contract.TunnelOff}, Push: push,
		}}
	}
	for _, row := range []struct {
		name  string
		push  *contract.PushStatus
		info  string
		guide []string // substrings
	}{
		{"disabled", &contract.PushStatus{}, "手机推送：已在配置中停用", []string{"enabled = true", "Named Tunnel"}},
		{"storage", &contract.PushStatus{Enabled: true, Error: "permission denied"}, "手机推送不可用：permission denied", nil},
		{"quick tunnel only", &contract.PushStatus{Enabled: true, Available: true}, "手机推送：需要 Named Tunnel",
			[]string{"Quick Tunnel 的地址每次启动都会变化", "http://127.0.0.1:7681", "tunnel.token", "tunnel.hostname", "重启 WebMux", "添加到主屏幕", "设置 → 通知推送"}},
		{"named without devices", &contract.PushStatus{Enabled: true, Available: true, PublicOrigin: named}, "手机推送：尚未有设备开启",
			[]string{"公网地址：" + named, "复制公网链接", "输入 Token"}},
		{"named with devices", &contract.PushStatus{Enabled: true, Available: true, Subscriptions: 2, PublicOrigin: named}, "手机推送：2 台设备已开启", nil},
	} {
		got := PushEntry(snap(row.push))
		if !got.Show || got.Info != row.info {
			t.Errorf("%s: %+v", row.name, got)
		}
		for _, want := range row.guide {
			if !strings.Contains(got.Guide, want) {
				t.Errorf("%s: guide lacks %q:\n%s", row.name, want, got.Guide)
			}
		}
		if row.push.PublicOrigin != "" && strings.Contains(got.Guide, "tunnel.token") {
			t.Errorf("%s: named guide repeats tunnel setup", row.name)
		}
		if strings.Contains(got.Info+got.Guide, "SECRET-TOKEN") {
			t.Errorf("%s: token leaked", row.name)
		}
	}
	if got := PushEntry(snap(nil)); got.Show {
		t.Fatalf("old server shows push: %+v", got)
	}
	hidden := snap(&contract.PushStatus{Enabled: true})
	hidden.Status.Tunnel = nil
	if got := PushEntry(hidden); got.Show {
		t.Fatalf("push shown without tunnel entries: %+v", got)
	}
}
