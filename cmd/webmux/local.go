package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/client"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/push"
	"github.com/rainchan/webmux/internal/runtime"
	"github.com/rainchan/webmux/internal/session"
)

// localRuntime is the runtime record and local API of the instance that
// holds the state directory's runtime lock. Any failure degrades to TCP only.
type localRuntime struct {
	dir      string
	lock     *runtime.Lock
	listener net.Listener
	server   *http.Server
	written  bool
}

// startRuntime acquires the lock, listens on the socket and writes the
// record. It must run after the TCP listener is bound and before it serves.
func startRuntime(ctx context.Context, c serverRun, addr net.Addr, deps api.Deps, shutdown func(), tunnels *tunnelController) *localRuntime {
	m, pushes := deps.Manager, deps.Push
	rt := &localRuntime{dir: runtime.Dir(c.StateDir)}
	if err := runtime.EnsureDir(rt.dir); err != nil {
		slog.Warn("runtime directory unavailable; runtime record and local API disabled", "error", err)
		return rt
	}
	lock, err := runtime.AcquireLock(rt.dir)
	if errors.Is(err, runtime.ErrLocked) {
		slog.Warn("状态目录已被另一实例使用，运行时记录未写入；local API disabled", "state_dir", c.StateDir)
		return rt
	}
	if err != nil {
		slog.Warn("runtime lock unavailable; runtime record and local API disabled", "error", err)
		return rt
	}
	rt.lock = lock
	identity, err := runtime.ProcessIdentity(os.Getpid())
	if err != nil {
		slog.Warn("process identity unavailable; runtime record and local API disabled", "error", err)
		return rt
	}
	record := runtime.Record{
		PID:              os.Getpid(),
		Identity:         identity,
		Version:          version,
		Listen:           addr.String(),
		ConfiguredListen: c.Listen,
		TokenConfigured:  c.Token != "",
		TokenFingerprint: runtime.TokenFingerprint(c.Token),
		TokenGenerated:   c.TokenGenerated,
		Launcher:         runtime.Launcher(os.Getenv),
		LogFile:          c.LogFile,
		StartedAt:        time.Now(),
	}
	if c.ConfigFound {
		record.ConfigPath = c.ConfigPath
	}
	if path, err := runtime.SocketPath(c.StateDir, os.Getenv); err != nil {
		slog.Warn("本机 API 不可用 (local API unavailable)", "error", err)
	} else if rt.listener, err = runtime.ListenSocket(path); err != nil {
		slog.Warn("本机 API 不可用 (local API unavailable)", "error", err)
	} else {
		record.Socket = path
		rt.server = &http.Server{
			Handler: api.NewLocal(deps, api.Local{
				Status:    func() contract.StatusV1 { return localStatus(record, c, m, tunnels, pushes) },
				Launcher:  record.Launcher,
				Shutdown:  shutdown,
				InSession: m.ContainsProcess,
				Tunnel:    tunnels.SetEnabled,
			}),
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
			BaseContext:       func(net.Listener) context.Context { return ctx },
			ConnContext:       api.LocalConnContext,
		}
	}
	if err := runtime.Write(rt.dir, record); err != nil {
		slog.Warn("runtime record not written", "error", err)
	} else {
		rt.written = true
	}
	return rt
}

func localStatus(record runtime.Record, c serverRun, m *session.Manager, tunnels *tunnelController, pushes *push.Service) contract.StatusV1 {
	s := client.FromRecord(record, client.NetInterfaces)
	s.State = contract.StateRunning
	s.LocalAPI = true
	sessions := len(m.List())
	s.Sessions = &sessions
	s.Health = contract.Health{Status: contract.HealthOK}
	client.ApplyConfig(&s, record, c.ConfigPath, c.ConfigExplicit)
	tunnel := tunnels.Status()
	s.Tunnel = &tunnel
	state := pushes.Status()
	s.Push = &contract.PushStatus{Enabled: state.Enabled, Available: state.Available, Error: state.Error, Subscriptions: state.Subscriptions}
	if host := c.Tunnel.NamedHostname(); host != "" {
		s.Push.PublicOrigin = "https://" + host
	}
	return s
}

// serve starts the local API; the listener was bound by startRuntime.
func (rt *localRuntime) serve() {
	if rt.server == nil {
		return
	}
	go func() {
		if err := rt.server.Serve(runtime.PeerListener(rt.listener)); err != nil && err != http.ErrServerClosed {
			slog.Warn("local API stopped", "error", err)
		}
	}()
}

// close runs after sessions are cleaned up: it stops the local API (which
// unlinks the socket), deletes the record and releases the lock.
func (rt *localRuntime) close(ctx context.Context) {
	if rt.server != nil {
		if err := rt.server.Shutdown(ctx); err != nil {
			_ = rt.server.Close()
		}
	} else if rt.listener != nil {
		_ = rt.listener.Close()
	}
	if rt.written {
		runtime.Remove(rt.dir, os.Getpid())
	}
	if rt.lock != nil {
		rt.lock.Release()
	}
}
