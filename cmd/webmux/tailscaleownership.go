package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/statefile"
	"github.com/rainchan/webmux/internal/tailscale"
)

// recordStale reports whether cfg no longer keeps the recorded entry, so the
// exact recorded target must be closed before cfg applies.
func recordStale(record *tailscaleState, cfg config.TailscaleConfig, target string, targetOK bool) bool {
	if !cfg.Enabled() || record.Mode != cfg.Mode || !targetOK || record.Target != target {
		return true
	}
	if cfg.Port == 0 {
		// Automatic selection cannot keep a previously explicit custom
		// port; remove that exact entry before choosing a candidate.
		if !autoCandidate(record.Port) {
			return true
		}
	} else if record.Port != cfg.Port {
		return true
	}
	// An explicit provider change closes the old entry through the recorded
	// provider first. "auto" keeps a native record because detection may
	// legitimately change, but never keeps a Windows record: auto does not
	// manage the Windows node.
	if cfg.Provider == "auto" {
		return record.Provider == tailscale.ProviderWindows
	}
	return providerFamily(record.Provider) != cfg.Provider
}

func validateOwnership(r tailscaleState) error {
	if r.V != tailscaleStateVersion {
		return errors.New("invalid ownership record")
	}
	if r.Provider == "" || r.DNSName == "" || r.Target == "" {
		return errors.New("invalid ownership record")
	}
	if r.Port < 1 || r.Port > 65535 {
		return errors.New("invalid ownership record")
	}
	if r.Status != "pending" && r.Status != "active" {
		return errors.New("invalid ownership record")
	}
	if r.Mode != config.TailscaleServe && r.Mode != config.TailscaleFunnel {
		return errors.New("invalid ownership record")
	}
	return nil
}
func (tc *tailscaleController) openLock() error {
	if tc.ownership != nil {
		return tc.ownership.Err()
	}
	var err error
	tc.ownership, _, err = statefile.Open[tailscaleState](tc.dir, statefile.TailscaleOwnership, validateOwnership)
	if statefile.IsKind(err, statefile.Missing) {
		return nil
	}
	return err
}
func (tc *tailscaleController) loadState() (*tailscaleState, error) {
	r, err := statefile.ReadSnapshot[tailscaleState](tc.dir, statefile.TailscaleOwnership, validateOwnership)
	if statefile.IsKind(err, statefile.Missing) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
func (tc *tailscaleController) saveRecord(record tailscaleState) error {
	if err := tc.openLock(); err != nil {
		return err
	}
	return tc.ownership.Commit(record)
}
func (tc *tailscaleController) deleteRecord() error {
	if tc.ownership == nil {
		return errors.New("ownership store unavailable")
	}
	return tc.ownership.Remove()
}

func (tc *tailscaleController) removeRecord(ctx context.Context, record *tailscaleState) error {
	if err := tc.removeVerified(ctx, record); err != nil {
		// Never leave an unverified record in memory: Close must not retry a
		// deletion that this process could not prove it owns.
		tc.clearRecord()
		return err
	}
	if err := tc.deleteRecord(); err != nil {
		return err
	}
	tc.clearRecord()
	return nil
}

// removeVerified deletes the exact recorded entry only when the live Serve
// status proves the same provider, DNS/port and proxy target. An unreadable
// status fails closed and removes nothing. A port that is empty or now
// proxies elsewhere no longer holds this record's entry: the record is stale,
// the foreign entry stays untouched, and the caller may drop the record.
func (tc *tailscaleController) removeVerified(ctx context.Context, record *tailscaleState) error {
	tc.public.Clear("tailscale")
	node := tc.svc.Inspect(ctx, providerFamily(record.Provider))
	status := node.Status
	if node.ServeErr != nil {
		return fmt.Errorf("无法确认 Tailscale 入口所有权: %w；%s", node.ServeErr, tc.staleHint(record.Port))
	}
	if status.Provider == tailscale.ProviderNone || !providerMatches(record.Provider, status.Provider) {
		return fmt.Errorf("无法确认上次运行的 Tailscale 入口所有权（recorded provider %q, detected %q）；%s", record.Provider, status.Provider, tc.staleHint(record.Port))
	}
	info, exists := node.Serve.Lookup(record.DNSName, record.Port)
	if !exists && status.DNSName != "" && status.DNSName != record.DNSName {
		info, exists = node.Serve.Lookup(status.DNSName, record.Port)
	}
	if !exists {
		return nil
	}
	if info.Proxy != record.Target {
		tc.logger.Warn("tailscale record is stale; the entry on its port belongs to another target and is left untouched", "port", record.Port)
		return nil
	}
	if err := node.Remove(ctx, record.Port); err != nil {
		return fmt.Errorf("无法移除上次运行的 Tailscale 入口；请先手工执行 `tailscale serve --https=%d off`：%s", record.Port, tailscaleReason(err, providerFamily(record.Provider)))
	}
	return nil
}

// staleHint names the manual recovery when ownership cannot be read.
func (tc *tailscaleController) staleHint(port int) string {
	return fmt.Sprintf("确认 Tailscale 可用后重试；若端口 %d 的入口已手工移除或 Tailscale 已卸载，请删除 %s", port, tc.path)
}

func providerFamily(provider string) string {
	if provider == tailscale.ProviderWSLNative {
		return "linux"
	}
	if provider == "" {
		return "auto"
	}
	return provider
}

func providerMatches(recorded, detected string) bool {
	if recorded == detected {
		return true
	}
	recorded, detected = providerFamily(recorded), providerFamily(detected)
	return recorded == detected
}

func (tc *tailscaleController) refuseUnownedTarget(ctx context.Context, target string, port int) error {
	status := tc.svc.Detect(ctx)
	if status.DNSName == "" {
		return nil
	}
	serve, err := tc.svc.ServeStatus(ctx)
	if err != nil {
		return nil
	}
	if entry, ok := serve.Lookup(status.DNSName, port); ok && entry.Proxy == target {
		return fmt.Errorf("tailscale-ownership.json 缺失，但 %d 端口已有指向 %s 的入口；无法确认所有权，拒绝启动", port, target)
	}
	return nil
}

// entryPointsTo reports whether any Serve entry already proxies to target.
func (tc *tailscaleController) entryPointsTo(ctx context.Context, target string) bool {
	status := tc.svc.Detect(ctx)
	if status.DNSName == "" {
		return false
	}
	serve, err := tc.svc.ServeStatus(ctx)
	if err != nil {
		return false
	}
	for _, entry := range serve.Entries {
		if info, ok := serve.Lookup(status.DNSName, entry.Port); ok && info.Proxy == target {
			return true
		}
	}
	return false
}
