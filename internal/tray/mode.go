package tray

import (
	"context"
	"errors"
	"strings"

	"github.com/rainchan/webmux/internal/contract"
)

// Detection is what the target distribution offers for systemd mode.
type Detection struct {
	// Systemd reports systemd as PID 1.
	Systemd bool
	// Unit reports an installed webmux user unit.
	Unit bool
}

// Reason explains why systemd mode is unavailable, or "".
func (d Detection) Reason() string {
	switch {
	case !d.Systemd:
		return "发行版未启用 systemd"
	case !d.Unit:
		return "未安装 webmux 用户单元"
	}
	return ""
}

// DetectSystemd checks PID 1 and the webmux user unit.
func DetectSystemd(ctx context.Context, r Runner, t Target) Detection {
	var d Detection
	inv := t.InitComm()
	if res, err := r.Run(ctx, inv.Argv, inv.Env); err == nil && res.Code == 0 && strings.TrimSpace(DecodeWSLOutput(res.Stdout)) == "systemd" {
		d.Systemd = true
	}
	if !d.Systemd {
		return d
	}
	inv = t.SystemdUnit()
	if res, err := r.Run(ctx, inv.Argv, inv.Env); err == nil && res.Code == 0 {
		d.Unit = true
	}
	return d
}

// ErrSystemdUnavailable means systemd mode was chosen explicitly but the
// distribution does not support it; the tray never falls back silently.
var ErrSystemdUnavailable = errors.New("systemd 方式不可用")

// Resolve picks the concrete mode for one start.
func Resolve(mode Mode, d Detection) (Mode, error) {
	switch mode {
	case ModeTray:
		return ModeTray, nil
	case ModeSystemd:
		if reason := d.Reason(); reason != "" {
			return "", errors.Join(ErrSystemdUnavailable, errors.New(reason))
		}
		return ModeSystemd, nil
	}
	if d.Reason() == "" {
		return ModeSystemd, nil
	}
	return ModeTray, nil
}

// ManagedMode reports which mode controls a running instance under the
// configured mode, or ok=false for an external instance: no runtime record,
// or a launcher that does not match.
func ManagedMode(status contract.StatusV1, configured Mode) (Mode, bool) {
	if !status.Record {
		return "", false
	}
	switch {
	case status.Launcher == contract.LauncherWindowsTray && configured != ModeSystemd:
		return ModeTray, true
	case status.Launcher == contract.LauncherSystemd && configured != ModeTray:
		return ModeSystemd, true
	}
	return "", false
}

// ModeLabel is the status-line name of a mode.
func ModeLabel(mode Mode) string {
	switch mode {
	case ModeSystemd:
		return "systemd"
	case ModeTray:
		return "托盘托管"
	}
	return "自动"
}
