package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/files"
	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/workspace"
)

// Services report transport-neutral error kinds; this file is the only place
// that turns them into HTTP statuses.

func cancelled(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// workspaceStatus maps workspace error kinds; cancellation is a timeout and
// anything else a server error.
func workspaceStatus(err error) int {
	switch {
	case errors.Is(err, workspace.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, workspace.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, workspace.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, workspace.ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, workspace.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, workspace.ErrTimeout), cancelled(err):
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// codeStatus maps code view error kinds. Missing files are 404, and the
// workspace size and timeout failures it surfaces keep their statuses.
func codeStatus(err error) int {
	switch {
	case errors.Is(err, codeview.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, codeview.ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, codeview.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, codeview.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, codeview.ErrGone):
		return http.StatusGone
	case errors.Is(err, codeview.ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, codeview.ErrBusy):
		return http.StatusTooManyRequests
	case errors.Is(err, codeview.ErrInternal):
		return http.StatusInternalServerError
	case errors.Is(err, codeview.ErrUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, codeview.ErrTimeout), cancelled(err):
		return http.StatusGatewayTimeout
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	case errors.Is(err, workspace.ErrTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, workspace.ErrTimeout):
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// launchStatus maps launch store errors to HTTP statuses; anything else is a
// failed save.
func launchStatus(err error) int {
	switch {
	case errors.Is(err, launch.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, launch.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, launch.ErrDuplicate):
		return http.StatusConflict
	case errors.Is(err, launch.ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// launchFailure answers err. Store errors name fields and paths, never
// environment values.
func launchFailure(w http.ResponseWriter, err error) {
	status := launchStatus(err)
	if status == http.StatusInternalServerError {
		slog.Error("launch config save failed", "error", err)
	}
	failure(w, status, err)
}

// filesFailure answers a files service error. A failed read of the upload
// body keeps the request-body classification.
func filesFailure(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, files.ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, files.ErrTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, files.ErrRead):
		status = uploadReadStatus(err)
	}
	failure(w, status, err)
}

// sessionFailure answers a session start: a saved launch config keeps its
// launch statuses, everything else is a workspace error.
func sessionFailure(w http.ResponseWriter, err error) {
	if errors.Is(err, launch.ErrInvalid) || errors.Is(err, launch.ErrNotFound) || errors.Is(err, launch.ErrDuplicate) || errors.Is(err, launch.ErrUnavailable) {
		launchFailure(w, err)
		return
	}
	failure(w, workspaceStatus(err), err)
}

func workspaceFailure(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	failure(w, workspaceStatus(err), err)
	return true
}
