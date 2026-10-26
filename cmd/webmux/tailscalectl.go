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
	cfg            config.TailscaleConfig
	svc            *tailscale.Service
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

	mu         sync.Mutex
	state      string
	url        string
	reason     string
	blocked    string
	record     *tailscaleState
	storageErr error
	lock       *os.File
	localPort  int
	closed     bool
	cancel     context.CancelFunc
	done       chan struct{}
}

func newTailscaleController(cfg config.TailscaleConfig, svc *tailscale.Service, stateDir string, public *api.PublicOrigins, token string, tokenGenerated bool, out io.Writer, logger *slog.Logger, isWSL func() bool, networkingMode func(context.Context) string) *tailscaleController {
	state := contract.TailscaleOff
	if cfg.Enabled() {
		state = contract.TailscaleStarting
	}
	return &tailscaleController{
		cfg: cfg, svc: svc, public: public, dir: stateDir,
		path: filepath.Join(stateDir, tailscaleStateFile), lockPath: filepath.Join(stateDir, tailscaleLockFile),
		token: token, tokenGenerated: tokenGenerated, out: out, logger: logger,
		isWSL: isWSL, networkingMode: networkingMode,
		state: state,
	}
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
	if record != nil {
		changed := record.Mode != tc.cfg.Mode
		if tc.cfg.Port == 0 {
			// Automatic selection cannot keep a previously explicit custom
			// port; remove that exact entry before choosing a candidate.
			changed = changed || !autoCandidate(record.Port)
		} else {
			changed = changed || record.Port != tc.cfg.Port
		}
		if !ok || record.Target != target {
			changed = true
		}
		// An explicit provider change must close the old entry through the
		// recorded provider before applying on the new one. "auto" never
		// rewrites ownership because detection may legitimately change.
		if tc.cfg.Provider != "auto" && providerFamily(record.Provider) != tc.cfg.Provider {
			changed = true
		}
		if changed {
			if err := tc.removeRecord(ctx, record); err != nil {
				return err
			}
			tc.record = nil
			record = nil
			removedOwned = true
		}
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
		return errors.New("Tailscale 状态目录已被另一实例使用")
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
// status proves the same provider, DNS/port and proxy target. A read failure or
// a target mismatch fails closed and removes nothing.
func (tc *tailscaleController) removeVerified(ctx context.Context, record *tailscaleState) error {
	tc.public.Clear("tailscale")
	tc.svc.SetProvider(providerFamily(record.Provider))
	defer tc.svc.SetProvider(tc.cfg.Provider)
	status := tc.svc.Detect(ctx)
	serve, err := tc.svc.ServeStatus(ctx)
	if err != nil {
		return fmt.Errorf("无法确认 Tailscale 入口所有权: %w", err)
	}
	if status.Provider == tailscale.ProviderNone || !providerMatches(record.Provider, status.Provider) {
		return fmt.Errorf("无法确认上次运行的 Tailscale 入口所有权（recorded provider %q, detected %q）", record.Provider, status.Provider)
	}
	info, exists := serve.Lookup(record.DNSName, record.Port)
	if !exists && status.DNSName != "" && status.DNSName != record.DNSName {
		info, exists = serve.Lookup(status.DNSName, record.Port)
	}
	if !exists {
		return nil
	}
	if info.Proxy != record.Target {
		return fmt.Errorf("端口 %d 的现有入口不属于上次记录的 target；拒绝删除，请手工确认", record.Port)
	}
	if err := tc.svc.Remove(ctx, record.Port); err != nil {
		return fmt.Errorf("无法移除上次运行的 Tailscale 入口；请先手工执行 `tailscale serve --https=%d off`：%s", record.Port, tailscaleReason(err, providerFamily(record.Provider)))
	}
	return nil
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

// Start applies the configured entry after the listener is bound.
func (tc *tailscaleController) Start(ctx context.Context, local string) {
	tc.mu.Lock()
	if tc.closed || !tc.cfg.Enabled() {
		tc.mu.Unlock()
		return
	}
	if tc.storageErr != nil {
		tc.state, tc.reason = contract.TailscaleFailed, tc.storageErr.Error()
		tc.mu.Unlock()
		return
	}
	port, ok := localPort(local)
	if !ok {
		tc.blocked = "托管 Tailscale 需要监听 127.0.0.1 或 0.0.0.0；请把 listen 改为 127.0.0.1:<port> 或 0.0.0.0:<port>"
		tc.state = contract.TailscaleFailed
		tc.reason = tc.blocked
		tc.mu.Unlock()
		return
	}
	tc.localPort = port

	tc.mu.Unlock()
	probe, cancelProbe := context.WithTimeout(ctx, time.Second)
	conn, err := (&net.Dialer{}).DialContext(probe, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	cancelProbe()
	if err != nil {
		tc.mu.Lock()
		tc.blocked = "127.0.0.1 无法访问当前监听端口；托管 Tailscale 需要 IPv4 回环可达"
		tc.state = contract.TailscaleFailed
		tc.reason = tc.blocked
		tc.mu.Unlock()
		return
	}
	_ = conn.Close()
	tc.mu.Lock()
	runCtx, cancel := context.WithCancel(ctx)
	tc.cancel = cancel
	done := make(chan struct{})
	tc.done = done
	tc.mu.Unlock()

	go func() {
		defer close(done)
		tc.apply(runCtx)
	}()
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
					tc.ready(status, rec.Port, target, false)
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
			tc.ready(status, port, target, false)
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
		tc.cleanupFailed(ctx, record, errors.New("Tailscale 命令已结束，但回读的入口与预期不符"))
		return
	}
	record.Status = "active"
	if err := tc.saveRecord(record); err != nil {
		tc.logger.Warn("tailscale state not marked active", "error", err)
	}
	tc.ready(status, port, target, true)
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

func (tc *tailscaleController) ready(status tailscale.Status, port int, target string, announce bool) {
	url := tailscale.PublicURL(status.DNSName, port)
	if err := tc.public.Set("tailscale", url); err != nil {
		tc.fail(fmt.Errorf("无法登记 Tailscale 来源: %w", err), true)
		return
	}
	tc.mu.Lock()
	tc.state, tc.url, tc.reason = contract.TailscaleReady, url, ""
	if record := tc.record; record != nil {
		record.Status = "active"
	}
	tc.mu.Unlock()
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
	reason := tailscaleReason(err, tc.cfg.Provider)
	tc.mu.Lock()
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
	record := tc.record
	tc.public.Clear("tailscale")
	tc.mu.Unlock()
	if done != nil {
		<-done
	}
	if record != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	if !tc.cfg.Enabled() {
		out.State = contract.TailscaleOff
		out.PublicURL = ""
	}
	if tc.storageErr != nil {
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
