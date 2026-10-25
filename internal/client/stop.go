package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"syscall"
	"time"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/runtime"
)

// DefaultStopTimeout is how long stop waits before SIGKILL.
const DefaultStopTimeout = 15 * time.Second

// Stop implements `webmux stop`. Without a live record and local API it
// never signals anything. It returns the result and exit code.
func Stop(ctx context.Context, t Target, force bool, timeout time.Duration) (contract.StopV1, int) {
	if t.ConfigErr != nil {
		return stopResult(contract.StopError, "configuration: "+t.ConfigErr.Error())
	}
	local := t.LocalClient()
	defer local.Close()
	statusCtx, cancel := context.WithTimeout(ctx, RequestTimeout)
	status, err := local.Status(statusCtx)
	cancel()
	if err == nil && status.PID != nil {
		identity, idErr := runtime.ProcessIdentity(*status.PID)
		shutdownCtx, cancel := context.WithTimeout(ctx, RequestTimeout)
		err = local.Shutdown(shutdownCtx, force)
		cancel()
		var apiErr *localclient.APIError
		switch {
		case err == nil && idErr == nil:
			return wait(ctx, *status.PID, identity, timeout)
		case err == nil:
			return stopResult(contract.StopStopped, "stopped")
		case errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict:
			return stopResult(contract.StopRefused, apiErr.Message)
		case errors.As(err, &apiErr):
			return stopResult(contract.StopError, apiErr.Message)
		}
		// The API stopped answering mid-request: fall back to signals.
	}
	if t.Record != nil {
		if hint := runtime.ManagerHint(t.Record.Launcher); hint != "" && !force {
			return stopResult(contract.StopRefused, fmt.Sprintf("WebMux is managed by %s; use `%s` (or --force)", t.Record.Launcher, hint))
		}
		if err := t.Record.Alive(); err != nil {
			return stopResult(contract.StopNotRunning, "not running")
		}
		if err := syscall.Kill(t.Record.PID, syscall.SIGTERM); err != nil {
			return stopResult(contract.StopError, fmt.Sprintf("signal PID %d: %v", t.Record.PID, err))
		}
		return wait(ctx, t.Record.PID, t.Record.Identity, timeout)
	}
	health, _ := ProbeConfigured(ctx, t)
	switch health.Status {
	case contract.HealthOK, contract.HealthUnauthorized:
		return stopResult(contract.StopError, "a WebMux on "+t.Config.Listen+" has no runtime record or local API (an older version or another state directory); no stoppable instance found")
	}
	return stopResult(contract.StopNotRunning, "not running")
}

// wait polls until the identified process is gone, then SIGKILLs it.
func wait(ctx context.Context, pid int, identity runtime.Identity, timeout time.Duration) (contract.StopV1, int) {
	record := runtime.Record{PID: pid, Identity: identity}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		if record.Alive() != nil {
			return stopResult(contract.StopStopped, "stopped")
		}
		select {
		case <-ctx.Done():
			return stopResult(contract.StopError, ctx.Err().Error())
		case <-ticker.C:
		}
	}
	if record.Alive() != nil {
		return stopResult(contract.StopStopped, "stopped")
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return stopResult(contract.StopKilled, fmt.Sprintf("PID %d did not exit within %s and was killed", pid, timeout))
}

func stopResult(result, message string) (contract.StopV1, int) {
	code := contract.ExitOK
	if result == contract.StopRefused || result == contract.StopError {
		code = contract.ExitError
	}
	return contract.StopV1{V: contract.V, Result: result, Message: message}, code
}
