package tray

import (
	"context"
	"fmt"
	"time"

	"github.com/rainchan/webmux/internal/contract"
)

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
