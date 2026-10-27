package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/statefile"
	"github.com/rainchan/webmux/internal/tailscale"
	"github.com/rainchan/webmux/internal/tailscalesettings"
	"github.com/rainchan/webmux/internal/wire"
)

const (
	tailscaleStateFile    = "tailscale-ownership.json"
	tailscaleLockFile     = "tailscale-ownership.json.lock"
	tailscaleStateVersion = 1
)

// tailscaleState records this instance's exact entry point so a crash can be
// reconciled on the next start. It is never used to claim someone else's entry.
type tailscaleState struct {
	V        int    `json:"v"`
	Provider string `json:"provider"`
	DNSName  string `json:"dnsName"`
	Mode     string `json:"mode"`
	Port     int    `json:"port"`
	Target   string `json:"target"`
	Status   string `json:"status"`
	Public   bool   `json:"public"`
}

type tailscaleController struct {
	svc            *tailscale.Service
	store          *tailscalesettings.Service
	public         *api.PublicOrigins
	dir            string
	path           string
	lockPath       string
	token          string
	tokenGenerated bool
	out            io.Writer
	logger         *slog.Logger
	isWSL          func() bool
	networkingMode func(context.Context) string

	// opMu serializes settings updates, resets and Close.
	opMu sync.Mutex

	mu            sync.Mutex
	cfg           config.TailscaleConfig
	web           bool
	state         string
	url           string
	reason        string
	blocked       string
	listenBlocked string
	record        *tailscaleState
	storageErr    error
	ownership     *statefile.Handle[tailscaleState]
	localPort     int
	closed        bool
	baseCtx       context.Context
	cancel        context.CancelFunc
	done          chan struct{}
}

// tailscaleControllerOptions are the launch-time inputs; Store supplies the
// effective settings and a nil Store keeps Cfg and rejects web updates.
type tailscaleControllerOptions struct {
	Cfg            config.TailscaleConfig
	Store          *tailscalesettings.Service
	Service        *tailscale.Service
	StateDir       string
	Public         *api.PublicOrigins
	Token          string
	TokenGenerated bool
	Out            io.Writer
	Logger         *slog.Logger
	IsWSL          func() bool
	NetworkingMode func(context.Context) string
}

// newTailscaleController takes the effective settings from Store; a nil Store
// keeps o.Cfg and rejects web updates.
func newTailscaleController(o tailscaleControllerOptions) *tailscaleController {
	cfg, web := o.Cfg, false
	if o.Store != nil {
		cfg, web = o.Store.Effective()
	}
	state := contract.TailscaleOff
	if cfg.Enabled() {
		state = contract.TailscaleStarting
	}
	return &tailscaleController{
		cfg: cfg, web: web, store: o.Store, svc: o.Service, public: o.Public, dir: o.StateDir,
		path: filepath.Join(o.StateDir, tailscaleStateFile), lockPath: filepath.Join(o.StateDir, tailscaleLockFile),
		token: o.Token, tokenGenerated: o.TokenGenerated, out: o.Out, logger: o.Logger,
		isWSL: o.IsWSL, networkingMode: o.NetworkingMode,
		state: state,
	}
}

// enableErrorLocked reports why this process may not publish an entry.
func (tc *tailscaleController) enableErrorLocked() error {
	if len(tc.token) < minTunnelTokenLength {
		return fmt.Errorf("%w: Tailscale Serve/Funnel requires a WebMux token of at least %d characters; run `webmux config init` or set a strong token, then restart WebMux", tailscalesettings.ErrEnableBlocked, minTunnelTokenLength)
	}
	if tc.listenBlocked != "" {
		return fmt.Errorf("%w: %s", tailscalesettings.ErrEnableBlocked, tc.listenBlocked)
	}
	return nil
}

// UpdateSettings validates, persists and applies a web update. The response
// reports the new intent; removal and apply run on the controller worker.
func (tc *tailscaleController) UpdateSettings(in contract.TailscaleSettingsUpdate) (contract.TailscaleStatus, error) {
	tc.opMu.Lock()
	defer tc.opMu.Unlock()
	if err := tc.updateSettings(in); err != nil {
		return tc.Status(), err
	}
	return tc.Status(), nil
}

func (tc *tailscaleController) updateSettings(in contract.TailscaleSettingsUpdate) error {
	if err := tc.store.Unavailable(); err != nil {
		return err
	}
	tc.mu.Lock()
	current, closed := tc.cfg, tc.closed
	enableErr := tc.enableErrorLocked()
	tc.mu.Unlock()
	if closed {
		return errors.New("WebMux is shutting down")
	}
	next, err := tailscalesettings.Prepare(current, in.Mode, in.Port, in.Provider)
	if err != nil {
		return err
	}
	if next.Provider == "windows" && !tc.isWSL() {
		return fmt.Errorf(`%w: provider "windows" is only valid inside WSL`, tailscalesettings.ErrInvalid)
	}
	if next.Mode == config.TailscaleFunnel && current.Mode != config.TailscaleFunnel && !in.ConfirmFunnel {
		return tailscalesettings.ErrFunnelUnconfirmed
	}
	if next.Enabled() && enableErr != nil {
		return enableErr
	}
	if err := tc.store.Save(next); err != nil {
		return err
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	unchanged := next == current && tc.state != contract.TailscaleFailed
	tc.cfg, tc.web = next, true
	if !unchanged {
		tc.launchLocked()
	}
	return nil
}

// ResetSettings removes the web override. Like the Cloudflare tunnel, a
// reset never exposes this process: the entry closes now and the TOML values
// apply on the next start.
func (tc *tailscaleController) ResetSettings() (contract.TailscaleStatus, error) {
	tc.opMu.Lock()
	defer tc.opMu.Unlock()
	tc.mu.Lock()
	closed := tc.closed
	tc.mu.Unlock()
	if closed {
		return tc.Status(), errors.New("WebMux is shutting down")
	}
	next, err := tc.store.Reset()
	if err != nil {
		return tc.Status(), err
	}
	next.Mode = config.TailscaleOff
	tc.mu.Lock()
	tc.cfg, tc.web = next, false
	tc.launchLocked()
	tc.mu.Unlock()
	return tc.Status(), nil
}

// Prepare reconciles the previous entry point before the listener binds. It
// refuses startup only when an owned entry cannot be closed or an unowned
// entry already points at this target.
func (tc *tailscaleController) Prepare(ctx context.Context, listen string) error {
	if err := statefile.CheckLegacy(tc.dir, statefile.TailscaleOwnership); err != nil {
		return err
	}
	if tc.cfg.Provider == "windows" && !tc.isWSL() {
		return errors.New(`tailscale.provider "windows" is only valid inside WSL`)
	}
	hasState := false
	if _, err := os.Lstat(tc.path); !errors.Is(err, os.ErrNotExist) {
		hasState = true
	}
	if !tc.cfg.Enabled() && !hasState {
		return nil
	}
	if err := tc.openLock(); err != nil {
		tc.storageErr = err
		if target, ok := targetFromListen(listen); ok && tc.entryPointsTo(ctx, target) {
			return fmt.Errorf("%w；已有入口指向当前监听地址，拒绝启动", err)
		}
		return nil
	}
	record, err := tc.loadState()
	if err != nil {
		tc.storageErr = err
		if target, ok := targetFromListen(listen); ok && tc.entryPointsTo(ctx, target) {
			return fmt.Errorf("%w；无法确认现有入口所有权，拒绝启动", err)
		}
		return nil
	}
	tc.record = record
	target, ok := targetFromListen(listen)
	if ok {
		tc.localPort = portFromTarget(target)
	}
	if !tc.cfg.Enabled() {
		if record == nil {
			return nil
		}
		return tc.removeRecord(ctx, record)
	}
	removedOwned := false
	if record != nil && recordStale(record, tc.cfg, target, ok) {
		if err := tc.removeRecord(ctx, record); err != nil {
			return err
		}
		tc.record = nil
		record = nil
		removedOwned = true
	}
	if record == nil && ok && !removedOwned {
		if tc.cfg.Port != 0 {
			if err := tc.refuseUnownedTarget(ctx, target, tc.cfg.Port); err != nil {
				return err
			}
		} else if tc.entryPointsTo(ctx, target) {
			return errors.New("tailscale-ownership.json 缺失，但已有入口指向当前监听地址；无法确认所有权，拒绝启动")
		}
	}
	return nil
}

func (tc *tailscaleController) fail(err error, revoke bool) {
	if revoke {
		tc.public.Clear("tailscale")
	}
	tc.mu.Lock()
	reason := tailscaleReason(err, tc.cfg.Provider)
	tc.state, tc.url, tc.reason = contract.TailscaleFailed, "", reason
	tc.mu.Unlock()
	tc.logger.Warn("tailscale entry failed; local HTTP continues", "error", reason)
}

func (tc *tailscaleController) clearRecord() {
	tc.mu.Lock()
	tc.record = nil
	tc.mu.Unlock()
}

func (tc *tailscaleController) failLocked(reason string, revoke bool) {
	if revoke {
		tc.public.Clear("tailscale")
	}
	tc.state, tc.url, tc.reason = contract.TailscaleFailed, "", reason
}

func (tc *tailscaleController) blockLocked(reason string) {
	tc.public.Clear("tailscale")
	tc.state, tc.url, tc.blocked, tc.reason = contract.TailscaleFailed, "", reason, ""
}

func (tc *tailscaleController) Close() {
	tc.opMu.Lock()
	defer tc.opMu.Unlock()
	tc.mu.Lock()
	if tc.closed {
		tc.mu.Unlock()
		return
	}
	tc.closed = true
	if tc.cancel != nil {
		tc.cancel()
	}
	done := tc.done
	tc.public.Clear("tailscale")
	tc.mu.Unlock()
	if done != nil {
		<-done
	}
	// Read the record only after apply has stopped: a conflict found during
	// apply clears it, and a stale snapshot would authorize removal.
	tc.mu.Lock()
	record := tc.record
	tc.mu.Unlock()
	if record != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := tc.removeVerified(ctx, record); err != nil {
			tc.logger.Warn("tailscale entry not removed; remove it manually and it will be reconciled on the next start",
				"error", err, "port", record.Port,
				"serveHint", fmt.Sprintf("tailscale serve --https=%d off", record.Port))
		} else {
			if err := tc.deleteRecord(); err != nil {
				tc.logger.Warn("ownership deletion not durable", "error", err)
			}
		}
	}
	tc.ownership.Close()
}

// Refresh bypasses the detection cache for an explicit settings-page refresh.
func (tc *tailscaleController) Refresh() {
	tc.svc.Refresh()
}

// Status is the read-only API view. Detection is cached by the service.
func (tc *tailscaleController) Status() contract.TailscaleStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status := tc.svc.Detect(ctx)
	tc.mu.Lock()
	defer tc.mu.Unlock()
	out := wire.TailscaleStatus(status)
	out.ConfiguredProvider = tc.cfg.Provider
	out.Mode = tc.cfg.Mode
	out.Port = tc.cfg.Port
	out.State = tc.state
	out.PublicURL = tc.url
	out.BlockedReason = tc.blocked
	out.Error = tc.reason
	if out.Error == "" {
		out.Error = status.Error
	}
	if out.Provider == "" {
		out.Provider = tailscale.ProviderNone
	}
	if out.Mode == "" {
		out.Mode = config.TailscaleOff
	}
	if out.State == "" {
		out.State = contract.TailscaleOff
	}
	out.Source = "config"
	if tc.web {
		out.Source = "web"
	}
	out.CanEnable, out.StorageAvailable = true, true
	if err := tc.enableErrorLocked(); err != nil {
		out.CanEnable, out.EnableBlockedReason = false, err.Error()
	}
	if err := tc.store.Unavailable(); err != nil {
		out.StorageAvailable, out.StorageError = false, err.Error()
		out.CanEnable, out.EnableBlockedReason = false, err.Error()
	}
	if tc.storageErr != nil && tc.cfg.Enabled() {
		out.Error = tc.storageErr.Error()
		out.State = contract.TailscaleFailed
	}
	return out
}
