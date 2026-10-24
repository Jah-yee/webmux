package tray

import (
	"context"
	"errors"
	"slices"
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
