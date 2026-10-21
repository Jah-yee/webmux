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
