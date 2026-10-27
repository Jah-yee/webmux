package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tunnel"
	"github.com/rainchan/webmux/internal/tunnelsettings"
)

// tunnelController serializes persisted intent and generation changes. Each
// worker joins its predecessor before launching: even rapid updates cannot
// leave two connectors running or let stale events authorize an origin.
type tunnelController struct {
	cfg                                 config.TunnelConfig
	store                               *tunnelsettings.Service
	web                                 bool
	enabledSource, defaultEnabledSource string
	local, token                        string
	public                              *api.PublicOrigins
	locate                              func(context.Context) (string, error)
	out                                 io.Writer
	logger                              *slog.Logger

	mu                 sync.Mutex
	state, url, reason string
	gen                uint64
	cancel             context.CancelFunc
	done               chan struct{}
	closed             bool
}

// tunnelControllerOptions are the launch-time inputs for a tunnelController.
type tunnelControllerOptions struct {
	Cfg    config.TunnelConfig
	Store  *tunnelsettings.Service
	Local  string
	Token  string
	Public *api.PublicOrigins
	Locate func(context.Context) (string, error)
	Out    io.Writer
	Logger *slog.Logger
}

func newTunnelController(o tunnelControllerOptions) *tunnelController {
	_, web := o.Store.Effective()
	return &tunnelController{cfg: o.Cfg, store: o.Store, web: web, local: o.Local, token: o.Token, public: o.Public, locate: o.Locate, out: o.Out, logger: o.Logger, state: contract.TunnelOff}
}

func (tc *tunnelController) enableErrorLocked() error {
	if len(tc.token) < minTunnelTokenLength {
		return fmt.Errorf("%w: a public tunnel requires a WebMux token of at least %d characters; run `webmux config init` or set a strong token, then restart WebMux", tunnelsettings.ErrEnableBlocked, minTunnelTokenLength)
	}
	return nil
}

func (tc *tunnelController) SetEnabled(enabled bool) (contract.TunnelStatus, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.closed {
		return tc.statusLocked(), errors.New("WebMux is shutting down")
	}
	if enabled {
		if err := tc.store.Unavailable(); err != nil {
			return tc.statusLocked(), err
		}
		if err := tc.enableErrorLocked(); err != nil {
			return tc.statusLocked(), err
		}
	}
	next := tc.cfg
	next.Enabled = enabled
	err := tc.store.Save(next)
	if err != nil && enabled {
		return tc.statusLocked(), err
	}
	if err == nil {
		tc.web = true
		tc.enabledSource = "web"
	}
	wasEnabled := tc.cfg.Enabled
	tc.cfg = next
	if !enabled || !wasEnabled || (tc.state != contract.TunnelStarting && tc.state != contract.TunnelReady) {
		tc.applyLocked("", tc.announcePlain)
	}
	if err != nil {
		return tc.statusLocked(), fmt.Errorf("%w: %w", tunnelsettings.ErrNotPersisted, err)
	}
	return tc.statusLocked(), nil
}

func (tc *tunnelController) Settings() contract.TunnelSettings {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.settingsLocked()
}

func (tc *tunnelController) settingsLocked() contract.TunnelSettings {
	mode, source := "quick", "config"
	if tc.cfg.Named() {
		mode = "named"
	}
	if tc.web {
		source = "web"
	}
	view := contract.TunnelSettings{Enabled: tc.cfg.Enabled, Mode: mode, Hostname: tc.cfg.NamedHostname(), TokenConfigured: tc.cfg.Named(), Source: source, State: tc.state, PublicURL: tc.url, Error: tc.reason, CanEnable: true, StorageAvailable: true}
	if err := tc.enableErrorLocked(); err != nil {
		view.CanEnable, view.EnableBlockedReason = false, err.Error()
	}
	if err := tc.store.Unavailable(); err != nil {
		view.StorageAvailable, view.StorageError = false, err.Error()
		view.CanEnable, view.EnableBlockedReason = false, err.Error()
	}
	return view
}

func (tc *tunnelController) UpdateSettings(in contract.TunnelSettingsUpdate) (contract.TunnelSettings, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.closed {
		return tc.settingsLocked(), errors.New("WebMux is shutting down")
	}
	if err := tc.store.Unavailable(); err != nil {
		return tc.settingsLocked(), err
	}
	next, err := tunnelsettings.Prepare(tc.cfg, in.Mode, in.Hostname, in.Token)
	if err != nil {
		return tc.settingsLocked(), err
	}
	if err := tc.store.Save(next); err != nil {
		return tc.settingsLocked(), err
	}
	changed := tc.cfg.Token != next.Token || tc.cfg.Hostname != next.Hostname
	tc.cfg, tc.web = next, true
	tc.enabledSource = "web"
	if next.Enabled && changed {
		tc.applyLocked("", tc.announcePlain)
	}
	return tc.settingsLocked(), nil
}

func (tc *tunnelController) ResetSettings() (contract.TunnelSettings, error) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.closed {
		return tc.settingsLocked(), errors.New("WebMux is shutting down")
	}
	next, err := tc.store.Reset()
	if err != nil {
		return tc.settingsLocked(), err
	}
	// Reset never exposes the current process; the TOML switch applies on
	// next startup. The returned view reports current process intent.
	next.Enabled = false
	tc.cfg, tc.web = next, false
	tc.enabledSource = tc.defaultEnabledSource
	tc.applyLocked("", tc.announcePlain)
	return tc.settingsLocked(), nil
}

// startLocated applies startup intent without writing a command-line override.
// Empty binary uses the web-managed, asynchronous locator path.
func (tc *tunnelController) startLocated(binary string) {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.closed || !tc.cfg.Enabled {
		return
	}
	announce := tc.announcePlain
	if binary != "" {
		announce = tc.announceWithToken
	}
	tc.applyLocked(binary, announce)
}

// applyLocked immediately revokes authorization and updates observable state.
// The request never waits for process shutdown, location or startup.
func (tc *tunnelController) applyLocked(binary string, announce func(string)) {
	tc.gen++
	tc.public.Clear("cloudflare")
	if tc.cancel != nil {
		tc.cancel()
	}
	previous := tc.done
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	tc.cancel, tc.done = cancel, done
	tc.state, tc.url, tc.reason = contract.TunnelOff, "", ""
	cfg, gen := tc.cfg, tc.gen
	if cfg.Enabled {
		tc.state = contract.TunnelStarting
		if err := tc.enableErrorLocked(); err != nil {
			tc.state, tc.reason = contract.TunnelFailed, err.Error()
			cfg.Enabled = false
			tc.logger.Warn("cloudflare tunnel disabled by login token prerequisite; local access continues", "error", err)
		}
	}
	go func() {
		defer close(done)
		if previous != nil {
			<-previous
		}
		if ctx.Err() != nil || !cfg.Enabled {
			return
		}
		tc.run(ctx, gen, cfg, binary, announce)
	}()
}

func (tc *tunnelController) run(ctx context.Context, gen uint64, cfg config.TunnelConfig, binary string, announce func(string)) {
	if binary == "" {
		path, err := tc.locate(ctx)
		if err != nil {
			tc.fail(gen, fmt.Errorf("cloudflare tunnel: %w", err), cfg.Token)
			return
		}
		binary = path
	}
	if ctx.Err() != nil {
		return
	}
	tun, err := tunnel.Start(ctx, tunnel.Options{Binary: binary, Local: tc.local, Token: cfg.Token, Hostname: cfg.Hostname, Logger: tc.logger})
	if err != nil {
		tc.fail(gen, err, cfg.Token)
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
			tc.fail(gen, err, cfg.Token)
			return
		case <-ctx.Done():
			return
		}
	}
}

func (tc *tunnelController) ready(gen uint64, origin string) bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if gen != tc.gen || !tc.cfg.Enabled || tc.closed {
		return false
	}
	if err := tc.public.Set("cloudflare", origin); err != nil {
		tc.logger.Error("cloudflare tunnel reported an unusable origin")
		return false
	}
	tc.state, tc.url = contract.TunnelReady, origin
	return true
}

func (tc *tunnelController) fail(gen uint64, err error, credential string) {
	reason := err.Error()
	if credential != "" {
		reason = strings.ReplaceAll(reason, credential, "[redacted]")
	}
	tc.mu.Lock()
	current := gen == tc.gen && !tc.closed
	if current {
		tc.public.Clear("cloudflare")
		tc.state, tc.url, tc.reason = contract.TunnelFailed, "", reason
	}
	tc.mu.Unlock()
	if current {
		tc.logger.Error("cloudflare tunnel stopped; public URL revoked, local access continues", "error", reason)
	}
}

// Close joins the entire worker chain and never modifies persisted intent.
func (tc *tunnelController) Close() {
	tc.mu.Lock()
	tc.closed = true
	tc.gen++
	tc.public.Clear("cloudflare")
	tc.state, tc.url, tc.reason = contract.TunnelOff, "", ""
	if tc.cancel != nil {
		tc.cancel()
	}
	done := tc.done
	tc.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (tc *tunnelController) Status() contract.TunnelStatus {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.statusLocked()
}

func (tc *tunnelController) statusLocked() contract.TunnelStatus {
	enabled := tc.cfg.Enabled
	return contract.TunnelStatus{State: tc.state, Named: tc.cfg.Named(), PublicURL: tc.url, Error: tc.reason, Enabled: &enabled, SettingsAvailable: true}
}

func (tc *tunnelController) announcePlain(origin string) {
	_, _ = fmt.Fprintln(tc.out, "WebMux public: "+origin)
}

func (tc *tunnelController) announceWithToken(origin string) {
	link := contract.AccessURL(origin, tc.token)
	code, err := renderQR(link)
	if err != nil {
		tc.logger.Error("render QR code", "error", err)
	}
	_, _ = fmt.Fprint(tc.out, "WebMux public: "+link+"\n"+code)
}
