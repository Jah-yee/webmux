package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/runtime"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

// Local supplies the server-side state behind /local/v1/*.
type Local struct {
	// Status describes the running instance; record and localApi are true.
	Status func() contract.StatusV1
	// Launcher is the recorded launcher; systemd and launchd refuse an
	// unforced shutdown.
	Launcher string
	// Shutdown takes the SIGTERM path. It runs after the 202 is flushed.
	Shutdown func()
	// InSession reports whether a peer PID runs inside a WebMux session.
	InSession func(pid int) bool
}

type peerKey struct{}

// LocalConnContext stores the peer PID of a runtime.PeerConn; use it as the
// local http.Server's ConnContext.
func LocalConnContext(ctx context.Context, c net.Conn) context.Context {
	if peer, ok := c.(*runtime.PeerConn); ok {
		return context.WithValue(ctx, peerKey{}, peer.PID)
	}
	return ctx
}

// NewLocal builds the Unix-socket handler. The socket's peer-UID check is
// the authentication, and browsers cannot reach a Unix socket, so token,
// Host and Origin checks are skipped. Routes match the TCP handler, plus
// /local/v1/*.
func NewLocal(c config.Config, m *session.Manager, workspaces *workspace.Service, codes *codeview.Service, local Local) http.Handler {
	var shared []*codeview.Service
	if codes != nil {
		shared = append(shared, codes)
	}
	s := newServer(c, m, workspaces, nil, shared)
	routes := s.routes()
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+contract.PathStatus, func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, local.Status())
	})
	mux.HandleFunc("POST "+contract.PathShutdown, func(w http.ResponseWriter, r *http.Request) {
		if local.fromSession(r) {
			failure(w, http.StatusForbidden, errors.New("processes inside a WebMux session cannot stop the server"))
			return
		}
		var req contract.ShutdownRequest
		if !decodeOptional(w, r, &req) {
			return
		}
		if hint := ManagerHint(local.Launcher); hint != "" && !req.Force {
			failure(w, http.StatusConflict, fmt.Errorf("WebMux is managed by %s; use `%s` (or --force)", local.Launcher, hint))
			return
		}
		reply(w, http.StatusAccepted, map[string]bool{"ok": true})
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		local.Shutdown()
	})
	mux.HandleFunc("GET "+contract.PathToken, func(w http.ResponseWriter, r *http.Request) {
		if local.fromSession(r) {
			failure(w, http.StatusForbidden, errors.New("processes inside a WebMux session cannot read the token"))
			return
		}
		if c.Token == "" {
			failure(w, http.StatusNotFound, errors.New("no token configured"))
			return
		}
		reply(w, http.StatusOK, contract.TokenResponse{Token: c.Token})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer baseHeaders(w)()
		if strings.HasPrefix(r.URL.Path, "/local/") {
			mux.ServeHTTP(w, r)
			return
		}
		routes.ServeHTTP(w, r)
	})
}

// decodeOptional accepts an empty body as the zero value.
func decodeOptional(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		failure(w, http.StatusBadRequest, err)
		return false
	}
	if len(data) == 0 {
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	return decode(w, r, v)
}

// fromSession fails closed: a peer without a known PID counts as inside.
func (l Local) fromSession(r *http.Request) bool {
	pid, ok := r.Context().Value(peerKey{}).(int)
	if !ok || pid <= 0 {
		return true
	}
	return l.InSession != nil && l.InSession(pid)
}

// ManagerHint is the stop command of a service manager, or "".
func ManagerHint(launcher string) string {
	switch launcher {
	case contract.LauncherSystemd:
		return "systemctl --user stop webmux"
	case contract.LauncherLaunchd:
		return fmt.Sprintf("launchctl bootout gui/%d/com.webmux", os.Getuid())
	}
	return ""
}
