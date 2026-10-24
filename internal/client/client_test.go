package client

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/runtime"
)

func TestLinks(t *testing.T) {
	hosts := func() ([]Interface, error) {
		return []Interface{
			{Name: "lo", Up: true, Loopback: true, Addrs: []net.IP{net.ParseIP("127.0.0.1")}},
			{Name: "eth0", Up: true, Addrs: []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("fe80::1"), net.ParseIP("169.254.3.4")}},
			{Name: "eth1", Up: false, Addrs: []net.IP{net.ParseIP("10.0.0.9")}},
			{Name: "eth2", Up: true, WSLVirtual: true, Addrs: []net.IP{net.ParseIP("172.20.1.2")}},
			{Name: "tailscale0", Up: true, Addrs: []net.IP{net.ParseIP("100.64.0.5")}},
		}, nil
	}
	remote := func(url, name, kind string) contract.RemoteURL {
		return contract.RemoteURL{URL: url, Interface: name, Kind: kind}
	}
	wildcard := []contract.RemoteURL{
		remote("http://100.64.0.5:7681", "tailscale0", "tailscale"),
		remote("http://192.168.1.20:7681", "eth0", "lan"),
	}
	cases := []struct {
		listen   string
		token    bool
		local    string
		remote   []contract.RemoteURL
		warnings int
	}{
		{"127.0.0.1:7681", false, "http://127.0.0.1:7681", []contract.RemoteURL{}, 0},
		{"localhost:7681", false, "http://127.0.0.1:7681", []contract.RemoteURL{}, 0},
		{"[::1]:7681", false, "http://[::1]:7681", []contract.RemoteURL{}, 0},
		{"0.0.0.0:7681", true, "http://127.0.0.1:7681", wildcard, 0},
		{"[::]:7681", false, "http://127.0.0.1:7681", wildcard, 1},
		{":7681", false, "http://127.0.0.1:7681", wildcard, 1},
		{"100.64.0.5:7681", false, "http://100.64.0.5:7681", []contract.RemoteURL{remote("http://100.64.0.5:7681", "tailscale0", "tailscale")}, 0},
		{"192.168.1.20:7681", false, "http://192.168.1.20:7681", []contract.RemoteURL{remote("http://192.168.1.20:7681", "eth0", "lan")}, 0},
	}
	for _, tc := range cases {
		urls, warnings := Links(tc.listen, tc.token, hosts)
		if urls.Local != tc.local || !reflect.DeepEqual(urls.Remote, tc.remote) || len(warnings) != tc.warnings {
			t.Errorf("%s: %+v %q", tc.listen, urls, warnings)
		}
		if strings.Contains(urls.Local, "token") {
			t.Errorf("%s: link carries a token", tc.listen)
		}
	}
}

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdg"))

	target := Locate(filepath.Join(dir, "absent.toml"), false)
	if target.ConfigErr != nil || target.ConfigExists || target.Config.Listen != "0.0.0.0:7681" || target.RunDir != filepath.Join(dir, "xdg", "webmux", "run") {
		t.Fatalf("default config: %+v", target)
	}
	if !errors.Is(target.RecordErr, runtime.ErrNoRecord) || target.Stale() || target.Socket != filepath.Join(target.RunDir, "webmux.sock") {
		t.Fatalf("no record: %+v", target)
	}
	if target := Locate(filepath.Join(dir, "absent.toml"), true); target.ConfigErr == nil {
		t.Fatal("explicit missing config accepted")
	}

	state := filepath.Join(dir, "state")
	path := writeConfig(t, dir, "state_dir = '"+state+"'\n")
	run := runtime.Dir(state)
	if err := runtime.EnsureDir(run); err != nil {
		t.Fatal(err)
	}
	exited := exec.Command("true")
	if err := exited.Run(); err != nil {
		t.Fatal(err)
	}
	self, err := runtime.ProcessIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for name, record := range map[string]runtime.Record{
		"missing pid":      {PID: exited.Process.Pid, Identity: runtime.Identity{Start: "1"}, Listen: "127.0.0.1:1"},
		"identity changed": {PID: os.Getpid(), Identity: runtime.Identity{Start: self.Start + "1", Exe: self.Exe}, Listen: "127.0.0.1:1"},
	} {
		if err := runtime.Write(run, record); err != nil {
			t.Fatal(err)
		}
		target := Locate(path, true)
		if target.Record != nil || !target.Stale() {
			t.Errorf("%s: stale record accepted: %+v", name, target)
		}
	}
	live := runtime.Record{PID: os.Getpid(), Identity: self, Listen: "127.0.0.1:1", Socket: "/run/x.sock"}
	if err := runtime.Write(run, live); err != nil {
		t.Fatal(err)
	}
	target = Locate(path, true)
	if target.Record == nil || target.Socket != "/run/x.sock" {
		t.Fatalf("live record: %+v", target)
	}
}

func TestDrift(t *testing.T) {
	record := runtime.Record{ConfiguredListen: "127.0.0.1:7681", TokenFingerprint: runtime.TokenFingerprint("a")}
	dir := t.TempDir()
	path := writeConfig(t, dir, "listen = '127.0.0.1:7700'\ntoken = 'a'\n")
	c, err := LoadConfig(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := Drift(record, c); !reflect.DeepEqual(got, []string{"listen"}) {
		t.Fatalf("drift %v", got)
	}
	c.Token = "b"
	if got := Drift(record, c); !reflect.DeepEqual(got, []string{"listen", "token"}) {
		t.Fatalf("drift %v", got)
	}
	record.TokenGenerated = true
	if got := Drift(record, c); !reflect.DeepEqual(got, []string{"listen"}) {
		t.Fatalf("generated tunnel token reported as drift: %v", got)
	}
}
