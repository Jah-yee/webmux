package tray

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rainchan/webmux/internal/contract"
)

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
