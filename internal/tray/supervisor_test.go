package tray

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/contract"
)

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_800_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	return ch
}

// Advance moves time in small steps so every timer that falls due fires and
// the loop can schedule the next one.
func (c *fakeClock) Advance(d time.Duration) {
	const step = 250 * time.Millisecond
	for d > 0 {
		s := min(step, d)
		d -= s
		c.mu.Lock()
		c.now = c.now.Add(s)
		kept := c.waiters[:0]
		for _, w := range c.waiters {
			if !w.at.After(c.now) {
				w.ch <- c.now
			} else {
				kept = append(kept, w)
			}
		}
		c.waiters = kept
		c.mu.Unlock()
		time.Sleep(2 * time.Millisecond)
	}
}

type fakeProc struct {
	pid  int
	done chan struct{}
	code int
	once sync.Once
}

func (p *fakeProc) PID() int              { return p.pid }
func (p *fakeProc) Done() <-chan struct{} { return p.done }
func (p *fakeProc) ExitCode() int         { return p.code }
func (p *fakeProc) exit(code int)         { p.once.Do(func() { p.code = code; close(p.done) }) }

// fakeService simulates WebMux inside WSL.
type fakeService struct {
	mu          sync.Mutex
	running     bool
	launcher    string
	record      bool
	sessions    int
	health      contract.HealthStatus
	statusErr   error
	checkErr    error
	detection   Detection
	ready       bool // a started server becomes healthy
	crash       bool // a started server exits at once
	ignoreStop  bool
	unitFailed  bool
	procs       []*fakeProc
	calls       []string
	watchers    []chan int
	stoppedPort contract.HealthStatus
	version     string // reported by Check
	tunnel      *contract.TunnelStatus
}

func (f *fakeService) call(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
}

func (f *fakeService) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == name {
			n++
		}
	}
	return n
}

func (f *fakeService) Check(context.Context) (string, error) {
	f.call("check")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version, f.checkErr
}
func (f *fakeService) Detect(context.Context) Detection {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.detection
}

func (f *fakeService) Status(context.Context) (contract.StatusV1, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return contract.StatusV1{}, f.statusErr
	}
	s := contract.StatusV1{V: 1, URLs: contract.URLs{Local: "http://127.0.0.1:7681", Remote: []contract.RemoteURL{}}, Listen: "127.0.0.1:7681", Config: contract.ConfigRef{Exists: true}}
	if !f.running {
		s.State = contract.StateStopped
		s.Health = contract.Health{Status: contract.HealthRefused}
		if f.stoppedPort != "" {
			s.Health.Status = f.stoppedPort
		}
		return s, nil
	}
	health := f.health
	if health == "" {
		health = contract.HealthOK
	}
	sessions := f.sessions
	s.State, s.Record, s.Launcher, s.Sessions, s.Health, s.LocalAPI = contract.StateRunning, f.record, f.launcher, &sessions, contract.Health{Status: health}, true
	if f.tunnel != nil {
		t := *f.tunnel
		s.Tunnel = &t
	}
	return s, nil
}

func (f *fakeService) Watch(ctx context.Context, sessions func(int)) error {
	f.mu.Lock()
	if !f.running {
		f.mu.Unlock()
		return errors.New("not running")
	}
	ch := make(chan int, 8)
	f.watchers = append(f.watchers, ch)
	ch <- f.sessions
	f.mu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case n, ok := <-ch:
			if !ok {
				return errors.New("disconnected")
			}
			sessions(n)
		}
	}
}

func (f *fakeService) setSessions(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions = n
	for _, ch := range f.watchers {
		ch <- n
	}
}

// die simulates WebMux exiting: subscriptions drop, processes end.
func (f *fakeService) die(code int) {
	f.mu.Lock()
	f.running = false
	for _, ch := range f.watchers {
		close(ch)
	}
	f.watchers = nil
	procs := f.procs
	f.mu.Unlock()
	for _, p := range procs {
		p.exit(code)
	}
}

func (f *fakeService) StartTray(context.Context) (Process, error) {
	f.call("startTray")
	f.mu.Lock()
	p := &fakeProc{pid: 1000 + len(f.procs), done: make(chan struct{})}
	f.procs = append(f.procs, p)
	crash := f.crash
	if !crash && f.ready {
		f.running, f.launcher, f.record = true, contract.LauncherWindowsTray, true
	}
	f.mu.Unlock()
	if crash {
		go p.exit(1)
	}
	return p, nil
}

func (f *fakeService) Shutdown(context.Context, bool) error {
	f.call("shutdown")
	f.mu.Lock()
	ignore := f.ignoreStop
	f.mu.Unlock()
	if !ignore {
		f.die(0)
	}
	return nil
}

func (f *fakeService) ForceStop(context.Context) (contract.StopV1, error) {
	f.call("forceStop")
	f.die(137)
	return contract.StopV1{V: 1, Result: contract.StopKilled}, nil
}

func (f *fakeService) Systemctl(_ context.Context, action string) error {
	f.call("systemctl " + action)
	f.mu.Lock()
	switch action {
	case "start":
		f.running, f.launcher, f.record = true, contract.LauncherSystemd, true
	}
	f.mu.Unlock()
	if action == "stop" {
		f.die(0)
	}
	return nil
}

func (f *fakeService) UnitFailed(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unitFailed
}

func (f *fakeService) Keepalive(on bool) error {
	f.call(fmt.Sprintf("keepalive %v", on))
	return nil
}
func (f *fakeService) Token(context.Context) (string, error) { return "", nil }
func (f *fakeService) ConfigInit(context.Context) error      { return nil }
func (f *fakeService) Install(context.Context) error {
	f.call("install")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checkErr, f.version = nil, "9.9.9"
	return nil
}
func (f *fakeService) SetTunnel(_ context.Context, enabled bool) (contract.TunnelStatus, error) {
	f.call(fmt.Sprintf("tunnel %v", enabled))
	f.mu.Lock()
	defer f.mu.Unlock()
	state := contract.TunnelOff
	if enabled {
		state = contract.TunnelStarting
	}
	f.tunnel = &contract.TunnelStatus{State: state}
	return *f.tunnel, nil
}

func (f *fakeService) setTunnel(t contract.TunnelStatus) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tunnel = &t
}

type harness struct {
	t       *testing.T
	clock   *fakeClock
	service *fakeService
	sup     *Supervisor
	mu      sync.Mutex
	notes   []string
	reach   error
}

func newHarness(t *testing.T, service *fakeService, settings Settings) *harness {
	return newVersionedHarness(t, service, settings, "")
}

// newVersionedHarness runs a tray that reports version.
func newVersionedHarness(t *testing.T, service *fakeService, settings Settings, version string) *harness {
	h := &harness{t: t, clock: newFakeClock(), service: service}
	h.sup = NewSupervisor(Options{
		Service:  service,
		Clock:    h.clock,
		Settings: settings,
		Version:  version,
		Reach: func(context.Context, string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.reach
		},
		Notify: func(title, message string) {
			h.mu.Lock()
			h.notes = append(h.notes, title+": "+message)
			h.mu.Unlock()
		},
	})
	t.Cleanup(func() { h.sup.Quit(false) })
	return h
}

func (h *harness) wait(what string, ok func(Snapshot) bool) Snapshot {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		snap := h.sup.Snapshot()
		if ok(snap) {
			return snap
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for %s; last %s %q (calls %v)", what, snap.State, snap.Reason, h.service.calls)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (h *harness) state(want State) Snapshot {
	h.t.Helper()
	return h.wait(string(want), func(s Snapshot) bool { return s.State == want })
}

// settle advances fake time in small steps until a start has settled, so
// timers the loop arms asynchronously are never skipped.
func (h *harness) settle() {
	h.t.Helper()
	for range 40 {
		switch h.sup.Snapshot().State {
		case StateRunning, StateFailed, StateExternal:
			return
		}
		h.clock.Advance(250 * time.Millisecond)
	}
}

func (h *harness) notifications() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.notes)
}

func manualSettings() Settings {
	s := DefaultSettings()
	s.StartWebMux = false
	return s
}

func TestSupervisorAutostartsTrayManaged(t *testing.T) {
	service := &fakeService{ready: true}
	h := newHarness(t, service, DefaultSettings())
	h.state(StateStarting)
	h.settle()
	snap := h.state(StateRunning)
	if snap.Mode != ModeTray || service.count("startTray") != 1 || !snap.Actions().Stop || snap.Actions().Start {
		t.Fatalf("running %+v calls %v", snap, service.calls)
	}
	service.setSessions(3)
	h.wait("session count", func(s Snapshot) bool { return s.Sessions == 3 })
}

func TestSupervisorAutoSelectsSystemd(t *testing.T) {
	service := &fakeService{detection: Detection{Systemd: true, Unit: true}}
	h := newHarness(t, service, DefaultSettings())
	h.settle()
	snap := h.state(StateRunning)
	if snap.Mode != ModeSystemd || service.count("systemctl start") != 1 || service.count("keepalive true") == 0 || service.count("startTray") != 0 {
		t.Fatalf("systemd start %+v %v", snap, service.calls)
	}
	h.sup.Stop()
	h.state(StateStopped)
	if service.count("systemctl stop") != 1 || service.count("keepalive false") != 1 {
		t.Fatalf("systemd stop calls %v", service.calls)
	}
	// systemd restarts are the unit's business: no tray restart.
	h.clock.Advance(BackoffMax)
	if service.count("systemctl start") != 1 {
		t.Fatal("tray restarted a user-stopped systemd instance")
	}
}

func TestSupervisorExplicitSystemdUnavailable(t *testing.T) {
	service := &fakeService{detection: Detection{Systemd: false}}
	settings := DefaultSettings()
	settings.Mode = ModeSystemd
	h := newHarness(t, service, settings)
	snap := h.state(StateFailed)
	if !strings.Contains(snap.Reason, "发行版未启用 systemd") || service.count("startTray") != 0 {
		t.Fatalf("explicit systemd %+v %v", snap, service.calls)
	}
}

func TestSupervisorExternalAndRecoveredInstances(t *testing.T) {
	external := &fakeService{running: true, record: true, launcher: contract.LauncherManual}
	h := newHarness(t, external, DefaultSettings())
	snap := h.state(StateExternal)
	if actions := snap.Actions(); actions.Stop || actions.Restart || !actions.Access || !strings.Contains(snap.Reason, "由外部管理") {
		t.Fatalf("external %+v %+v", snap, actions)
	}
	if external.count("startTray") != 0 {
		t.Fatal("started a second instance")
	}
	h.sup.Stop()
	h.clock.Advance(time.Second)
	if external.count("shutdown") != 0 {
		t.Fatal("stopped an external instance")
	}

	legacy := &fakeService{running: true, record: false, launcher: contract.LauncherWindowsTray}
	h = newHarness(t, legacy, DefaultSettings())
	h.state(StateExternal)

	ours := &fakeService{running: true, record: true, launcher: contract.LauncherWindowsTray, sessions: 2}
	h = newHarness(t, ours, DefaultSettings())
	snap = h.state(StateRunning)
	if !snap.Actions().Stop || !snap.Actions().Restart || ours.count("startTray") != 0 {
		t.Fatalf("recovered control %+v", snap)
	}
}

func TestSupervisorReadyTimeoutKeepsProcess(t *testing.T) {
	service := &fakeService{ready: false}
	h := newHarness(t, service, DefaultSettings())
	h.state(StateStarting)
	h.clock.Advance(ReadyTimeout - 5*time.Second)
	h.state(StateStarting) // a slow WSL cold start is not a failure
	h.clock.Advance(10 * time.Second)
	snap := h.state(StateFailed)
	if snap.Reason != "WebMux 未在 60 秒内就绪：连接被拒绝（请查看日志）" || service.count("forceStop") != 0 || len(h.notifications()) != 0 {
		t.Fatalf("ready timeout %q %v %v", snap.Reason, service.calls, h.notifications())
	}
	// It becomes healthy later: the tray takes it over.
	service.mu.Lock()
	service.running, service.record, service.launcher = true, true, contract.LauncherWindowsTray
	service.mu.Unlock()
	h.clock.Advance(StatusInterval)
	h.state(StateRunning)
}

func TestSupervisorUnexpectedExitBacksOffAndGivesUp(t *testing.T) {
	service := &fakeService{ready: true}
	h := newHarness(t, service, DefaultSettings())
	h.settle()
	h.state(StateRunning)
	service.mu.Lock()
	service.crash = true
	service.mu.Unlock()
	service.die(1) // e.g. wsl --shutdown
	snap := h.state(StateRestarting)
	if notes := h.notifications(); len(notes) != 1 || !strings.Contains(notes[0], "会话已丢失") {
		t.Fatalf("notifications %v", notes)
	}
	if !strings.Contains(snap.Reason, "2 秒后重试") {
		t.Fatalf("first backoff %q", snap.Reason)
	}
	delays := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	for i, delay := range delays {
		retry := fmt.Sprintf("%d 秒后重试", int(delay/time.Second))
		h.wait("backoff "+retry, func(s Snapshot) bool { return s.State == StateRestarting && strings.Contains(s.Reason, retry) })
		h.clock.Advance(delay - 250*time.Millisecond)
		if got := service.count("startTray"); got != i+1 {
			t.Fatalf("restart %d happened before its %s backoff (%d starts)", i+1, delay, got)
		}
		h.clock.Advance(250 * time.Millisecond)
		h.wait(fmt.Sprintf("restart %d", i+1), func(Snapshot) bool { return service.count("startTray") == i+2 })
	}
	snap = h.state(StateFailed)
	if !strings.Contains(snap.Reason, "多次启动失败，请查看日志") || !snap.Actions().Start {
		t.Fatalf("gave up %+v", snap)
	}
	h.clock.Advance(BackoffMax * 2)
	if service.count("startTray") != 5 {
		t.Fatalf("kept retrying: %d starts", service.count("startTray"))
	}
	// A manual start resets the failure budget.
	service.mu.Lock()
	service.crash = false
	service.mu.Unlock()
	h.sup.Start()
	h.state(StateStarting)
	h.settle()
	h.state(StateRunning)
}

// A blocked tray stays blocked without polling, and recovers only when the
// user acts: a recheck after an install (or opening the menu) succeeds.
func TestSupervisorRecoversFromMissingWebmux(t *testing.T) {
	service := &fakeService{checkErr: fmt.Errorf("%w ~/.local/bin/webmux", ErrWebmuxMissing)}
	h := newHarness(t, service, DefaultSettings())
	snap := h.state(StateFailed)
	if snap.Blocker != BlockerMissing || snap.Actions().Start || !snap.Installable() {
		t.Fatalf("missing webmux %+v", snap)
	}
	h.sup.Recheck() // menu opened, nothing changed
	h.wait("second check", func(Snapshot) bool { return service.count("check") == 2 })
	h.clock.Advance(2*StatusInterval + BackoffMax)
	snap = h.sup.Snapshot()
	if snap.State != StateFailed || snap.Blocker != BlockerMissing || service.count("check") != 2 {
		t.Fatalf("still missing %+v %v", snap, service.calls)
	}
	if err := service.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.sup.Recheck()
	snap = h.state(StateStopped)
	if snap.Blocker != "" || !snap.Actions().Start || snap.Reason != "" || snap.WebmuxVersion != "9.9.9" {
		t.Fatalf("after install %+v", snap)
	}
	h.settle()
	if service.count("startTray") != 0 {
		t.Fatalf("recovery started WebMux: %v", service.calls)
	}
}

func TestSupervisorOffersUpgradeForOlderWebmux(t *testing.T) {
	for _, tc := range []struct {
		tray, installed, want string
	}{
		{"0.2.0", "0.1.9", "0.2.0"},
		{"0.2.0", "0.2.0", ""},
		{"0.2.0", "0.10.0", ""},
		{"dev", "0.1.0", ""},
		{"0.2.0", "dev", ""},
	} {
		service := &fakeService{ready: true, version: tc.installed}
		h := newVersionedHarness(t, service, DefaultSettings(), tc.tray)
		h.settle()
		snap := h.wait("checked", func(s Snapshot) bool { return s.State == StateRunning && s.WebmuxVersion == tc.installed })
		if snap.UpgradeTo != tc.want || snap.Installable() != (tc.want != "") {
			t.Errorf("tray %s, webmux %s: upgrade %q installable %v", tc.tray, tc.installed, snap.UpgradeTo, snap.Installable())
		}
		h.sup.Quit(false)
	}
}

func TestSupervisorFollowsTunnel(t *testing.T) {
	service := &fakeService{ready: true}
	h := newHarness(t, service, DefaultSettings())
	h.settle()
	h.state(StateRunning)
	if _, err := service.SetTunnel(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	h.sup.Refresh()
	h.wait("tunnel starting", func(s Snapshot) bool { return s.TunnelActive() })
	// While it starts, the status is polled at the starting interval.
	service.setTunnel(contract.TunnelStatus{State: contract.TunnelReady, PublicURL: "https://abc.trycloudflare.com"})
	h.clock.Advance(StartingInterval)
	h.wait("tunnel ready", func(s Snapshot) bool {
		return s.Status.Tunnel != nil && s.Status.Tunnel.State == contract.TunnelReady
	})
	service.setTunnel(contract.TunnelStatus{State: contract.TunnelFailed, Error: "cloudflared exited: exit status 1"})
	h.clock.Advance(StatusInterval)
	snap := h.wait("tunnel failed", func(s Snapshot) bool { return !s.TunnelActive() })
	if snap.State != StateRunning {
		t.Fatalf("tunnel failure changed WebMux state: %+v", snap)
	}
	if notes := h.notifications(); len(notes) != 1 || !strings.Contains(notes[0], "公网隧道已断开") || !strings.Contains(notes[0], "exit status 1") {
		t.Fatalf("notifications %v", notes)
	}
	h.clock.Advance(StatusInterval)
	if notes := h.notifications(); len(notes) != 1 {
		t.Fatalf("failure notified twice: %v", notes)
	}
	// A crash with an open tunnel says that the tunnel is gone too.
	service.setTunnel(contract.TunnelStatus{State: contract.TunnelReady, PublicURL: "https://abc.trycloudflare.com"})
	h.sup.Refresh()
	h.wait("tunnel ready again", func(s Snapshot) bool { return s.TunnelActive() })
	service.mu.Lock()
	service.tunnel = nil
	service.mu.Unlock()
	service.die(1)
	h.state(StateRestarting)
	if notes := h.notifications(); len(notes) != 2 || !strings.Contains(notes[1], "公网隧道已关闭") {
		t.Fatalf("crash notifications %v", notes)
	}
}

func TestSupervisorUserStopDoesNotRestart(t *testing.T) {
	service := &fakeService{ready: true}
	h := newHarness(t, service, DefaultSettings())
	h.settle()
	h.state(StateRunning)
	h.sup.Stop()
	h.state(StateStopped)
	h.clock.Advance(2 * BackoffMax)
	if service.count("startTray") != 1 || service.count("shutdown") != 1 || service.count("forceStop") != 0 || len(h.notifications()) != 0 {
		t.Fatalf("after user stop: %v %v", service.calls, h.notifications())
	}
}

func TestSupervisorStopTimeoutForces(t *testing.T) {
	service := &fakeService{ready: true, ignoreStop: true}
	h := newHarness(t, service, DefaultSettings())
	h.settle()
	h.state(StateRunning)
	h.sup.Stop()
	h.state(StateStopping)
	h.clock.Advance(StopTimeout - time.Second)
	if service.count("forceStop") != 0 {
		t.Fatal("forced before the stop timeout")
	}
	h.clock.Advance(2 * time.Second)
	h.state(StateStopped)
	if service.count("forceStop") != 1 {
		t.Fatalf("calls %v", service.calls)
	}
}

func TestSupervisorRestart(t *testing.T) {
	service := &fakeService{ready: true}
	h := newHarness(t, service, DefaultSettings())
	h.settle()
	h.state(StateRunning)
	h.sup.Restart()
	h.wait("second start", func(Snapshot) bool { return service.count("startTray") == 2 })
	h.settle()
	h.state(StateRunning)
	if service.count("shutdown") != 1 {
		t.Fatalf("calls %v", service.calls)
	}
}

func TestSupervisorUnhealthyAndUnreachable(t *testing.T) {
	service := &fakeService{running: true, record: true, launcher: contract.LauncherWindowsTray}
	h := newHarness(t, service, manualSettings())
	h.state(StateRunning)
	h.mu.Lock()
	h.reach = errors.New("connection refused")
	h.mu.Unlock()
	h.clock.Advance(ReachInterval)
	snap := h.state(StateUnreachable)
	if !strings.Contains(snap.Reason, "Windows 无法访问 127.0.0.1:7681") || !snap.Actions().Stop {
		t.Fatalf("unreachable %+v", snap)
	}
	h.mu.Lock()
	h.reach = nil
	h.mu.Unlock()
	h.clock.Advance(ReachInterval)
	h.state(StateRunning)

	service.mu.Lock()
	service.statusErr = context.DeadlineExceeded
	service.mu.Unlock()
	for range UnhealthyLimit - 1 {
		h.clock.Advance(StatusInterval)
	}
	if h.sup.Snapshot().State == StateFailed {
		t.Fatal("failed before three unanswered checks")
	}
	h.clock.Advance(StatusInterval)
	snap = h.state(StateFailed)
	if !strings.Contains(snap.Reason, "无响应") {
		t.Fatalf("unhealthy reason %q", snap.Reason)
	}
	service.mu.Lock()
	service.statusErr = nil
	service.mu.Unlock()
	h.clock.Advance(StatusInterval)
	h.state(StateRunning)
}

func TestSupervisorRefusesOccupiedPortAndOldWebmux(t *testing.T) {
	occupied := &fakeService{stoppedPort: contract.HealthNotWebMux}
	h := newHarness(t, occupied, DefaultSettings())
	snap := h.state(StateFailed)
	if !strings.Contains(snap.Reason, "已被其他程序占用") || occupied.count("startTray") != 0 {
		t.Fatalf("occupied %+v %v", snap, occupied.calls)
	}

	old := &fakeService{checkErr: ErrWebmuxTooOld}
	h = newHarness(t, old, DefaultSettings())
	snap = h.state(StateFailed)
	if snap.Reason != ErrWebmuxTooOld.Error() || snap.Actions().Start {
		t.Fatalf("old webmux %+v", snap)
	}
	h.clock.Advance(2*StatusInterval + BackoffMax)
	if old.count("check") != 1 || old.count("startTray") != 0 {
		t.Fatalf("retried an incompatible webmux: %v", old.calls)
	}
}

func TestSupervisorSystemdUnitFailure(t *testing.T) {
	service := &fakeService{running: true, record: true, launcher: contract.LauncherSystemd}
	h := newHarness(t, service, manualSettings())
	h.state(StateRunning)
	service.mu.Lock()
	service.unitFailed = true
	service.mu.Unlock()
	service.die(1)
	h.clock.Advance(StatusInterval)
	snap := h.state(StateFailed)
	if !strings.Contains(snap.Reason, "失败状态") || service.count("startTray") != 0 || service.count("systemctl start") != 0 {
		t.Fatalf("unit failure %+v %v", snap, service.calls)
	}
	if notes := h.notifications(); len(notes) < 2 {
		t.Fatalf("notifications %v", notes)
	}
}

func TestSupervisorQuit(t *testing.T) {
	service := &fakeService{ready: true}
	h := newHarness(t, service, DefaultSettings())
	h.settle()
	h.state(StateRunning)
	h.sup.Quit(false)
	if service.count("shutdown") != 0 {
		t.Fatal("quit-only stopped WebMux")
	}

	service = &fakeService{ready: true}
	h = newHarness(t, service, DefaultSettings())
	h.settle()
	h.state(StateRunning)
	done := make(chan struct{})
	go func() { h.sup.Quit(true); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("quit with stop did not finish")
	}
	if service.count("shutdown") != 1 {
		t.Fatalf("calls %v", service.calls)
	}
}

func TestSupervisorSwitchDistroRestartsManagedInstance(t *testing.T) {
	service := &fakeService{ready: true}
	h := newHarness(t, service, DefaultSettings())
	h.settle()
	h.state(StateRunning)
	next := DefaultSettings()
	next.Distro = "Debian"
	h.sup.SetSettings(next)
	h.wait("restart in the new distribution", func(s Snapshot) bool {
		return service.count("startTray") == 2 && s.Settings.Distro == "Debian"
	})
	if service.count("shutdown") != 1 {
		t.Fatalf("calls %v", service.calls)
	}
	// Path-only changes never stop a running instance.
	h.settle()
	h.state(StateRunning)
	paths := next
	paths.LogFile = "~/other.log"
	h.sup.SetSettings(paths)
	h.wait("settings applied", func(s Snapshot) bool { return s.Settings.LogFile == "~/other.log" })
	if service.count("shutdown") != 1 {
		t.Fatal("path change stopped WebMux")
	}
}

func TestActionsMatchMenuRules(t *testing.T) {
	running := contract.StatusV1{State: contract.StateRunning, URLs: contract.URLs{Local: "http://127.0.0.1:7681"}}
	for _, tc := range []struct {
		snap Snapshot
		want Actions
	}{
		{Snapshot{State: StateStopped}, Actions{Start: true}},
		{Snapshot{State: StateStarting}, Actions{}},
		{Snapshot{State: StateStopping, HasStatus: true, Status: running}, Actions{Access: true}},
		{Snapshot{State: StateRunning, HasStatus: true, Status: running, Mode: ModeTray}, Actions{Stop: true, Restart: true, Access: true}},
		{Snapshot{State: StateExternal, HasStatus: true, Status: running}, Actions{Access: true}},
		{Snapshot{State: StateFailed, Reason: "找不到 webmux ~/x", Blocker: BlockerMissing}, Actions{}},
		{Snapshot{State: StateFailed, Reason: "多次启动失败"}, Actions{Start: true}},
	} {
		if got := tc.snap.Actions(); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.snap.State, got, tc.want)
		}
	}
}
