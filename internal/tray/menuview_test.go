package tray

import (
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/contract"
)

func TestInstallEntry(t *testing.T) {
	missing := Snapshot{State: StateFailed, Blocker: BlockerMissing}
	older := Snapshot{State: StateRunning, WebmuxVersion: "0.1.0", UpgradeTo: "0.2.0"}
	for _, tc := range []struct {
		name               string
		snap               Snapshot
		supported, running bool
		show, enabled      bool
		title              string
	}{
		{"usable", Snapshot{State: StateStopped, WebmuxVersion: "0.2.0"}, true, false, false, false, ""},
		{"missing", missing, true, false, true, true, "安装 WSL 中的 webmux"},
		{"incompatible", Snapshot{State: StateFailed, Blocker: BlockerIncompatible}, true, false, true, true, "升级 WSL 中的 webmux"},
		{"older while running", older, true, false, true, true, "升级 WSL 中的 webmux（0.1.0 → 0.2.0）"},
		{"development tray", missing, false, false, true, false, ErrInstallUnsupported.Error()},
		{"installing", missing, true, true, true, false, "正在安装"},
		{"while starting", Snapshot{State: StateStarting, UpgradeTo: "0.2.0", WebmuxVersion: "0.1.0"}, true, false, true, false, "升级"},
	} {
		got := InstallEntry(tc.snap, tc.supported, tc.running)
		if got.Show != tc.show || got.Enabled != tc.enabled || !strings.Contains(got.Title, tc.title) {
			t.Errorf("%s: %+v", tc.name, got)
		}
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
