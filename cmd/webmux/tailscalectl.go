package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
	tailscaleStateFile    = "tailscale.json"
	tailscaleLockFile     = "tailscale.lock"
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
	lock          *os.File
	localPort     int
	closed        bool
	baseCtx       context.Context
	cancel        context.CancelFunc
	done          chan struct{}
}

// newTailscaleController takes the effective settings from store; a nil store
// keeps cfg and rejects web updates.
func newTailscaleController(cfg config.TailscaleConfig, store *tailscalesettings.Service, svc *tailscale.Service, stateDir string, public *api.PublicOrigins, token string, tokenGenerated bool, out io.Writer, logger *slog.Logger, isWSL func() bool, networkingMode func(context.Context) string) *tailscaleController {
	web := false
	if store != nil {
		cfg, web = store.Effective()
	}
	state := contract.TailscaleOff
	if cfg.Enabled() {
		state = contract.TailscaleStarting
	}
	return &tailscaleController{
		cfg: cfg, web: web, store: store, svc: svc, public: public, dir: stateDir,
		path: filepath.Join(stateDir, tailscaleStateFile), lockPath: filepath.Join(stateDir, tailscaleLockFile),
		token: token, tokenGenerated: tokenGenerated, out: out, logger: logger,
		isWSL: isWSL, networkingMode: networkingMode,
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
		return fmt.Errorf("%w: provider \"windows\" is only valid inside WSL", tailscalesettings.ErrInvalid)
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
	if tc.cfg.Provider == "windows" && !tc.isWSL() {
		return errors.New("tailscale.provider \"windows\" is only valid inside WSL")
	}
	hasState := false
	if _, err := os.Stat(tc.path); err == nil {
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
			return errors.New("tailscale.json 缺失，但已有入口指向当前监听地址；无法确认所有权，拒绝启动")
		}
	}
	return nil
}

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

func (tc *tailscaleController) openLock() error {
	if tc.lock != nil {
		return nil
	}
	if err := os.MkdirAll(tc.dir, 0700); err != nil {
		return fmt.Errorf("创建状态目录: %w", err)
	}
	lock, err := os.OpenFile(tc.lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("打开 tailscale 锁文件: %w", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return errors.New("检测到 Tailscale 状态目录已被另一实例使用")
	}
	tc.lock = lock
	return nil
}

func (tc *tailscaleController) loadState() (*tailscaleState, error) {
	data, err := os.ReadFile(tc.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 tailscale.json: %w", err)
	}
	var record tailscaleState
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, errors.New("tailscale.json 损坏，无法确认入口所有权")
	}
	if record.V != tailscaleStateVersion {
		return nil, errors.New("tailscale.json 版本不受支持")
	}
	return &record, nil
}

func (tc *tailscaleController) saveRecord(record tailscaleState) error {
	if err := os.MkdirAll(tc.dir, 0700); err != nil {
		return err
	}
	return statefile.WriteJSON(tc.path, record, statefile.Options{TempPattern: ".tailscale-*"})
}

func (tc *tailscaleController) deleteRecord() {
	_ = os.Remove(tc.path)
}

func (tc *tailscaleController) removeRecord(ctx context.Context, record *tailscaleState) error {
	if err := tc.removeVerified(ctx, record); err != nil {
		// Never leave an unverified record in memory: Close must not retry a
		// deletion that this process could not prove it owns.
		tc.clearRecord()
		return err
	}
	tc.deleteRecord()
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
		return fmt.Errorf("tailscale.json 缺失，但 %d 端口已有指向 %s 的入口；无法确认所有权，拒绝启动", port, target)
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
	tc.deleteRecord()
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
			tc.failLocked("tailscale.provider \"windows\" 只能在 WSL 中使用", false)
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
		tc.blockLocked("检测到 Windows Tailscale；如需托管，请显式设置 tailscale.provider = \"windows\"（auto 永不代管 Windows）")
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
		tc.fail(fmt.Errorf("无法写入 tailscale.json: %w", err), false)
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
	tc.clearRecord()
	tc.deleteRecord()
	tc.fail(cause, false)
}

// tailscaleReason maps only genuine CLI errors to user-facing guidance.
func tailscaleReason(err error, provider string) string {
	var cliErr *tailscale.CLIError
	if errors.As(err, &cliErr) {
		return classifyTailscaleError(cliErr, provider)
	}
	return err.Error()
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
			tc.deleteRecord()
		}
	}
	if tc.lock != nil {
		_ = tc.lock.Close()
		tc.lock = nil
	}
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

func choosePort(serve tailscale.ServeStatus) int {
	for _, port := range []int{443, 8443, 10000} {
		if !hasPort(serve, port) {
			return port
		}
	}
	return 0
}

func autoCandidate(port int) bool {
	return port == 443 || port == 8443 || port == 10000
}

func hasPort(serve tailscale.ServeStatus, port int) bool {
	for _, entry := range serve.Entries {
		if entry.Port == port {
			return true
		}
	}
	return false
}

func targetFromListen(listen string) (string, bool) {
	host, portText, err := net.SplitHostPort(listen)
	if err != nil {
		return "", false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", false
	}
	if host == "" {
		host = "0.0.0.0"
	}
	if strings.EqualFold(host, "localhost") {
		return fmt.Sprintf("http://127.0.0.1:%d", port), true
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsUnspecified() && !ip.Equal(net.IPv4(127, 0, 0, 1)) {
		return "", false
	}
	return fmt.Sprintf("http://127.0.0.1:%d", port), true
}

func portFromTarget(target string) int {
	_, port, err := net.SplitHostPort(strings.TrimPrefix(target, "http://"))
	if err != nil {
		return 0
	}
	value, _ := strconv.Atoi(port)
	return value
}

func localPort(local string) (int, bool) {
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(local, "http://"))
	if err != nil || host != "127.0.0.1" {
		return 0, false
	}
	port, err := strconv.Atoi(portText)
	return port, err == nil && port > 0
}

func classifyTailscaleError(err error, provider string) string {
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "access denied"), strings.Contains(lower, "permission"), strings.Contains(lower, "operator"):
		if provider == "windows" {
			return "Tailscale 权限不足；请在 Windows 管理员终端执行一次 `tailscale set --operator=<Windows 用户>`"
		}
		return "Tailscale 权限不足；请执行一次 `tailscale set --operator=$USER`"
	case strings.Contains(lower, "funnel") && (strings.Contains(lower, "not available") || strings.Contains(lower, "attribute") || strings.Contains(lower, "does not")):
		return "Tailscale 节点缺少 funnel 属性；请在 Tailscale 管理台为节点启用 Funnel"
	case strings.Contains(lower, "certificate"), strings.Contains(lower, "https"):
		return "Tailscale HTTPS 证书未就绪；请先在 Tailscale 管理台启用 HTTPS"
	case strings.Contains(lower, "in use"), strings.Contains(lower, "conflict"), strings.Contains(lower, "already"):
		return "Tailscale 端口冲突；请换一个空闲端口或先移除冲突入口"
	case strings.Contains(lower, "timed out"), strings.Contains(lower, "deadline"):
		return "Tailscale 命令超时；请检查 Tailscale 状态后重试"
	default:
		return "Tailscale 命令失败；请查看服务日志并确认 Tailscale 状态"
	}
}
