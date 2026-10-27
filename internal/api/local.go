package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/runtime"
)

// Local supplies the server-side state behind /local/v1/*.
type Local struct {
	Configuration func() contract.Configuration
	// Status describes the running instance; record and localApi are true.
	Status func() contract.StatusV1
	// Launcher is the recorded launcher; systemd and launchd refuse an
	// unforced shutdown.
	Launcher string
	// Shutdown takes the SIGTERM path. It runs after the 202 is flushed.
	Shutdown func()
	// InSession reports whether a peer PID runs inside a WebMux session.
	InSession func(pid int) bool
	// Tunnel starts (true) or stops (false) the Cloudflare Tunnel and returns
	// its state at once. Enable preconditions answer 409; storage failures
	// answer 503, including an emergency stop that was not persisted.
	// Nil leaves /local/v1/tunnel unrouted.
	Tunnel func(enabled bool) (contract.TunnelStatus, error)
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
func NewLocal(d Deps, local Local) http.Handler {
	c := d.Config
	d.Public, d.Tunnel = nil, nil
	if local.Status != nil {
		d.Tunnel = func() contract.TunnelStatus {
			if status := local.Status(); status.Tunnel != nil {
				return *status.Tunnel
			}
			return contract.TunnelStatus{State: contract.TunnelOff}
		}
	}
	s := newServer(d)
	s.local = true
	routes := s.routes()
	mux := http.NewServeMux()
	if local.Configuration != nil {
		mux.HandleFunc("GET "+contract.PathConfiguration, func(w http.ResponseWriter, r *http.Request) { reply(w, 200, local.Configuration()) })
	}
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
		if hint := runtime.ManagerHint(local.Launcher); hint != "" && !req.Force {
			failure(w, http.StatusConflict, fmt.Errorf("WebMux is managed by %s; use `%s` (or --force)", local.Launcher, hint))
			return
		}
		reply(w, http.StatusAccepted, contract.Ack{OK: true})
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
	if local.Tunnel != nil {
		mux.HandleFunc("POST "+contract.PathTunnel, func(w http.ResponseWriter, r *http.Request) {
			if local.fromSession(r) {
				failure(w, http.StatusForbidden, errors.New("processes inside a WebMux session cannot control the tunnel"))
				return
			}
			var req contract.TunnelRequest
			if !decode(w, r, &req) {
				return
			}
			if req.Enabled == nil {
				failure(w, http.StatusBadRequest, errors.New("enabled is required"))
				return
			}
			status, err := local.Tunnel(*req.Enabled)
			if err != nil {
				if tunnelStatus(err) == http.StatusInternalServerError {
					// Preserve the existing local callback's refusal contract.
					failure(w, http.StatusConflict, err)
				} else {
					tunnelFailure(w, err)
				}
				return
			}
			reply(w, http.StatusAccepted, status)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer baseHeaders(w)()
		if strings.HasPrefix(r.URL.Path, "/local/") {
			mux.ServeHTTP(w, r)
			return
		}
		if (r.URL.Path == "/api/tunnel" || strings.HasPrefix(r.URL.Path, "/api/tunnel/")) && local.fromSession(r) {
			failure(w, http.StatusForbidden, errors.New("processes inside a WebMux session cannot control the tunnel"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/tailscale/") && local.fromSession(r) {
			failure(w, http.StatusForbidden, errors.New("processes inside a WebMux session cannot change Tailscale settings"))
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
