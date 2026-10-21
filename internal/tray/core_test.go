package tray

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/contract"
)

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tray.json")
	s, err := LoadSettings(path)
	if err != nil || !reflect.DeepEqual(s, DefaultSettings()) {
		t.Fatalf("missing file: %+v %v", s, err)
	}
	if s.Webmux != "~/.local/bin/webmux" || s.Mode != ModeAuto || !s.StartWebMux || !s.AutoRestart || s.Config != "" {
		t.Fatalf("defaults %+v", s)
	}
	for name, content := range map[string]string{
		"corrupt":      "{not json",
		"unknown":      `{"mode":"auto","token":"secret"}`,
		"invalid mode": `{"mode":"docker"}`,
		"relative":     `{"config":"configs/webmux.toml"}`,
		"loopback":     `{"forward":["127.0.0.1"]}`,
		"trailing":     `{"mode":"tray"} {}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := LoadSettings(path)
		if err == nil || !reflect.DeepEqual(s, DefaultSettings()) {
			t.Errorf("%s: %+v %v", name, s, err)
		}
		if data, _ := os.ReadFile(path); string(data) != content {
			t.Errorf("%s: file rewritten", name)
		}
	}
	custom := DefaultSettings()
	custom.Distro, custom.Mode, custom.Config, custom.Forward = "Ubuntu-24.04", ModeSystemd, "~/my configs/webmux;$(id).toml", []string{"100.64.0.9"}
	if err := SaveSettings(path, custom); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(strings.ToLower(string(data)), "token") {
		t.Fatalf("settings mention a token: %s", data)
	}
	if got, err := LoadSettings(path); err != nil || !reflect.DeepEqual(got, custom) {
		t.Fatalf("round trip %+v %v", got, err)
	}
	// A UTF-8 BOM from Notepad is accepted.
	if err := os.WriteFile(path, append([]byte("\xef\xbb\xbf"), data...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(path); err != nil {
		t.Fatalf("BOM: %v", err)
	}
}

func TestInvocationsAreArgv(t *testing.T) {
	target := Target{Distro: "Ubuntu", Webmux: "~/.local/bin/webmux", Config: "~/my configs/webmux;$(id).toml"}
	serve := target.Serve("~/logs/web mux.log", []string{"WSLENV=USERPROFILE/p:WT_SESSION"})
	want := []string{"wsl.exe", "-d", "Ubuntu", "--cd", "~", "--exec", "./.local/bin/webmux", "serve", "--log-file", "~/logs/web mux.log", "--config", "~/my configs/webmux;$(id).toml"}
	if !slices.Equal(serve.Argv, want) {
		t.Fatalf("serve argv %q", serve.Argv)
	}
	if !slices.Equal(serve.Env, []string{"WEBMUX_LAUNCHER=windows-tray", "WSLENV=USERPROFILE/p:WT_SESSION:WEBMUX_LAUNCHER"}) {
		t.Fatalf("serve env %q", serve.Env)
	}
	if env := (Target{Webmux: "/usr/bin/webmux"}).Serve("/l", nil).Env; env[1] != "WSLENV=WEBMUX_LAUNCHER" {
		t.Fatalf("empty WSLENV: %q", env)
	}
	if MergeWSLENV("A:WEBMUX_LAUNCHER/u", "WEBMUX_LAUNCHER") != "A:WEBMUX_LAUNCHER/u" {
		t.Fatal("duplicate WSLENV entry")
	}
	bridge := (Target{Webmux: "/opt/web mux/webmux"}).Bridge()
	if !slices.Equal(bridge.Argv, []string{"wsl.exe", "--cd", "~", "--exec", "/opt/web mux/webmux", "bridge"}) {
		t.Fatalf("bridge argv %q", bridge.Argv)
	}
	if got := target.Systemctl("restart").Argv; !slices.Equal(got, []string{"wsl.exe", "-d", "Ubuntu", "--exec", "systemctl", "--user", "restart", "webmux.service"}) {
		t.Fatalf("systemctl argv %q", got)
	}
	for _, inv := range []Invocation{serve, bridge, target.Status(), target.ForceStop(), target.ConfigInit(), target.Probe()} {
		for _, arg := range inv.Argv {
			if arg == "sh" || arg == "bash" || arg == "-c" {
				t.Fatalf("shell in argv %q", inv.Argv)
			}
		}
	}
}

// scriptedRunner answers Run by the program after --exec.
type scriptedRunner map[string]Result

func (r scriptedRunner) Run(_ context.Context, argv, _ []string) (Result, error) {
	for i, arg := range argv {
		if arg == "--exec" && i+1 < len(argv) {
			key := strings.Join(argv[i+1:], " ")
			if res, ok := r[key]; ok {
				return res, nil
			}
		}
	}
	return Result{Code: 1}, nil
}
func (scriptedRunner) Start([]string, []string) (Process, error) { return nil, errors.New("unused") }

func TestModeSelection(t *testing.T) {
	target := Target{Webmux: "webmux"}
	systemd := scriptedRunner{"cat /proc/1/comm": {Stdout: []byte("systemd\n")}, "systemctl --user cat webmux.service": {}}
	noUnit := scriptedRunner{"cat /proc/1/comm": {Stdout: []byte("systemd\n")}}
	initd := scriptedRunner{"cat /proc/1/comm": {Stdout: []byte("init\n")}}
	cases := []struct {
		runner scriptedRunner
		mode   Mode
		want   Mode
		reason string
	}{
		{systemd, ModeAuto, ModeSystemd, ""},
		{initd, ModeAuto, ModeTray, ""},
		{noUnit, ModeAuto, ModeTray, ""},
		{systemd, ModeTray, ModeTray, ""},
		{initd, ModeSystemd, "", "发行版未启用 systemd"},
		{noUnit, ModeSystemd, "", "未安装 webmux 用户单元"},
	}
	for _, tc := range cases {
		d := DetectSystemd(context.Background(), tc.runner, target)
		got, err := Resolve(tc.mode, d)
		if got != tc.want || (tc.reason == "") != (err == nil) || (err != nil && (!errors.Is(err, ErrSystemdUnavailable) || !strings.Contains(err.Error(), tc.reason))) {
			t.Errorf("%v %s: %s %v", d, tc.mode, got, err)
		}
	}
	running := func(record bool, launcher string) contract.StatusV1 {
		return contract.StatusV1{State: contract.StateRunning, Record: record, Launcher: launcher}
	}
	for _, tc := range []struct {
		status     contract.StatusV1
		configured Mode
		mode       Mode
		managed    bool
	}{
		{running(true, "windows-tray"), ModeAuto, ModeTray, true},
		{running(true, "windows-tray"), ModeTray, ModeTray, true},
		{running(true, "windows-tray"), ModeSystemd, "", false},
		{running(true, "systemd"), ModeAuto, ModeSystemd, true},
		{running(true, "systemd"), ModeTray, "", false},
		{running(true, "manual"), ModeAuto, "", false},
		{running(false, "windows-tray"), ModeAuto, "", false},
	} {
		if mode, managed := ManagedMode(tc.status, tc.configured); mode != tc.mode || managed != tc.managed {
			t.Errorf("%+v under %s: %s %v", tc.status, tc.configured, mode, managed)
		}
	}
}

func TestPlanForward(t *testing.T) {
	ifaces := []WinInterface{
		{Name: "Loopback", Up: true, Loopback: true, Addrs: []net.IP{net.ParseIP("127.0.0.1")}},
		{Name: "Wi-Fi", Up: true, Addrs: []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("fe80::1"), net.ParseIP("169.254.1.1")}},
		{Name: "vEthernet (WSL (Hyper-V firewall))", Up: true, Addrs: []net.IP{net.ParseIP("172.29.0.1")}},
		{Name: "Tailscale", Up: true, Addrs: []net.IP{net.ParseIP("100.64.0.9")}},
		{Name: "Ethernet 2", Up: false, Addrs: []net.IP{net.ParseIP("10.0.0.5")}},
	}
	candidates := []Candidate{{"100.64.0.9", "Tailscale", "tailscale"}, {"192.168.1.20", "Wi-Fi", "lan"}}
	if got := WindowsCandidates(ifaces); !reflect.DeepEqual(got, candidates) {
		t.Fatalf("candidates %+v", got)
	}
	healthy := Snapshot{HasStatus: true, Reachable: true, Status: contract.StatusV1{State: contract.StateRunning, Health: contract.Health{Status: contract.HealthOK}, TokenConfigured: true, URLs: contract.URLs{Local: "http://127.0.0.1:7681", Remote: []contract.RemoteURL{}}, Config: contract.ConfigRef{Exists: true}}}
	plan := PlanForward("nat", healthy, ifaces)
	if !plan.Needed || plan.Blocked != "" || !reflect.DeepEqual(plan.Candidates, candidates) || plan.Headline != "可通过托盘转发" {
		t.Fatalf("NAT plan %+v", plan)
	}
	if plan := PlanForward("mirrored", healthy, ifaces); plan.Needed || plan.Headline != "无需转发（镜像网络模式）" || plan.Blocked == "" {
		t.Fatalf("mirrored plan %+v", plan)
	}
	remote := healthy
	remote.Status.URLs.Remote = []contract.RemoteURL{{URL: "http://100.64.0.5:7681"}}
	if plan := PlanForward("nat", remote, ifaces); plan.Needed {
		t.Fatalf("remote links still need forwarding: %+v", plan)
	}
	noToken := healthy
	noToken.Status.TokenConfigured = false
	noToken.Status.Config.Exists = false
	if plan := PlanForward("nat", noToken, ifaces); !strings.Contains(plan.Blocked, "请先设置 Token") || !plan.OfferInit {
		t.Fatalf("no token plan %+v", plan)
	}
	stopped := healthy
	stopped.Status.State = contract.StateStopped
	if plan := PlanForward("nat", stopped, ifaces); plan.Blocked == "" {
		t.Fatal("forwarding allowed while stopped")
	}
	unreachable := healthy
	unreachable.Reachable = false
	if plan := PlanForward("nat", unreachable, ifaces); plan.Blocked == "" {
		t.Fatal("forwarding allowed while Windows cannot reach WebMux")
	}
}

func TestLogRotatesAndRedacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "WebMux", "tray.log")
	log, err := OpenLog(path, 200)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	secret := "tok_9f8e7d6c5b4a"
	release := log.Redact(secret)
	log.Printf("copied token %s to the clipboard", secret)
	release()
	if first, _ := os.ReadFile(path); strings.Contains(string(first), secret) || !strings.Contains(string(first), "[redacted]") {
		t.Fatalf("token not redacted: %q", first)
	}
	for range 20 {
		log.Printf("health check failed: connection refused")
	}
	current, _ := os.ReadFile(path)
	previous, _ := os.ReadFile(path + ".1")
	if len(current) > 200 || len(previous) == 0 || len(previous) > 200 {
		t.Fatalf("rotation: %d / %d bytes", len(current), len(previous))
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 2 {
		t.Fatalf("expected one rotated file, have %v", entries)
	}
	all := string(current) + string(previous)
	if strings.Contains(all, secret) {
		t.Fatal("token written to the tray log")
	}
}

// probeRunner answers the version probe of a current webmux.
type probeRunner struct{}

func (probeRunner) Run(context.Context, []string, []string) (Result, error) {
	return Result{Stderr: []byte("  -log-file string\n"), Code: 1}, nil
}
func (probeRunner) Start([]string, []string) (Process, error) { return nil, errors.New("unused") }

func TestTargetServiceRetargets(t *testing.T) {
	build := func(s Settings) *WSLService {
		return &WSLService{Runner: probeRunner{}, Target: TargetOf(s), LogFile: s.LogFile}
	}
	first := DefaultSettings()
	first.Distro = "Ubuntu"
	svc := NewTargetService(build, first)
	next := first
	next.Distro = "Debian"
	// While an instance runs in Ubuntu, its stop must still reach Ubuntu.
	svc.Retarget(next, false)
	if svc.Current().Target.Distro != "Ubuntu" {
		t.Fatal("deferred retarget applied early")
	}
	if err := svc.Check(context.Background()); err != nil || svc.Current().Target.Distro != "Debian" {
		t.Fatalf("next start still targets %s (%v)", svc.Current().Target.Distro, err)
	}
	logs := next
	logs.LogFile = "~/other.log"
	svc.Retarget(logs, false)
	if !svc.Settle() || svc.Current().LogFile != "~/other.log" || svc.Settle() {
		t.Fatal("pending path change not settled exactly once")
	}
	svc.Retarget(first, true)
	if svc.Current().Target.Distro != "Ubuntu" {
		t.Fatal("immediate retarget ignored")
	}
}
