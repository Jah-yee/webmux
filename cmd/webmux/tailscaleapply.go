package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tailscale"
)

// Start records the bound listener and applies the configured entry. Web
// updates later reuse the same listener facts.
func (tc *tailscaleController) Start(ctx context.Context, local string) {
	tc.mu.Lock()
	if tc.closed {
		tc.mu.Unlock()
		return
	}
	tc.baseCtx = ctx
	port, ok := localPort(local)
	if ok {
		tc.localPort = port
	} else {
		tc.listenBlocked = "托管 Tailscale 需要监听 127.0.0.1 或 0.0.0.0；请把 listen 改为 127.0.0.1:<port> 或 0.0.0.0:<port>"
	}
	tc.mu.Unlock()
	if ok {
		probe, cancelProbe := context.WithTimeout(ctx, time.Second)
		conn, err := (&net.Dialer{}).DialContext(probe, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
		cancelProbe()
		if err != nil {
			tc.mu.Lock()
			tc.listenBlocked = "127.0.0.1 无法访问当前监听端口；托管 Tailscale 需要 IPv4 回环可达"
			tc.mu.Unlock()
		} else {
			_ = conn.Close()
		}
	}
	tc.mu.Lock()
	defer tc.mu.Unlock()
	if tc.closed || !tc.cfg.Enabled() {
		return
	}
	tc.launchLocked()
}

// launchLocked supersedes the running worker with one for tc.cfg. The new
// worker joins its predecessor, revokes any origin it may have published,
// closes the recorded entry when tc.cfg no longer keeps it, and only then
// applies tc.cfg. Callers hold tc.mu; the request never waits for the CLI.
func (tc *tailscaleController) launchLocked() {
	if tc.cancel != nil {
		tc.cancel()
	}
	previous := tc.done
	base := tc.baseCtx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	done := make(chan struct{})
	tc.cancel, tc.done = cancel, done
	cfg := tc.cfg
	tc.resetStateLocked(cfg)
	go func() {
		defer close(done)
		if previous != nil {
			<-previous
		}
		if ctx.Err() != nil {
			return
		}
		tc.mu.Lock()
		tc.resetStateLocked(cfg)
		tc.mu.Unlock()
		if !tc.reconcile(cfg) {
			return
		}
		tc.svc.Configure(cfg.Provider)
		if !cfg.Enabled() {
			tc.mu.Lock()
			if ctx.Err() == nil {
				tc.state = contract.TailscaleOff
			}
			tc.mu.Unlock()
			return
		}
		if err := tc.openLock(); err != nil {
			tc.mu.Lock()
			tc.storageErr = err
			tc.mu.Unlock()
		}
		tc.mu.Lock()
		storageErr, blocked := tc.storageErr, tc.listenBlocked
		if storageErr != nil {
			tc.failLocked(storageErr.Error(), false)
		} else if blocked != "" {
			tc.blockLocked(blocked)
		}
		tc.mu.Unlock()
		if storageErr == nil && blocked == "" {
			tc.apply(ctx)
		}
	}()
}

// resetStateLocked revokes the origin and reports the intent of cfg. Closing
// a recorded entry is still work in progress, so it reports starting until
// the worker has removed it.
func (tc *tailscaleController) resetStateLocked(cfg config.TailscaleConfig) {
	tc.public.Clear("tailscale")
	tc.state, tc.url, tc.reason, tc.blocked = contract.TailscaleOff, "", "", ""
	if cfg.Enabled() || tc.record != nil {
		tc.state = contract.TailscaleStarting
	}
}

// reconcile closes the recorded entry when cfg no longer keeps it. A failed
// removal keeps the record for a later attempt and leaves cfg unapplied.
func (tc *tailscaleController) reconcile(cfg config.TailscaleConfig) bool {
	tc.mu.Lock()
	record, port := tc.record, tc.localPort
	tc.mu.Unlock()
	if record == nil {
		return true
	}
	target := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !recordStale(record, cfg, target, port != 0) {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := tc.removeVerified(ctx, record); err != nil {
		tc.fail(fmt.Errorf("未能关闭上一个 Tailscale 入口，新设置尚未应用：%s", err.Error()), true)
		return false
	}
	if err := tc.deleteRecord(); err != nil {
		tc.fail(err, true)
		return false
	}
	tc.clearRecord()
	// Entries in the cached detection predate the removal.
	tc.svc.Refresh()
	return true
}

func (tc *tailscaleController) apply(ctx context.Context) {
	status := tc.svc.Detect(ctx)
	tc.mu.Lock()
	tc.blocked = ""
	provider := status.Provider
	if tc.cfg.Provider == "windows" {
		if !tc.isWSL() {
			tc.failLocked(`tailscale.provider "windows" 只能在 WSL 中使用`, false)
			tc.mu.Unlock()
			return
		}
		if tc.networkingMode(ctx) != "mirrored" {
			tc.blockLocked("WSL NAT 模式无法托管 Windows Tailscale：可改用 mirrored 网络、在 WSL 内安装 Tailscale，或继续用 Windows 托盘转发并手工配置 Serve")
			tc.mu.Unlock()
			return
		}
	}
	if tc.cfg.Provider == "auto" && tc.isWSL() && provider == tailscale.ProviderWindows {
		tc.blockLocked(`检测到 Windows Tailscale；如需托管，请显式设置 tailscale.provider = "windows"（auto 永不代管 Windows）`)
		tc.mu.Unlock()
		return
	}
	switch {
	case provider == tailscale.ProviderNone:
		tc.failLocked("未找到可用的 Tailscale CLI", false)
		tc.mu.Unlock()
		return
	case status.BackendState != tailscale.BackendRunning:
		tc.failLocked("Tailscale 后端状态为 "+status.BackendState+"，请先登录或启动 Tailscale", false)
		tc.mu.Unlock()
		return
	case status.DNSName == "":
		tc.failLocked("Tailscale 未报告节点 DNS 名", false)
		tc.mu.Unlock()
		return
	case !status.HTTPSReady:
		tc.failLocked("Tailscale HTTPS 证书尚未就绪，请先在 Tailscale 管理台启用 HTTPS", false)
		tc.mu.Unlock()
		return
	}
	localPort := tc.localPort
	cfg := tc.cfg
	tc.mu.Unlock()

	serve, err := tc.svc.ServeStatus(ctx)
	if err != nil {
		tc.fail(err, false)
		return
	}
	target := fmt.Sprintf("http://127.0.0.1:%d", localPort)
	port := cfg.Port
	if port == 0 {
		tc.mu.Lock()
		rec := tc.record
		tc.mu.Unlock()
		if rec != nil && rec.Mode == cfg.Mode && rec.Target == target && autoCandidate(rec.Port) {
			if info, exists := serve.Lookup(status.DNSName, rec.Port); exists {
				if tc.canReuse(info, target, rec.Port, status.DNSName, cfg.Mode) {
					tc.ready(ctx, status, rec.Port, target, false)
					return
				}
				tc.clearRecord()
				tc.fail(fmt.Errorf("端口 %d 的现有入口与记录不匹配；WebMux 不会覆盖，请先手工移除", rec.Port), false)
				return
			}
			port = rec.Port
		}
		if port == 0 {
			port = choosePort(serve)
			if port == 0 {
				tc.fail(errors.New("443、8443 与 10000 都已有入口；请为 Serve 显式指定其他空闲端口"), false)
				return
			}
		}
	}
	if info, exists := serve.Lookup(status.DNSName, port); exists {
		if tc.canReuse(info, target, port, status.DNSName, cfg.Mode) {
			tc.ready(ctx, status, port, target, false)
			return
		}
		tc.clearRecord()
		tc.fail(fmt.Errorf("端口 %d 已被其他 Tailscale 入口占用，WebMux 不会覆盖", port), false)
		return
	}
	record := tailscaleState{
		V: tailscaleStateVersion, Provider: status.Provider, DNSName: status.DNSName,
		Mode: cfg.Mode, Port: port, Target: target, Status: "pending",
		Public: cfg.Mode == config.TailscaleFunnel,
	}
	tc.mu.Lock()
	tc.record = &record
	tc.mu.Unlock()
	if err := tc.saveRecord(record); err != nil {
		tc.clearRecord()
		tc.fail(fmt.Errorf("无法写入 tailscale-ownership.json: %w", err), false)
		return
	}
	if err := tc.svc.Apply(ctx, cfg.Mode, port, localPort); err != nil {
		tc.cleanupFailed(ctx, record, err)
		return
	}
	after, err := tc.svc.ServeStatus(ctx)
	if err != nil {
		tc.cleanupFailed(ctx, record, fmt.Errorf("无法回读 Tailscale 状态: %w", err))
		return
	}
	info, ok := after.Lookup(status.DNSName, port)
	wantPublic := cfg.Mode == config.TailscaleFunnel
	if !ok || info.Handlers != 1 || info.Proxy != target || info.Public != wantPublic {
		tc.cleanupFailed(ctx, record, errors.New("执行 Tailscale 命令已结束，但回读的入口与预期不符"))
		return
	}
	tc.ready(ctx, status, port, target, true)
}

func (tc *tailscaleController) canReuse(info tailscale.HandlerInfo, target string, port int, dns, mode string) bool {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	record := tc.record
	if record == nil || record.Port != port || record.Target != target || record.Mode != mode {
		return false
	}
	if info.Handlers != 1 || info.Proxy != target {
		return false
	}
	return info.Public == (mode == config.TailscaleFunnel)
}

// ready publishes the origin only while ctx is current: launchLocked cancels
// a superseded worker under tc.mu, so a stale apply cannot authorize its URL.
func (tc *tailscaleController) ready(ctx context.Context, status tailscale.Status, port int, target string, announce bool) {
	// Entries in the cached detection predate the apply; refresh before the
	// ready state ends the page's polling.
	tc.svc.Refresh()
	url := tailscale.PublicURL(status.DNSName, port)
	tc.mu.Lock()
	if ctx.Err() != nil {
		tc.mu.Unlock()
		return
	}
	if err := tc.public.Set("tailscale", url); err != nil {
		tc.mu.Unlock()
		tc.fail(fmt.Errorf("无法登记 Tailscale 来源: %w", err), true)
		return
	}
	tc.state, tc.url, tc.reason = contract.TailscaleReady, url, ""
	var persist *tailscaleState
	if record := tc.record; record != nil && record.Status != "active" {
		record.Status = "active"
		saved := *record
		persist = &saved
	}
	tc.mu.Unlock()
	if persist != nil {
		if err := tc.saveRecord(*persist); err != nil {
			tc.logger.Warn("tailscale state not marked active", "error", err)
		}
	}
	if announce {
		link := url
		if tc.tokenGenerated {
			link = contract.AccessURL(url, tc.token)
		}
		_, _ = fmt.Fprintln(tc.out, "WebMux tailscale: "+link)
	}
}

func (tc *tailscaleController) cleanupFailed(ctx context.Context, record tailscaleState, cause error) {
	if err := tc.removeVerified(ctx, &record); err != nil {
		tc.fail(fmt.Errorf("%s；本次入口可能仍存在，请手工执行 `tailscale serve --https=%d off`：%s", tailscaleReason(cause, providerFamily(record.Provider)), record.Port, err), false)
		return
	}
	if err := tc.deleteRecord(); err != nil {
		tc.fail(err, false)
		return
	}
	tc.clearRecord()
	tc.fail(cause, false)
}
