package tray

import (
	"context"
	"fmt"
	"time"

	"github.com/rainchan/webmux/internal/contract"
)

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
