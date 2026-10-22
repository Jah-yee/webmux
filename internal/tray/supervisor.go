package tray

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rainchan/webmux/internal/contract"
)

// State is the tray's view of WebMux.
type State string

const (
	StateStopped     State = "stopped"
	StateStarting    State = "starting"
	StateRunning     State = "running"
	StateUnreachable State = "unreachable"
	StateExternal    State = "external"
	StateStopping    State = "stopping"
	StateRestarting  State = "restarting"
	StateFailed      State = "failed"
)

// Timing of the supervisor; see wsl-service-lifecycle.
const (
	ReadyTimeout      = 60 * time.Second
	StatusInterval    = 60 * time.Second
	StartingInterval  = 2 * time.Second
	ReachInterval     = 10 * time.Second
	StopTimeout       = 15 * time.Second
	BackoffFirst      = 2 * time.Second
	BackoffMax        = 60 * time.Second
	FailureWindow     = 10 * time.Minute
	MaxFailures       = 5
	UnhealthyLimit    = 3
	WatchBackoffFirst = time.Second
	WatchBackoffMax   = 30 * time.Second
)

// Clock is injectable for tests.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// RealClock is the wall clock.
var RealClock Clock = realClock{}

// Reacher checks from Windows that a local link answers.
type Reacher func(ctx context.Context, url string) error

// Snapshot is an immutable view published to the UI.
type Snapshot struct {
	State State
	// Mode is the mode managing the current instance, or the mode the next
	// start would use.
	Mode      Mode
	Detection Detection
	Status    contract.StatusV1
	// HasStatus is false until the first status arrives.
	HasStatus bool
	Sessions  int
	// Reason is the latest failure or hint for the status line.
	Reason    string
	Reachable bool
	Settings  Settings
	// Blocker is set while webmux cannot be used at all; only an install,
	// a settings change or a successful recheck clears it.
	Blocker Blocker
	// Tray is this tray's build.
	Tray BuildInfo
	// Installed is the build at the configured webmux path; Compat
	// classifies it and is "" until checked.
	Installed BuildInfo
	Compat    Compat
	// Running is the build of the running instance, or nil; RunningCompat
	// classifies it.
	Running       *BuildInfo
	RunningCompat Compat
	// Degraded marks an instance whose status uses a contract the tray does
	// not speak; only reachability from Windows follows it.
	Degraded bool
}

// Blocker classifies failures that need user action before a start.
// BlockerIncompatible is a webmux without the local API, which the tray
// cannot start; an unsupported contract is a Compat, not a Blocker.
type Blocker string

const (
	BlockerMissing      Blocker = "missing"
	BlockerIncompatible Blocker = "incompatible"
	BlockerNoWSL        Blocker = "no-wsl"
)

func blockerOf(err error) Blocker {
	switch {
	case errors.Is(err, ErrWebmuxMissing):
		return BlockerMissing
	case errors.Is(err, ErrWebmuxTooOld):
		return BlockerIncompatible
	case errors.Is(err, ErrNoWSL):
		return BlockerNoWSL
	}
	return ""
}

// Installable reports whether the menu should offer installing or
// upgrading the server.
func (s Snapshot) Installable() bool {
	return s.Blocker == BlockerMissing || s.Blocker == BlockerIncompatible || s.Compat.Older()
}

// TunnelActive reports a starting or ready tunnel on the running instance.
func (s Snapshot) TunnelActive() bool {
	t := s.Status.Tunnel
	return s.HasStatus && s.Status.State == contract.StateRunning && t != nil && (t.State == contract.TunnelStarting || t.State == contract.TunnelReady)
}

// Managed reports whether the tray may stop or restart the instance.
func (s Snapshot) Managed() bool {
	switch s.State {
	case StateRunning, StateUnreachable, StateFailed:
		return s.HasStatus && s.Status.State == contract.StateRunning && s.Mode != ""
	}
	return false
}

// Actions are the menu enablement rules.
type Actions struct {
	Start, Stop, Restart, Access bool
}

func (s Snapshot) Actions() Actions {
	running := s.HasStatus && s.Status.State == contract.StateRunning
	switch s.State {
	case StateStarting, StateStopping, StateRestarting:
		return Actions{Access: running && s.Status.URLs.Local != ""}
	case StateExternal:
		return Actions{Access: true}
	case StateRunning, StateUnreachable:
		return Actions{Stop: true, Restart: true, Access: true}
	case StateFailed:
		if running {
			return Actions{Stop: s.Managed(), Restart: s.Managed(), Access: true}
		}
		return Actions{Start: s.Blocker == ""}
	}
	return Actions{Start: true}
}

// Supervisor runs a single event loop; Service calls run in goroutines and
// report back as events so the loop never blocks on wsl.exe.
type Supervisor struct {
	service Service
	reach   Reacher
	clock   Clock
	tray    BuildInfo
	publish func(Snapshot)
	notify  func(title, message string)
	logf    func(format string, args ...any)

	events chan any
	done   chan struct{}

	mu   sync.Mutex
	last Snapshot
}

// Options configure a Supervisor.
type Options struct {
	Service  Service
	Reach    Reacher
	Clock    Clock
	Settings Settings
	// Version is the tray version, compared with the installed server.
	Version string
	// Publish receives every snapshot from the loop goroutine.
	Publish func(Snapshot)
	Notify  func(title, message string)
	Logf    func(format string, args ...any)
}

type commandKind int

const (
	cmdStart commandKind = iota
	cmdStop
	cmdRestart
	cmdRefresh
	cmdRecheck
	cmdSettings
	cmdQuit
)

type command struct {
	kind     commandKind
	settings Settings
	stop     bool
	reply    chan struct{}
}

type statusEvent struct {
	gen    int
	status contract.StatusV1
	err    error
}
type reachEvent struct {
	gen int
	err error
}
type sessionsEvent struct {
	gen   int
	count int
}
type watchEndEvent struct {
	gen int
	err error
}
type exitEvent struct {
	gen  int
	code int
}
type startEvent struct {
	gen       int
	mode      Mode
	detection Detection
	proc      Process
	status    contract.StatusV1
	running   bool
	fatal     bool
	checked   bool
	info      BuildInfo
	// degraded is set with running for a reachable instance whose status
	// uses an unsupported contract.
	degraded *ContractError
	err      error
}
type checkEvent struct {
	info BuildInfo
	err  error
}
type stopEvent struct {
	gen    int
	forced bool
	err    error
}

// NewSupervisor starts the loop. It refreshes status first, then starts
// WebMux when Settings.StartWebMux is set and nothing runs.
func NewSupervisor(o Options) *Supervisor {
	if o.Clock == nil {
		o.Clock = RealClock
	}
	if o.Notify == nil {
		o.Notify = func(string, string) {}
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	if o.Publish == nil {
		o.Publish = func(Snapshot) {}
	}
	s := &Supervisor{service: o.Service, reach: o.Reach, clock: o.Clock, tray: BuildInfo{Version: o.Version, Contract: slices.Max(SupportedContracts)}, publish: o.Publish, notify: o.Notify, logf: o.Logf, events: make(chan any, 64), done: make(chan struct{})}
	go s.loop(o.Settings)
	return s
}

// Snapshot returns the latest published state without blocking.
func (s *Supervisor) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

func (s *Supervisor) send(c command) {
	select {
	case s.events <- c:
	case <-s.done:
	}
}

func (s *Supervisor) Start()   { s.send(command{kind: cmdStart}) }
func (s *Supervisor) Stop()    { s.send(command{kind: cmdStop}) }
func (s *Supervisor) Restart() { s.send(command{kind: cmdRestart}) }
func (s *Supervisor) Refresh() { s.send(command{kind: cmdRefresh}) }

// Recheck verifies the installed server again, for example after an
// install or when the menu opens on a blocked tray.
func (s *Supervisor) Recheck() { s.send(command{kind: cmdRecheck}) }

// SetSettings applies new settings. A changed distribution or mode restarts
// a managed running instance (the UI confirms first); other path changes
// apply at the next start.
func (s *Supervisor) SetSettings(settings Settings) {
	s.send(command{kind: cmdSettings, settings: settings})
}

// Quit ends the loop, stopping a managed instance first when stop is set.
// It returns after the loop has finished.
func (s *Supervisor) Quit(stop bool) {
	reply := make(chan struct{})
	s.send(command{kind: cmdQuit, stop: stop, reply: reply})
	select {
	case <-reply:
	case <-s.done:
	}
}

// loopState is owned by the loop goroutine.
type loopState struct {
	Snapshot
	gen        int // instance generation: discards stale results
	proc       Process
	userStop   bool
	unhealthy  int
	failures   []time.Time
	pendingOp  bool
	statusBusy bool
	checkBusy  bool
	reachBusy  bool
	watching   bool
	watchStop  context.CancelFunc
	watchDelay time.Duration
	after      []func() // continuation once an operation settles
	quitting   bool

	nextStatus, nextReach, readyBy, backoffAt, watchAt time.Time
	lastReason                                         string
}

func (s *Supervisor) loop(settings Settings) {
	defer close(s.done)
	ls := &loopState{Snapshot: Snapshot{State: StateStopped, Settings: settings, Mode: settings.Mode, Tray: s.tray}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	boot := true
	s.refresh(ctx, ls)
	s.publishState(ls)
	for {
		wait := s.nextWake(ls)
		select {
		case ev := <-s.events:
			if done := s.handle(ctx, ls, ev, &boot); done {
				if ls.watchStop != nil {
					ls.watchStop()
				}
				return
			}
		case <-wait:
			s.tick(ctx, ls)
		}
		s.publishState(ls)
	}
}

func (s *Supervisor) publishState(ls *loopState) {
	snap := ls.Snapshot
	s.mu.Lock()
	changed := snapshotKey(snap) != snapshotKey(s.last)
	s.last = snap
	s.mu.Unlock()
	if changed {
		s.publish(snap)
	}
}

func snapshotKey(s Snapshot) string {
	running := ""
	if s.Running != nil {
		running = s.Running.String()
	}
	return fmt.Sprintf("%s|%s|%v|%d|%s|%v|%v|%s|%s|%v|%v|%q|%v|%s|%v|%s|%s|%s|%v|%v", s.State, s.Mode, s.Detection, s.Sessions, s.Reason, s.Reachable, s.HasStatus, s.Status.State, s.Status.Version, s.Status.Drift, s.Status.Config, s.Status.URLs, s.Settings, s.Blocker, s.Installed, s.Compat, running, s.RunningCompat, s.Degraded, tunnelKey(s.Status.Tunnel))
}

func tunnelKey(t *contract.TunnelStatus) string {
	if t == nil {
		return ""
	}
	return t.State + "|" + t.PublicURL + "|" + t.Error
}

func (s *Supervisor) nextWake(ls *loopState) <-chan time.Time {
	var earliest time.Time
	for _, t := range []time.Time{ls.nextStatus, ls.nextReach, ls.readyBy, ls.backoffAt, ls.watchAt} {
		if !t.IsZero() && (earliest.IsZero() || t.Before(earliest)) {
			earliest = t
		}
	}
	if earliest.IsZero() {
		return nil
	}
	return s.clock.After(max(0, earliest.Sub(s.clock.Now())))
}

func due(t, now time.Time) bool { return !t.IsZero() && !now.Before(t) }

func (s *Supervisor) tick(ctx context.Context, ls *loopState) {
	now := s.clock.Now()
	if due(ls.backoffAt, now) {
		ls.backoffAt = time.Time{}
		if ls.State == StateRestarting {
			s.start(ctx, ls)
		}
	}
	if due(ls.readyBy, now) {
		ls.readyBy = time.Time{}
		if ls.State == StateStarting {
			reason := ls.lastReason
			if reason == "" {
				reason = "连接被拒绝"
			}
			s.fail(ls, fmt.Sprintf("WebMux 未在 60 秒内就绪：%s（请查看日志）", reason))
			ls.nextStatus = now.Add(StatusInterval)
		}
	}
	if due(ls.nextStatus, now) {
		ls.nextStatus = time.Time{}
		s.refresh(ctx, ls)
	}
	if due(ls.nextReach, now) {
		ls.nextReach = time.Time{}
		s.checkReach(ctx, ls)
	}
	if due(ls.watchAt, now) {
		ls.watchAt = time.Time{}
		s.ensureWatch(ctx, ls)
	}
}

func (s *Supervisor) post(ev any) {
	select {
	case s.events <- ev:
	case <-s.done:
	}
}

func (s *Supervisor) refresh(ctx context.Context, ls *loopState) {
	if ls.statusBusy {
		return
	}
	ls.statusBusy = true
	gen := ls.gen
	go func() {
		status, err := s.service.Status(ctx)
		s.post(statusEvent{gen: gen, status: status, err: err})
	}()
}

func (s *Supervisor) checkReach(ctx context.Context, ls *loopState) {
	if ls.reachBusy || s.reach == nil || ls.Status.URLs.Local == "" {
		return
	}
	ls.reachBusy = true
	gen, url := ls.gen, ls.Status.URLs.Local
	go func() {
		s.post(reachEvent{gen: gen, err: s.reach(ctx, url)})
	}()
}

func (s *Supervisor) ensureWatch(ctx context.Context, ls *loopState) {
	if ls.watching {
		return
	}
	ls.watching = true
	watchCtx, stop := context.WithCancel(ctx)
	ls.watchStop = stop
	gen := ls.gen
	go func() {
		err := s.service.Watch(watchCtx, func(count int) { s.post(sessionsEvent{gen: gen, count: count}) })
		s.post(watchEndEvent{gen: gen, err: err})
	}()
}

func (s *Supervisor) stopWatch(ls *loopState) {
	if ls.watchStop != nil {
		ls.watchStop()
		ls.watchStop = nil
	}
	ls.watchAt = time.Time{}
}

func (s *Supervisor) fail(ls *loopState, reason string) {
	ls.State = StateFailed
	ls.Reason = reason
	s.logf("failed: %s", reason)
}

// block records a failure that needs the user to install or fix webmux.
func (s *Supervisor) block(ls *loopState, err error) {
	ls.Blocker = blockerOf(err)
	ls.Installed, ls.Compat = BuildInfo{}, ""
	s.fail(ls, err.Error())
}

func (s *Supervisor) recheck(ctx context.Context, ls *loopState) {
	if ls.checkBusy || ls.pendingOp {
		return
	}
	ls.checkBusy = true
	go func() {
		info, err := s.service.Check(ctx)
		s.post(checkEvent{info: info, err: err})
	}()
}

// checked classifies the installed build and logs both sides.
func (s *Supervisor) checked(ls *loopState, info BuildInfo) {
	ls.Installed = info
	ls.Compat = Classify(ls.Tray, info)
	s.logf("check: tray %s, WSL webmux %s: %s", ls.Tray, info, ls.Compat)
}

func (s *Supervisor) onCheck(ctx context.Context, ls *loopState, ev checkEvent) {
	if ev.err != nil {
		blocker := blockerOf(ev.err)
		if blocker == "" {
			s.logf("check: %v", ev.err)
			return
		}
		running := ls.HasStatus && ls.Status.State == contract.StateRunning
		if !running && (ls.State == StateStopped || ls.State == StateFailed) {
			s.block(ls, ev.err)
			return
		}
		// A running server keeps working; the menu offers the install.
		ls.Blocker = blocker
		ls.Installed, ls.Compat = BuildInfo{}, ""
		return
	}
	s.checked(ls, ev.info)
	if ls.Blocker == "" {
		return
	}
	ls.Blocker = ""
	if ls.State == StateFailed {
		ls.State = StateStopped
		ls.Reason = ""
	}
	s.logf("webmux %s is usable again", ev.info.Version)
	s.refresh(ctx, ls)
}

func (s *Supervisor) handle(ctx context.Context, ls *loopState, ev any, boot *bool) bool {
	switch ev := ev.(type) {
	case command:
		return s.command(ctx, ls, ev)
	case statusEvent:
		ls.statusBusy = false
		wasBoot := *boot
		*boot = false
		s.onStatus(ctx, ls, ev)
		if wasBoot && ls.Settings.StartWebMux && ls.State == StateStopped {
			s.start(ctx, ls) // checks webmux itself
		} else if wasBoot && ls.Blocker == "" {
			s.recheck(ctx, ls)
		}
	case checkEvent:
		ls.checkBusy = false
		s.onCheck(ctx, ls, ev)
	case reachEvent:
		ls.reachBusy = false
		if ev.gen != ls.gen {
			return false
		}
		s.onReach(ls, ev.err)
	case sessionsEvent:
		if ev.gen == ls.gen {
			ls.Sessions = ev.count
		}
	case watchEndEvent:
		if ev.gen != ls.gen {
			return false
		}
		ls.watching = false
		ls.watchStop = nil
		if ls.State == StateRunning || ls.State == StateUnreachable || ls.State == StateExternal || ls.State == StateFailed {
			s.logf("session subscription ended: %v", ev.err)
			s.refresh(ctx, ls)
			if ls.watchDelay == 0 {
				ls.watchDelay = WatchBackoffFirst
			} else {
				ls.watchDelay = min(2*ls.watchDelay, WatchBackoffMax)
			}
			ls.watchAt = s.clock.Now().Add(ls.watchDelay)
		}
	case exitEvent:
		if ev.gen != ls.gen || ls.proc == nil {
			return false
		}
		ls.proc = nil
		s.logf("tray-managed WebMux exited with status %d", ev.code)
		if !ls.userStop && ls.State != StateStopping {
			s.unexpectedExit(ctx, ls, fmt.Sprintf("退出码 %d", ev.code), ls.TunnelActive())
		}
	case startEvent:
		s.onStart(ctx, ls, ev)
	case stopEvent:
		s.onStop(ctx, ls, ev)
		return ls.quitting
	}
	return false
}

func (s *Supervisor) command(ctx context.Context, ls *loopState, c command) bool {
	switch c.kind {
	case cmdStart:
		switch ls.State {
		case StateStopped, StateFailed, StateRestarting:
			if ls.State != StateFailed || !ls.HasStatus || ls.Status.State != contract.StateRunning {
				ls.failures = nil
				s.start(ctx, ls)
			}
		}
	case cmdStop:
		if ls.Actions().Stop {
			s.stop(ctx, ls, nil)
		} else if ls.State == StateRestarting {
			ls.backoffAt = time.Time{}
			ls.userStop = true
			ls.State = StateStopped
			ls.Reason = ""
		}
	case cmdRestart:
		if ls.Actions().Restart {
			s.stop(ctx, ls, func() { ls.failures = nil; s.start(ctx, ls) })
		}
	case cmdRefresh:
		s.refresh(ctx, ls)
		if ls.State == StateRunning || ls.State == StateUnreachable {
			s.checkReach(ctx, ls)
		}
	case cmdRecheck:
		s.recheck(ctx, ls)
	case cmdSettings:
		old := ls.Settings
		ls.Settings = c.settings
		if ls.Mode == old.Mode || !ls.HasStatus || ls.Status.State != contract.StateRunning {
			ls.Mode = c.settings.Mode
		}
		targetChanged := old.Distro != c.settings.Distro || old.Mode != c.settings.Mode
		if targetChanged && ls.Actions().Stop {
			s.stop(ctx, ls, func() { ls.Mode = ls.Settings.Mode; ls.failures = nil; s.start(ctx, ls) })
		} else if old.Distro != c.settings.Distro {
			ls.gen++
			s.stopWatch(ls)
			ls.watching = false
			ls.HasStatus = false
			ls.Status = contract.StatusV1{}
			ls.Degraded = false
			clearRunning(ls)
			ls.State = StateStopped
			s.refresh(ctx, ls)
		}
		if old.Distro != c.settings.Distro || old.Webmux != c.settings.Webmux {
			// The verdict belonged to the previous binary.
			if ls.Blocker != "" && ls.State == StateFailed {
				ls.State, ls.Reason = StateStopped, ""
			}
			ls.Blocker, ls.Installed, ls.Compat = "", BuildInfo{}, ""
			s.recheck(ctx, ls)
		}
	case cmdQuit:
		finish := func() {
			ls.quitting = true
			close(c.reply)
		}
		if c.stop && ls.Actions().Stop {
			s.stop(ctx, ls, finish)
			return false
		}
		finish()
		return true
	}
	return false
}

// start resolves the mode, refuses to start a second instance, then launches.
func (s *Supervisor) start(ctx context.Context, ls *loopState) {
	if ls.pendingOp {
		return
	}
	ls.pendingOp = true
	ls.userStop = false
	ls.State = StateStarting
	ls.Reason = ""
	ls.lastReason = ""
	ls.backoffAt = time.Time{}
	ls.gen++
	gen := ls.gen
	mode := ls.Settings.Mode
	go func() {
		ev := startEvent{gen: gen}
		defer func() { s.post(ev) }()
		info, err := s.service.Check(ctx)
		if err != nil {
			b := blockerOf(err)
			ev.err, ev.fatal = err, b == BlockerMissing || b == BlockerIncompatible
			return
		}
		ev.checked, ev.info = true, info
		ev.detection = s.service.Detect(ctx)
		resolved, err := Resolve(mode, ev.detection)
		if err != nil {
			ev.err, ev.fatal = err, true
			return
		}
		ev.mode = resolved
		status, err := s.service.Status(ctx)
		var unsupported *ContractError
		if errors.As(err, &unsupported) {
			// Only Windows reachability tells whether it already runs.
			if unsupported.Local != "" && s.reach != nil && s.reach(ctx, unsupported.Local) == nil {
				ev.running, ev.degraded = true, unsupported
				return
			}
			status, err = contract.StatusV1{}, nil
		}
		if err != nil {
			ev.err = err
			return
		}
		ev.status = status
		switch {
		case status.State == contract.StateRunning:
			ev.running = true
			return
		case status.Health.Status == contract.HealthNotWebMux:
			ev.err, ev.fatal = fmt.Errorf("端口 %s 已被其他程序占用", status.Listen), true
			return
		}
		if resolved == ModeSystemd {
			if err := s.service.Keepalive(true); err != nil {
				s.logf("keepalive: %v", err)
			}
			ev.err = s.service.Systemctl(ctx, "start")
			return
		}
		ev.proc, ev.err = s.service.StartTray(ctx)
	}()
}

func (s *Supervisor) onStart(ctx context.Context, ls *loopState, ev startEvent) {
	ls.pendingOp = false
	if ev.gen != ls.gen {
		return
	}
	ls.Detection = ev.detection
	if ev.checked {
		ls.Blocker = ""
		s.checked(ls, ev.info)
	}
	if ev.mode != "" {
		ls.Mode = ev.mode
	}
	if ev.degraded != nil {
		// Reachability decides; the deadline covers it never answering.
		ls.readyBy = s.clock.Now().Add(ReadyTimeout)
		s.onStatus(ctx, ls, statusEvent{gen: ls.gen, err: ev.degraded})
		return
	}
	if ev.running {
		s.onStatus(ctx, ls, statusEvent{gen: ls.gen, status: ev.status})
		return
	}
	if ev.err != nil {
		s.logf("start failed: %v", ev.err)
		if b := blockerOf(ev.err); ev.fatal && (b == BlockerMissing || b == BlockerIncompatible) {
			s.block(ls, ev.err)
			return
		}
		if ev.fatal {
			s.fail(ls, ev.err.Error())
			return
		}
		// WSL still booting, for example: counts toward the backoff.
		s.failure(ls, ev.err.Error())
		return
	}
	ls.State = StateStarting
	ls.readyBy = s.clock.Now().Add(ReadyTimeout)
	ls.nextStatus = s.clock.Now().Add(StartingInterval)
	if ev.proc != nil {
		ls.proc = ev.proc
		gen, proc := ls.gen, ev.proc
		go func() {
			<-proc.Done()
			s.post(exitEvent{gen: gen, code: proc.ExitCode()})
		}()
	}
	s.logf("starting WebMux (%s)", ModeLabel(ls.Mode))
}

// failure records an unexpected exit or failed start and schedules the
// backoff restart in tray-managed mode.
func (s *Supervisor) failure(ls *loopState, reason string) {
	now := s.clock.Now()
	kept := ls.failures[:0]
	for _, at := range ls.failures {
		if now.Sub(at) < FailureWindow {
			kept = append(kept, at)
		}
	}
	ls.failures = append(kept, now)
	if ls.Mode == ModeSystemd || !ls.Settings.AutoRestart {
		s.fail(ls, reason)
		return
	}
	if len(ls.failures) >= MaxFailures {
		s.fail(ls, "多次启动失败，请查看日志（"+reason+"）")
		return
	}
	delay := BackoffFirst << (len(ls.failures) - 1)
	ls.State = StateRestarting
	ls.Reason = fmt.Sprintf("%s；%d 秒后重试", reason, int(min(delay, BackoffMax)/time.Second))
	ls.backoffAt = now.Add(min(delay, BackoffMax))
	s.logf("restart in %s after: %s", min(delay, BackoffMax), reason)
}

func (s *Supervisor) unexpectedExit(ctx context.Context, ls *loopState, detail string, tunnel bool) {
	s.stopWatch(ls)
	ls.watching = false
	ls.gen++
	ls.Sessions = 0
	clearRunning(ls)
	ls.readyBy = time.Time{}
	message := "WebMux 意外退出（" + detail + "），终端会话已丢失"
	if tunnel {
		message += "；公网隧道已关闭"
	}
	s.notify("WebMux 已退出", message)
	if ls.Mode == ModeSystemd {
		gen := ls.gen
		go func() {
			if s.service.UnitFailed(ctx) {
				s.post(statusEvent{gen: gen, err: errUnitFailed})
				return
			}
			s.post(statusEvent{gen: gen, status: contract.StatusV1{V: contract.V, State: contract.StateStopped}})
		}()
		ls.State = StateStopped
		ls.Reason = "WebMux 已退出：" + detail
		return
	}
	s.failure(ls, "WebMux 意外退出："+detail)
}

var errUnitFailed = errors.New("systemd 单元 webmux.service 处于失败状态")

func (s *Supervisor) onStatus(ctx context.Context, ls *loopState, ev statusEvent) {
	if ev.gen != ls.gen {
		return
	}
	now := s.clock.Now()
	if errors.Is(ev.err, errUnitFailed) {
		s.fail(ls, ev.err.Error())
		s.notify("WebMux 服务失败", ev.err.Error())
		return
	}
	var unsupported *ContractError
	if errors.As(ev.err, &unsupported) {
		s.onDegraded(ctx, ls, unsupported, now)
		return
	}
	if ev.err != nil && blockerOf(ev.err) != "" {
		s.block(ls, ev.err)
		return
	}
	defer s.schedule(ls, now)
	status := ev.status
	wasRunning := ls.HasStatus && ls.Status.State == contract.StateRunning
	tunnelWas := ls.TunnelActive()
	if ev.err == nil {
		ls.Status, ls.HasStatus, ls.Degraded = status, true, false
		if status.State == contract.StateRunning {
			setRunning(ls, BuildInfo{Version: status.Version, Contract: status.V})
		} else {
			clearRunning(ls)
		}
	}
	if t := status.Tunnel; ev.err == nil && tunnelWas && t != nil && t.State == contract.TunnelFailed {
		s.notify("公网隧道已断开", t.Error)
	}
	running := ev.err == nil && status.State == contract.StateRunning
	healthy := running && status.Health.Status == contract.HealthOK
	if ev.err != nil || (status.State == contract.StateUnresponsive) || (running && !healthy) {
		reason := describeStatusError(ev.err, status)
		ls.lastReason = reason
		switch ls.State {
		case StateRunning, StateUnreachable, StateExternal:
			ls.unhealthy++
			if ls.unhealthy >= UnhealthyLimit {
				s.fail(ls, "WebMux 无响应："+reason)
			}
		case StateStopped:
			if ls.HasStatus && status.State == contract.StateUnresponsive {
				s.fail(ls, "WebMux 无响应："+reason)
			}
		}
		return
	}
	ls.unhealthy = 0
	if running {
		mode, managed := ManagedMode(status, ls.Settings.Mode)
		if s.sessionsUnknown(ls) && status.Sessions != nil {
			ls.Sessions = *status.Sessions
		}
		if !managed {
			ls.State = StateExternal
			ls.Mode = ""
			ls.Reason = "外部实例：由外部管理"
		} else {
			ls.Mode = mode
			if ls.State != StateRunning && ls.State != StateUnreachable {
				s.logf("WebMux running (%s)", ModeLabel(mode))
				if mode == ModeSystemd {
					// Also after a tray restart or a unit started outside the tray.
					go func() {
						if err := s.service.Keepalive(true); err != nil {
							s.logf("keepalive: %v", err)
						}
					}()
				}
				ls.State = StateRunning
				ls.Reachable = true
				ls.watchDelay = 0
				s.checkReach(ctx, ls)
			}
			ls.readyBy = time.Time{}
			ls.Reason = s.hint(ls)
		}
		s.ensureWatch(ctx, ls)
		return
	}
	// Stopped.
	switch ls.State {
	case StateStarting:
		return // keep waiting until the process exits or the deadline passes
	case StateRunning, StateUnreachable, StateFailed:
		if wasRunning && !ls.userStop && ls.Mode != "" {
			s.unexpectedExit(ctx, ls, "实例已不再运行", tunnelWas)
			return
		}
		if ls.State == StateFailed {
			return // keep the failure until the user acts
		}
	case StateRestarting, StateStopping:
		return
	}
	ls.State = StateStopped
	ls.Sessions = 0
	ls.Mode = ls.Settings.Mode
	ls.Reason = ""
	if status.Health.Status == contract.HealthNotWebMux {
		s.fail(ls, fmt.Sprintf("端口 %s 已被其他程序占用", status.Listen))
	}
}

// onDegraded follows an instance whose status uses a contract the tray does
// not speak. Only reachability from Windows tells whether it runs, so the
// unparsed status never counts as unresponsive: it neither fails nor
// restarts the instance.
func (s *Supervisor) onDegraded(ctx context.Context, ls *loopState, e *ContractError, now time.Time) {
	defer s.schedule(ls, now)
	ls.lastReason = e.Error()
	ls.unhealthy = 0
	local := e.Local
	if local == "" {
		local = ls.Status.URLs.Local
	}
	var state contract.State
	if ls.Degraded && ls.HasStatus {
		state = ls.Status.State // what reachability last decided
	}
	if !ls.Degraded {
		s.logf("status: %v; following the instance by reachability", e)
		s.stopWatch(ls)
		ls.watching = false
		ls.Sessions = 0
	}
	ls.Degraded = true
	ls.Status = contract.StatusV1{V: e.V, Version: e.Version, State: state, URLs: contract.URLs{Local: local}}
	ls.HasStatus = true
	s.checkReach(ctx, ls)
}

// onDegradedReach turns reachability into the state of a degraded instance.
func (s *Supervisor) onDegradedReach(ls *loopState, err error) {
	if ls.State == StateStopping || ls.State == StateRestarting {
		return
	}
	now := s.clock.Now()
	if err != nil {
		// Starting waits for its deadline; stopped and failed stay.
		if ls.State == StateRunning || ls.State == StateUnreachable {
			s.logf("Windows cannot reach %s: %v", ls.Status.URLs.Local, err)
			ls.State = StateUnreachable
			ls.Reachable = false
			ls.Reason = s.hint(ls)
			ls.nextReach = now.Add(ReachInterval)
		}
		return
	}
	if ls.State != StateRunning {
		s.logf("WebMux %s running with an unsupported contract (%s)", ls.Status.Version, ModeLabel(ls.Mode))
	}
	ls.Status.State = contract.StateRunning
	setRunning(ls, BuildInfo{Version: ls.Status.Version, Contract: ls.Status.V})
	ls.State = StateRunning
	ls.Reachable = true
	ls.readyBy = time.Time{}
	ls.Reason = s.hint(ls)
	ls.nextReach = now.Add(ReachInterval)
}

func setRunning(ls *loopState, b BuildInfo) {
	ls.Running = &b
	ls.RunningCompat = Classify(ls.Tray, b)
}

func clearRunning(ls *loopState) { ls.Running, ls.RunningCompat = nil, "" }

// sessionsUnknown is true before the subscription reports a count.
func (s *Supervisor) sessionsUnknown(ls *loopState) bool { return !ls.watching || ls.Sessions == 0 }

func (s *Supervisor) hint(ls *loopState) string {
	var hints []string
	if len(ls.Status.Drift) > 0 {
		hints = append(hints, "配置已修改，重启 WebMux 后生效")
	}
	if !ls.Reachable && ls.Status.URLs.Local != "" {
		hints = append(hints, UnreachableHint(ls.Status.URLs.Local))
	}
	return strings.Join(hints, "；")
}

// UnreachableHint is the status line when WSL is healthy but Windows
// cannot reach the local link.
func UnreachableHint(local string) string {
	return "WebMux 已在 WSL 中运行，但 Windows 无法访问 " + strings.TrimPrefix(local, "http://") + "，请检查 WSL localhostForwarding / networkingMode"
}

func describeStatusError(err error, status contract.StatusV1) string {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "超时"
		}
		return err.Error()
	}
	switch status.Health.Status {
	case contract.HealthUnauthorized:
		return "Token 不匹配"
	case contract.HealthRefused:
		return "连接被拒绝"
	case contract.HealthTimeout:
		return "超时"
	}
	if status.Health.Message != "" {
		return status.Health.Message
	}
	return string(status.Health.Status)
}

func (s *Supervisor) schedule(ls *loopState, now time.Time) {
	switch ls.State {
	case StateStarting:
		ls.nextStatus = now.Add(StartingInterval)
	case StateRestarting, StateStopping:
	default:
		ls.nextStatus = now.Add(StatusInterval)
		if t := ls.Status.Tunnel; ls.HasStatus && t != nil && t.State == contract.TunnelStarting {
			ls.nextStatus = now.Add(StartingInterval)
		}
	}
	if (ls.State == StateRunning || ls.State == StateUnreachable) && ls.nextReach.IsZero() && !ls.reachBusy {
		ls.nextReach = now.Add(ReachInterval)
	}
}

func (s *Supervisor) onReach(ls *loopState, err error) {
	if ls.Degraded {
		s.onDegradedReach(ls, err)
		return
	}
	if ls.State != StateRunning && ls.State != StateUnreachable {
		return
	}
	ls.Reachable = err == nil
	if err != nil {
		s.logf("Windows cannot reach %s: %v", ls.Status.URLs.Local, err)
		ls.State = StateUnreachable
	} else {
		ls.State = StateRunning
	}
	ls.Reason = s.hint(ls)
	ls.nextReach = s.clock.Now().Add(ReachInterval)
}

// stop is the user stop: local API shutdown, then a forced stop after 15 s
// or when the API does not answer; systemd mode uses systemctl.
func (s *Supervisor) stop(ctx context.Context, ls *loopState, then func()) {
	if ls.pendingOp {
		return
	}
	ls.pendingOp = true
	ls.userStop = true
	ls.State = StateStopping
	ls.Reason = ""
	if then != nil {
		ls.after = append(ls.after, then)
	}
	s.stopWatch(ls)
	ls.watching = false
	ls.gen++
	gen, mode, proc := ls.gen, ls.Mode, ls.proc
	ls.proc = nil
	go func() {
		ev := stopEvent{gen: gen}
		defer func() { s.post(ev) }()
		if mode == ModeSystemd {
			ev.err = s.service.Systemctl(ctx, "stop")
			if err := s.service.Keepalive(false); err != nil {
				s.logf("keepalive: %v", err)
			}
			return
		}
		if err := s.service.Shutdown(ctx, false); err == nil && s.waitStopped(ctx, proc) {
			return
		}
		s.logf("WebMux did not stop within %s; forcing", StopTimeout)
		stop, err := s.service.ForceStop(ctx)
		ev.forced, ev.err = stop.Result == contract.StopKilled, err
	}()
}

func (s *Supervisor) waitStopped(ctx context.Context, proc Process) bool {
	deadline := s.clock.After(StopTimeout)
	var exited <-chan struct{}
	if proc != nil {
		exited = proc.Done()
	}
	for {
		select {
		case <-exited:
			return true
		case <-deadline:
			return false
		case <-s.clock.After(500 * time.Millisecond):
			status, err := s.service.Status(ctx)
			if err == nil && status.State == contract.StateStopped {
				return true
			}
			// A degraded instance has stopped once its link stops answering.
			var unsupported *ContractError
			if errors.As(err, &unsupported) && unsupported.Local != "" && s.reach != nil && s.reach(ctx, unsupported.Local) != nil {
				return true
			}
		}
	}
}

func (s *Supervisor) onStop(ctx context.Context, ls *loopState, ev stopEvent) {
	ls.pendingOp = false
	if ev.forced {
		s.logf("WebMux was forcibly killed")
	}
	if ev.err != nil {
		s.logf("stop: %v", ev.err)
	}
	ls.State = StateStopped
	ls.Sessions = 0
	ls.Mode = ls.Settings.Mode
	ls.HasStatus = false
	ls.Status = contract.StatusV1{}
	ls.Degraded = false
	clearRunning(ls)
	if ev.err != nil {
		ls.Reason = "停止失败：" + ev.err.Error()
	}
	s.refresh(ctx, ls)
	s.continueAfter(ls)
}

func (s *Supervisor) continueAfter(ls *loopState) {
	after := ls.after
	ls.after = nil
	for _, f := range after {
		f()
	}
}
