package tray

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rainchan/webmux/internal/contract"
)

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
