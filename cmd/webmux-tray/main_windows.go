// Command webmux-tray runs WebMux inside WSL from the Windows notification
// area. Build it with -ldflags "-H=windowsgui".
package main

import (
	"fmt"
	stdlog "log"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"

	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/tray"
	"github.com/rainchan/webmux/internal/tray/winui"
	"github.com/rainchan/webmux/internal/wslclient"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	if err := run(); err != nil {
		_, _ = windows.MessageBox(0, windows.StringToUTF16Ptr("WebMux 托盘无法启动："+err.Error()), windows.StringToUTF16Ptr("WebMux"), windows.MB_OK|windows.MB_ICONERROR)
		os.Exit(1)
	}
}

func run() error {
	instance, err := winui.AcquireInstance()
	if err != nil {
		return err
	}
	if instance == nil {
		// Another tray runs and now shows its status.
		return nil
	}

	appData, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
	if err != nil {
		return err
	}
	localAppData, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return err
	}

	log, err := tray.OpenLog(filepath.Join(localAppData, "WebMux", "tray.log"), tray.DefaultLogLimit)
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	stdlog.SetOutput(log) // fyne.io/systray reports errors through package log

	runner := tray.ExecRunner{}
	keepalive := &tray.Keepalive{
		Runner:   runner,
		Path:     filepath.Join(localAppData, "WebMux", "keepalive.json"),
		Identify: winui.Identify,
		Kill:     winui.Kill,
	}
	environ := os.Environ()
	newService := func(s tray.Settings) *tray.WSLService {
		return &tray.WSLService{
			Runner:      runner,
			Target:      tray.TargetOf(s),
			LogFile:     s.LogFile,
			Environ:     environ,
			KeepAlive:   keepalive,
			Version:     version,
			DownloadDir: filepath.Join(localAppData, "WebMux", "downloads"),
			Digests:     tray.BuiltinDigests(),
			Logf:        log.Printf,
			Dial: func(t tray.Target) localclient.Dialer {
				return &wslclient.BridgeDialer{
					Command: func() *exec.Cmd {
						inv := t.Bridge()
						return wslclient.NewCommand(inv.Argv, inv.Env...)
					},
					Stderr: log,
				}
			},
		}
	}

	winui.Run(winui.Options{
		Version:      version,
		SettingsPath: filepath.Join(appData, "WebMux", "tray.json"),
		Log:          log,
		Runner:       runner,
		NewService:   newService,
		Reach:        winui.NewReacher(),
		Instance:     instance,
		Executable:   exe,
	})
	return nil
}
