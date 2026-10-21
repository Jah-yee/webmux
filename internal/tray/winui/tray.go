//go:build windows

package winui

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
	"golang.org/x/sys/windows"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tray"
	"github.com/rainchan/webmux/internal/wslclient"
)

var (
	//go:embed icons/stopped.ico
	iconStopped []byte
	//go:embed icons/starting.ico
	iconStarting []byte
	//go:embed icons/running.ico
	iconRunning []byte
	//go:embed icons/error.ico
	iconError []byte
)

const (
	settingsPoll   = 2 * time.Second
	uiInterval     = 10 * time.Second
	probeInterval  = 30 * time.Second
	invalidSetting = "托盘设置无效，已使用默认值"
)

// Options wire the tray; cmd/webmux-tray builds them.
type Options struct {
	Version string
	// SettingsPath is %APPDATA%\WebMux\tray.json.
	SettingsPath string
	Log          *tray.Log
	// Runner runs wsl.exe for the tray's own probes.
	Runner tray.Runner
	// NewService builds the WSL service for one set of settings.
	NewService func(tray.Settings) *tray.WSLService
	Reach      tray.Reacher
	Instance   *Instance
	// Executable is the absolute path of webmux-tray.exe.
	Executable string
}

// App is the running tray.
type App struct {
	o        Options
	log      *tray.Log
	svc      *tray.TargetService
	sup      *tray.Supervisor
	menu     *menu
	notifier *Notifier
	clip     *Clipboard
	relays   *relays

	hwnd     windows.HWND
	prevProc atomic.Uintptr

	applyMu sync.Mutex // serializes settings changes and their dialogs

	mu           sync.Mutex
	settings     tray.Settings
	settingsWarn string
	stamp        fileStamp
	noWSL        string
	distros      []wslclient.Distro
	netMode      string
	detection    tray.Detection
	detected     bool
	lastProbe    time.Time
	probing      bool
	bound        bindings
	journal      string
	installing   atomic.Bool

	lastIcon []byte
	lastTip  string

	kick     chan struct{}
	done     chan struct{}
	quitOnce sync.Once
}

// Run shows the tray until the user quits or Windows ends the session. It
// must run on the main goroutine.
func Run(o Options) {
	a := &App{o: o, log: o.Log, relays: newRelays(o.Log.Printf), kick: make(chan struct{}, 1), done: make(chan struct{})}
	a.notifier = &Notifier{Window: func() windows.HWND { return a.hwnd }, Logf: a.log.Printf}
	a.clip = &Clipboard{Owner: func() windows.HWND { return a.hwnd }}
	a.stamp = statFile(o.SettingsPath)
	settings, err := tray.LoadSettings(o.SettingsPath)
	if err != nil {
		a.log.Printf("tray settings: %v", err)
		a.settingsWarn = invalidSetting
	}
	a.settings = settings
	a.svc = tray.NewTargetService(o.NewService, settings)
	a.log.Printf("webmux-tray %s starting", o.Version)
	systray.Run(a.onReady, a.onExit)
}

func (a *App) onReady() {
	systray.SetIcon(iconStopped)
	systray.SetTooltip("WebMux · 正在检查 WSL")
	a.hwnd = findTrayWindow()
	if a.hwnd == 0 {
		a.log.Printf("tray window not found; notifications and menu refresh are unavailable")
	}
	a.menu = buildMenu()

	distros, err := CheckWSL(context.Background(), a.o.Runner)
	if err != nil {
		a.log.Printf("WSL check failed: %v", err)
		a.noWSL = err.Error()
	} else {
		a.distros = distros
		a.mu.Lock()
		settings := a.settings
		a.mu.Unlock()
		a.sup = tray.NewSupervisor(tray.Options{
			Service:  a.svc,
			Reach:    a.o.Reach,
			Settings: settings,
			Publish:  func(tray.Snapshot) { a.kickUI() },
			Version:  a.o.Version,
			Notify:   a.notifier.Show,
			Logf:     a.log.Printf,
		})
		a.probe(true)
	}
	a.listen()
	systray.SetOnTapped(func() { go a.openLocal() })
	if a.hwnd != 0 {
		subclass(a.hwnd, windows.NewCallback(a.wndProc), &a.prevProc)
	}
	if a.o.Instance != nil {
		a.o.Instance.Wakes(a.onWake)
	}
	go a.uiLoop()
	go a.watchSettings()
	a.kickUI()
}

// onExit runs when the tray quits or Windows ends the session; it never
// asks anything.
func (a *App) onExit() {
	a.shutdown(false)
}

// wndProc refreshes the status whenever the menu is about to open; the
// menu itself always shows the latest snapshot without waiting.
func (a *App) wndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	if msg == systrayMessage && lParam == wmRButtonUp {
		go a.onMenuOpen()
	}
	return callWindowProc(a.prevProc.Load(), hwnd, msg, wParam, lParam)
}

func (a *App) onMenuOpen() {
	if a.sup != nil {
		a.sup.Refresh()
		// A blocked tray never polls; opening the menu is the user's retry.
		if a.sup.Snapshot().Blocker != "" {
			a.sup.Recheck()
		}
	}
	a.probe(false)
}

func (a *App) kickUI() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func (a *App) snapshot() tray.Snapshot {
	if a.sup != nil {
		return a.sup.Snapshot()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return tray.Snapshot{State: tray.StateFailed, Reason: a.noWSL, Mode: a.settings.Mode, Settings: a.settings}
}

func (a *App) uiLoop() {
	ticker := time.NewTicker(uiInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.kick:
		case <-ticker.C:
		case <-a.done:
			return
		}
		a.update()
	}
}

// idle reports that no instance runs and no start or stop is in flight, so
// the service may switch to new paths or another distribution.
func idle(snap tray.Snapshot) bool {
	running := snap.HasStatus && snap.Status.State == contract.StateRunning
	busy := snap.State == tray.StateStarting || snap.State == tray.StateStopping || snap.State == tray.StateRestarting
	return !running && !busy
}

// update reconciles forwarding with the latest state and redraws.
func (a *App) update() {
	snap := a.snapshot()
	if a.sup != nil && idle(snap) && a.svc.Settle() {
		go a.sup.Refresh()
	}
	a.mu.Lock()
	v := view{
		settings:     cloneSettings(a.settings),
		settingsWarn: a.settingsWarn,
		noWSL:        a.noWSL != "",
		distros:      a.distros,
		detection:    a.detection,
		detected:     a.detected,
	}
	netMode := a.netMode
	a.mu.Unlock()
	if !v.noWSL && netMode != "" {
		v.plan = tray.PlanForward(netMode, snap, windowsInterfaces())
	}
	a.reconcileForward(snap, v)
	v.forwarding = a.relays.listening()
	v.relayState = a.relays.state
	v.autostart = AutostartEnabled(a.o.Executable)
	v.installSupported = a.svc.Current().Digests != nil
	v.installing = a.installing.Load()

	select {
	case <-a.done:
		return
	default:
	}
	b := a.menu.render(snap, v)
	a.mu.Lock()
	a.bound = b
	a.mu.Unlock()
	if icon := iconFor(snap.State); !bytes.Equal(icon, a.lastIcon) {
		systray.SetIcon(icon)
		a.lastIcon = icon
	}
	if tip := tooltip(snap); tip != a.lastTip {
		systray.SetTooltip(truncateUTF16(tip, 127))
		a.lastTip = tip
	}
}

func truncateUTF16(s string, limit int) string {
	u := windows.StringToUTF16(s)
	if len(u)-1 <= limit {
		return s
	}
	return windows.UTF16ToString(u[:limit])
}

// reconcileForward listens on the enabled addresses while the plan allows
// forwarding, following address, port and condition changes.
func (a *App) reconcileForward(snap tray.Snapshot, v view) {
	want := map[string]string{}
	target := ""
	if host, port, ok := tray.ForwardTarget(snap.Status); ok && v.plan.Needed && v.plan.Blocked == "" && snap.HasStatus {
		target = net.JoinHostPort(host, port)
		for _, c := range v.plan.Candidates {
			if slices.Contains(v.settings.Forward, c.Address) {
				want[c.Address] = net.JoinHostPort(c.Address, port)
			}
		}
	}
	a.relays.reconcile(want, target)
}

// probe refreshes the distribution list, the networking mode and the
// systemd detection in the background, at most every probeInterval unless
// forced.
func (a *App) probe(force bool) {
	a.mu.Lock()
	if a.noWSL != "" || a.probing || (!force && time.Since(a.lastProbe) < probeInterval) {
		a.mu.Unlock()
		return
	}
	a.probing = true
	a.lastProbe = time.Now()
	settings := a.settings
	a.mu.Unlock()
	go func() {
		ctx := context.Background()
		target := tray.TargetOf(settings)
		distros, err := Distros(ctx, a.o.Runner)
		if err != nil {
			a.log.Printf("wsl.exe --list: %v", err)
		}
		mode := NetworkingMode(ctx, a.o.Runner, target)
		detectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		detection := tray.DetectSystemd(detectCtx, a.o.Runner, target)
		cancel()
		a.mu.Lock()
		a.probing = false
		if err == nil {
			a.distros = distros
		}
		if a.settings.Distro == settings.Distro {
			a.netMode = mode
			a.detection, a.detected = detection, true
		}
		a.mu.Unlock()
		a.kickUI()
	}()
}

func (a *App) onWake() {
	snap := a.snapshot()
	message := tooltip(snap)
	if snap.Reason != "" {
		message += "\n" + snap.Reason
	}
	a.notifier.Show("WebMux 托盘已在运行", message)
}

// fileStamp detects settings file changes.
type fileStamp struct {
	exists bool
	mod    time.Time
	size   int64
}

func statFile(path string) fileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{exists: true, mod: info.ModTime(), size: info.Size()}
}

// watchSettings reloads tray.json when it changes. An invalid file keeps
// the tray running with defaults and is never rewritten until a menu
// action saves settings.
func (a *App) watchSettings() {
	ticker := time.NewTicker(settingsPoll)
	defer ticker.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
		}
		stamp := statFile(a.o.SettingsPath)
		a.mu.Lock()
		changed := stamp != a.stamp
		a.stamp = stamp
		a.mu.Unlock()
		if !changed {
			continue
		}
		settings, err := tray.LoadSettings(a.o.SettingsPath)
		a.mu.Lock()
		if err != nil {
			a.log.Printf("tray settings: %v", err)
			a.settingsWarn = invalidSetting
		} else {
			a.settingsWarn = ""
		}
		a.mu.Unlock()
		a.log.Printf("tray settings reloaded")
		a.apply(settings, true)
		a.kickUI()
	}
}

func cloneSettings(s tray.Settings) tray.Settings {
	s.Forward = slices.Clone(s.Forward)
	if s.Forward == nil {
		s.Forward = []string{}
	}
	return s
}

// apply makes next the effective settings. A distribution or mode change
// that stops a managed running instance needs confirmation when sessions
// exist; declining from the menu cancels, declining a file change keeps
// the current distribution and mode. It reports whether next applied.
func (a *App) apply(next tray.Settings, fromFile bool) bool {
	a.applyMu.Lock()
	defer a.applyMu.Unlock()
	a.mu.Lock()
	old := cloneSettings(a.settings)
	a.mu.Unlock()
	next = cloneSettings(next)
	if reflect.DeepEqual(old, next) {
		return true
	}
	snap := a.snapshot()
	if a.sup != nil && (old.Distro != next.Distro || old.Mode != next.Mode) && snap.Actions().Stop {
		prefix := "切换启动方式需要停止 WebMux，"
		if old.Distro != next.Distro {
			prefix = "切换发行版需要停止 WebMux，"
		}
		if !confirmSessions(prefix, snap.Sessions) {
			if !fromFile {
				return false
			}
			a.log.Printf("kept distribution and mode: the user declined to stop WebMux")
			next.Distro, next.Mode = old.Distro, old.Mode
		}
	}
	a.mu.Lock()
	a.settings = next
	if old.Distro != next.Distro {
		a.netMode, a.detected = "", false
	}
	a.mu.Unlock()
	if a.sup != nil {
		// The supervisor refreshes the new distribution at once unless it
		// first stops the instance running in the old one.
		now := (old.Distro != next.Distro && !snap.Actions().Stop) || idle(snap)
		a.svc.Retarget(next, now)
		a.sup.SetSettings(next)
	}
	if old.Distro != next.Distro {
		a.probe(true)
	}
	a.kickUI()
	return true
}

// changeSettings applies a menu change and saves it.
func (a *App) changeSettings(edit func(*tray.Settings)) {
	a.mu.Lock()
	next := cloneSettings(a.settings)
	a.mu.Unlock()
	edit(&next)
	if !a.apply(next, false) {
		return
	}
	if err := tray.SaveSettings(a.o.SettingsPath, next); err != nil {
		a.log.Printf("save tray settings: %v", err)
		a.notifier.Show("保存托盘设置失败", err.Error())
		return
	}
	a.mu.Lock()
	a.stamp = statFile(a.o.SettingsPath)
	a.settingsWarn = ""
	a.mu.Unlock()
	a.kickUI()
}

// shutdown stops the supervisor (and WebMux when stop is set), forwarding
// and pending clipboard secrets. It runs once.
func (a *App) shutdown(stop bool) {
	a.quitOnce.Do(func() {
		a.log.Printf("tray exiting (stop WebMux: %v)", stop)
		if a.sup != nil {
			a.sup.Quit(stop)
		}
		close(a.done)
		a.relays.closeAll()
		a.svc.Close()
		a.clip.Flush()
		a.mu.Lock()
		if a.journal != "" {
			_ = os.Remove(a.journal)
		}
		a.mu.Unlock()
	})
}

func (a *App) quit() {
	stop := false
	if a.sup != nil {
		if snap := a.sup.Snapshot(); snap.Managed() {
			switch askQuit(snap.Sessions, a.relays.any()) {
			case quitCancel:
				return
			case quitStop:
				stop = true
			}
		}
	}
	a.shutdown(stop)
	systray.Quit()
}

func (a *App) fail(title string, err error) {
	a.log.Printf("%s: %v", title, err)
	a.notifier.Show(title, err.Error())
}

// targetDistro is the distribution name of the instance being managed, for
// \\wsl.localhost paths.
func (a *App) targetDistro() string {
	if d := a.svc.Current().Target.Distro; d != "" {
		return d
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return defaultDistro(a.distros)
}

var errNotRunning = errors.New("WebMux 未运行")

func (a *App) openLocal() {
	snap := a.snapshot()
	if !snap.Actions().Access || !snap.HasStatus || snap.Status.URLs.Local == "" {
		a.notifier.Show("WebMux", tooltip(snap))
		return
	}
	if err := OpenURL(snap.Status.URLs.Local); err != nil {
		a.fail("无法打开浏览器", err)
	}
}

// copyLink copies url with the token fragment, so the link opens signed
// in. The token is fetched for this click only and the clipboard entry is
// handled like "复制 Token": kept out of history and cleared after 60 s.
func (a *App) copyLink(url string) {
	if url == "" {
		a.fail("复制失败", errNotRunning)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token, err := a.svc.Current().Token(ctx)
	if err != nil {
		a.fail("复制失败", err)
		return
	}
	if token == "" {
		if err := a.clip.Write(url, false); err != nil {
			a.fail("复制失败", err)
			return
		}
		a.notifier.Show("WebMux", "已复制 "+url)
		return
	}
	release := a.log.Redact(token)
	err = a.clip.Write(contract.AccessURL(url, token), true)
	release()
	if err != nil {
		a.fail("复制失败", err)
		return
	}
	a.notifier.Show("WebMux", "已复制 "+url+"（含 Token，60 秒后自动清除）")
}

// copyToken fetches the token for this click only; it is redacted from the
// log while held and never cached, shown or written to disk.
func (a *App) copyToken() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	token, err := a.svc.Current().Token(ctx)
	if err != nil {
		a.fail("复制 Token 失败", err)
		return
	}
	release := a.log.Redact(token)
	err = a.clip.Write(token, true)
	release()
	if err != nil {
		a.fail("复制 Token 失败", err)
		return
	}
	a.notifier.Show("WebMux", "已复制 Token（60 秒后自动清除）")
}

func (a *App) initConfig() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := a.svc.Current().ConfigInit(ctx); err != nil {
		a.fail("初始化配置失败", err)
		return
	}
	a.log.Printf("webmux config init succeeded")
	a.notifier.Show("WebMux", "已生成配置与 Token，重启 WebMux 后生效")
	if a.sup == nil {
		return
	}
	a.sup.Refresh()
	snap := a.sup.Snapshot()
	if !snap.Actions().Restart {
		return
	}
	text := "已生成配置与 Token，重启 WebMux 后生效。\n\n现在重启 WebMux 吗？"
	if snap.Sessions > 0 {
		text += fmt.Sprintf("重启将结束 %d 个终端会话，且不可恢复。", snap.Sessions)
	}
	if confirm(text) {
		a.sup.Restart()
	}
}

// installWebmux downloads and installs the release server into WSL, then
// rechecks; a running server keeps its old binary until restarted.
func (a *App) installWebmux() {
	if !a.installing.CompareAndSwap(false, true) {
		return
	}
	a.kickUI()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	err := a.svc.Current().Install(ctx)
	cancel()
	a.installing.Store(false)
	a.kickUI()
	if err != nil {
		a.fail("安装 webmux 失败", err)
		return
	}
	a.notifier.Show("WebMux", "已安装 webmux "+a.o.Version)
	a.sup.Recheck()
	snap := a.sup.Snapshot()
	if !snap.HasStatus || snap.Status.State != contract.StateRunning {
		return
	}
	text := "新版本在重启 WebMux 后生效，重启会结束所有终端会话。"
	if !snap.Actions().Restart {
		messageBox(text, windows.MB_OK|windows.MB_ICONINFORMATION)
		return
	}
	if snap.Sessions > 0 {
		text += fmt.Sprintf("\n\n当前有 %d 个终端会话。", snap.Sessions)
	}
	if confirm(text + "\n\n现在重启 WebMux 吗？") {
		a.sup.Restart()
	}
}

// toggleTunnel turns the public tunnel off, or on after the risk is
// confirmed. The state is never saved: it ends with the instance.
func (a *App) toggleTunnel() {
	enable := !a.snapshot().TunnelActive()
	if enable && !confirm(tray.TunnelWarning) {
		a.kickUI()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	_, err := a.svc.Current().SetTunnel(ctx, enable)
	cancel()
	a.sup.Refresh()
	if err == nil {
		return
	}
	if !enable {
		a.fail("关闭公网隧道失败", err)
		return
	}
	snap := a.snapshot()
	if snap.HasStatus && !snap.Status.Config.Exists {
		if confirm("开启公网隧道失败：" + err.Error() + "\n\n需要至少 43 个字符的 Token。现在初始化配置（生成 Token）吗？之后需要重启 WebMux。") {
			a.initConfig()
		}
		return
	}
	a.fail("开启公网隧道失败", fmt.Errorf("%w；请在配置中设置至少 43 个字符的 Token 并重启 WebMux", err))
}

func (a *App) openServiceLog() {
	snap := a.snapshot()
	a.mu.Lock()
	v := view{settings: a.settings, detection: a.detection, detected: a.detected}
	a.mu.Unlock()
	if systemdMode(snap, v) {
		a.openJournal()
		return
	}
	path, err := WSLPath(a.targetDistro(), snap.Status.LogFile)
	if err == nil {
		err = OpenInNotepad(path)
	}
	if err != nil {
		a.fail("无法打开 WebMux 日志", err)
	}
}

// openJournal exports the unit's recent journal to a temporary file.
func (a *App) openJournal() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inv := a.svc.Current().Target.Journal()
	res, err := a.o.Runner.Run(ctx, inv.Argv, inv.Env)
	if err == nil && res.Code != 0 {
		err = fmt.Errorf("journalctl: exit %d: %s", res.Code, wslclient.DecodeOutput(res.Stderr))
	}
	if err != nil {
		a.fail("无法读取 systemd 日志", err)
		return
	}
	f, err := os.CreateTemp("", "webmux-journal-*.log")
	if err != nil {
		a.fail("无法读取 systemd 日志", err)
		return
	}
	_, err = f.WriteString(wslclient.DecodeOutput(res.Stdout))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		a.fail("无法读取 systemd 日志", err)
		return
	}
	a.mu.Lock()
	if a.journal != "" {
		_ = os.Remove(a.journal)
	}
	a.journal = f.Name()
	a.mu.Unlock()
	if err := OpenInNotepad(f.Name()); err != nil {
		a.fail("无法打开 systemd 日志", err)
	}
}

func (a *App) openConfig() {
	snap := a.snapshot()
	path, err := WSLPath(a.targetDistro(), snap.Status.Config.Path)
	if err == nil {
		err = OpenInNotepad(path)
	}
	if err != nil {
		a.fail("无法打开 WebMux 配置文件", err)
	}
}

// editSettings opens tray.json, creating it with defaults only if missing.
func (a *App) editSettings() {
	path := a.o.SettingsPath
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := tray.SaveSettings(path, tray.DefaultSettings()); err != nil {
			a.fail("无法创建托盘设置", err)
			return
		}
		a.mu.Lock()
		a.stamp = statFile(path)
		a.mu.Unlock()
	}
	if err := OpenInNotepad(path); err != nil {
		a.fail("无法打开托盘设置", err)
	}
}

func (a *App) toggleAutostart() {
	on := !AutostartEnabled(a.o.Executable)
	if err := SetAutostart(on, a.o.Executable); err != nil {
		a.fail("无法修改登录启动项", err)
	}
	a.kickUI()
}

func (a *App) stopWebMux() {
	snap := a.sup.Snapshot()
	if confirmSessions("停止 WebMux ", snap.Sessions) {
		a.sup.Stop()
	}
}

func (a *App) restartWebMux() {
	snap := a.sup.Snapshot()
	if confirmSessions("重启 WebMux ", snap.Sessions) {
		a.sup.Restart()
	}
}

func (a *App) toggleForward(addr string) {
	a.changeSettings(func(s *tray.Settings) {
		if i := slices.Index(s.Forward, addr); i >= 0 {
			s.Forward = slices.Delete(s.Forward, i, i+1)
		} else {
			s.Forward = append(s.Forward, addr)
		}
	})
}

func (a *App) bindings() bindings {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.bound
}

// on runs f for each click of it. Clicks arriving while f runs (a dialog
// is open, for example) are dropped by fyne.io/systray.
func on(it *item, f func()) {
	go func() {
		for range it.mi.ClickedCh {
			f()
		}
	}()
}

// listen connects the menu items to their actions.
func (a *App) listen() {
	m := a.menu
	on(m.logService, a.openServiceLog)
	on(m.logTray, func() {
		if err := OpenInNotepad(a.log.Path()); err != nil {
			a.fail("无法打开托盘日志", err)
		}
	})
	on(m.quit, a.quit)
	if a.sup == nil {
		return
	}
	on(m.open, a.openLocal)
	on(m.copyLocal, func() { a.copyLink(a.snapshot().Status.URLs.Local) })
	on(m.copyRemote, func() {
		if b := a.bindings(); len(b.remote) > 0 {
			a.copyLink(b.remote[0].URL)
		}
	})
	for i, it := range m.remoteLinks {
		on(it, func() {
			if b := a.bindings(); i < b.reported {
				a.copyLink(b.remote[i].URL)
			}
		})
	}
	for i, it := range m.forwardLinks {
		on(it, func() {
			if b := a.bindings(); b.reported+i < len(b.remote) {
				a.copyLink(b.remote[b.reported+i].URL)
			}
		})
	}
	on(m.copyToken, a.copyToken)
	on(m.tunnel, a.toggleTunnel)
	on(m.copyPublic, func() {
		a.copyLink(tray.TunnelEntry(a.snapshot()).PublicURL)
	})
	on(m.install, a.installWebmux)
	on(m.fwInit, a.initConfig)
	for i, it := range m.fwCandidates {
		on(it, func() {
			if b := a.bindings(); i < len(b.candidates) {
				a.toggleForward(b.candidates[i].Address)
			}
		})
	}
	on(m.start, a.sup.Start)
	on(m.stop, a.stopWebMux)
	on(m.restart, a.restartWebMux)
	on(m.openConfig, a.openConfig)
	on(m.initConfig, a.initConfig)
	on(m.distroDefault, func() { a.changeSettings(func(s *tray.Settings) { s.Distro = "" }) })
	for i, it := range m.distros {
		on(it, func() {
			if b := a.bindings(); i < len(b.distros) {
				name := b.distros[i]
				a.changeSettings(func(s *tray.Settings) { s.Distro = name })
			}
		})
	}
	on(m.modeAuto, func() { a.changeSettings(func(s *tray.Settings) { s.Mode = tray.ModeAuto }) })
	on(m.modeTray, func() { a.changeSettings(func(s *tray.Settings) { s.Mode = tray.ModeTray }) })
	on(m.modeSystemd, func() { a.changeSettings(func(s *tray.Settings) { s.Mode = tray.ModeSystemd }) })
	on(m.editSettings, a.editSettings)
	on(m.autostart, a.toggleAutostart)
}
