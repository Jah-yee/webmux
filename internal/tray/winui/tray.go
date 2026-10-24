//go:build windows

package winui

import (
	"bytes"
	"context"
	_ "embed"
	"net"
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
		// A blocked or incompatible tray never polls the installed file;
		// opening the menu is the user's retry.
		if snap := a.sup.Snapshot(); snap.Blocker != "" || snap.Compat.Incompatible() {
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
