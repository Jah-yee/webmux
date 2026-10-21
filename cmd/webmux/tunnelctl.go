package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tunnel"
)

// tunnelController owns the Cloudflare Tunnel of a running server so the
// local API can start and stop it without a restart. Every launch gets a new
// generation; events of an older generation are discarded so a stopped
// cloudflared can never re-authorize its origin.
type tunnelController struct {
	cfg    config.TunnelConfig
	local  string // origin URL cloudflared forwards to
	token  string // effective WebMux token
	public *api.PublicOrigin
	locate func(context.Context) (string, error)
	out    io.Writer
	logger *slog.Logger

	mu     sync.Mutex
	state  string
	url    string
	reason string
	gen    uint64
	cancel context.CancelFunc
	done   chan struct{} // closed when the current generation's worker returns
	closed bool
}

func newTunnelController(cfg config.TunnelConfig, local, token string, public *api.PublicOrigin, locate func(context.Context) (string, error), out io.Writer, logger *slog.Logger) *tunnelController {
	return &tunnelController{cfg: cfg, local: local, token: token, public: public, locate: locate, out: out, logger: logger, state: contract.TunnelOff}
}

// SetEnabled implements api.Local.Tunnel.
func (tc *tunnelController) SetEnabled(enabled bool) (contract.TunnelStatus, error) {
	if !enabled {
		tc.Stop()
		return tc.Status(), nil
	}
	return tc.Start()
}

// Start launches the configured tunnel at runtime and returns at once. The
// public URL it prints carries no token: a service manager persists stdout.
func (tc *tunnelController) Start() (contract.TunnelStatus, error) {
	if len(tc.token) < minTunnelTokenLength {
		return tc.Status(), fmt.Errorf("a public tunnel requires a WebMux token of at least %d characters; run `webmux config init` or set a strong token, then restart WebMux", minTunnelTokenLength)
	}
	return tc.launch("", tc.announcePlain)
}

// startLocated runs the startup --tunnel with an already located binary and
// announces the token link and QR code, as the operator may not know a
// generated token.
func (tc *tunnelController) startLocated(binary string) {
	_, _ = tc.launch(binary, tc.announceWithToken)
}

// launch starts a new generation unless one is starting or ready. An empty
// binary is located first.
func (tc *tunnelController) launch(binary string, announce func(string)) (contract.TunnelStatus, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.closed {
		return tc.statusLocked(), errors.New("WebMux is shutting down")
	}
	if tc.state == contract.TunnelStarting || tc.state == contract.TunnelReady {
		return tc.statusLocked(), nil
	}
	tc.gen++
	// Stop and Close end the process explicitly, so it does not inherit the
	// server context, whose cancellation would read as an unexpected exit.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	tc.state, tc.url, tc.reason = contract.TunnelStarting, "", ""
	tc.cancel, tc.done = cancel, done
	go tc.run(ctx, tc.gen, binary, announce, done)
	return tc.statusLocked(), nil
}

func (tc *tunnelController) run(ctx context.Context, gen uint64, binary string, announce func(string), done chan struct{}) {
	defer close(done)
	if binary == "" {
		path, err := tc.locate(ctx)
		if err != nil {
			tc.fail(gen, fmt.Errorf("cloudflare tunnel: %w", err))
			return
		}
		binary = path
	}
	tun, err := tunnel.Start(ctx, tunnel.Options{Binary: binary, Local: tc.local, Token: tc.cfg.Token, Hostname: tc.cfg.Hostname, Logger: tc.logger})
	if err != nil {
		tc.fail(gen, err)
		return
	}
	defer tun.Close()
	for {
		select {
		case origin := <-tun.Ready():
			if tc.ready(gen, origin) {
				announce(origin)
			}
		case <-tun.Done():
			err := tun.Err()
			if err == nil {
				err = errors.New("cloudflared exited")
			}
			tc.fail(gen, err)
			return
		}
	}
}

// ready authorizes origin if gen is still current.
func (tc *tunnelController) ready(gen uint64, origin string) bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if gen != tc.gen {
		return false
	}
	if err := tc.public.Set(origin); err != nil {
		tc.logger.Error("cloudflare tunnel reported an unusable origin", "error", err)
		return false
	}
	tc.state, tc.url = contract.TunnelReady, origin
	return true
}

// fail revokes the public origin if gen is still current. It never retries.
func (tc *tunnelController) fail(gen uint64, err error) {
	tc.mu.Lock()
	current := gen == tc.gen
	if current {
		tc.public.Clear()
		tc.state, tc.url, tc.reason = contract.TunnelFailed, "", err.Error()
	}
	tc.mu.Unlock()
	if current {
		tc.logger.Error("cloudflare tunnel stopped; public URL revoked, local access continues", "error", err)
	}
}

// Stop revokes the public origin first, then ends cloudflared and waits for
// it. Stopping an idle controller has no effect.
func (tc *tunnelController) Stop() {
	tc.mu.Lock()
	tc.gen++
	tc.public.Clear()
	tc.state, tc.url, tc.reason = contract.TunnelOff, "", ""
	cancel, done := tc.cancel, tc.done
	tc.cancel, tc.done = nil, nil
	tc.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

// Close stops the tunnel for good; later starts are refused.
func (tc *tunnelController) Close() {
	tc.mu.Lock()
	tc.closed = true
	tc.mu.Unlock()
	tc.Stop()
}

func (tc *tunnelController) Status() contract.TunnelStatus {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.statusLocked()
}

func (tc *tunnelController) statusLocked() contract.TunnelStatus {
	s := contract.TunnelStatus{State: tc.state, Named: tc.cfg.Named()}
	switch tc.state {
	case contract.TunnelReady:
		s.PublicURL = tc.url
	case contract.TunnelFailed:
		s.Error = tc.reason
	}
	return s
}

func (tc *tunnelController) announcePlain(origin string) {
	_, _ = fmt.Fprintln(tc.out, "WebMux public: "+origin)
}

func (tc *tunnelController) announceWithToken(origin string) {
	link := accessURL(origin, tc.token)
	code, err := renderQR(link)
	if err != nil {
		tc.logger.Error("render QR code", "error", err)
	}
	_, _ = fmt.Fprint(tc.out, "WebMux public: "+link+"\n"+code)
}
