package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/rainchan/webmux/internal/attach"
	"github.com/rainchan/webmux/internal/client"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/sessioncmd"
)

// sessionEnv connects the shared session commands to this machine's
// server through its local socket.
func sessionEnv(stdout, stderr io.Writer) sessioncmd.Env {
	path := config.DefaultPath()
	explicit := false
	return sessioncmd.Env{
		Flags: func(flags *flag.FlagSet) {
			flags.Func("config", "TOML configuration path (default "+path+")", func(value string) error {
				path, explicit = value, true
				return nil
			})
		},
		Connect: func() (*localclient.Client, error) {
			target := client.Locate(path, explicit)
			if target.ConfigErr != nil {
				return nil, fmt.Errorf("configuration: %w", target.ConfigErr)
			}
			return target.LocalClient(), nil
		},
		Stdout:         stdout,
		Stderr:         stderr,
		OpenConsole:    func() (sessioncmd.Console, error) { return attach.OpenConsole() },
		OpenBrowser:    openBrowser,
		ResolveCwd:     resolveCwd,
		NotRunningHint: "start it with `webmux serve` (or your service manager)",
		Label:          "terminal",
	}
}

// resolveCwd keeps "~" for the server to expand and makes other paths
// absolute; the default is the current directory.
func resolveCwd(_ context.Context, cwd string) (string, error) {
	if cwd == "~" || strings.HasPrefix(cwd, "~/") {
		return cwd, nil
	}
	if cwd == "" {
		return os.Getwd()
	}
	return filepath.Abs(cwd)
}

func openBrowser(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, url).Start()
}

// sessionCommand runs one of run, attach, ls and open. SIGINT and SIGTERM
// end the command (attachments restore the terminal first); in raw mode
// Ctrl+C is input for the session instead.
func sessionCommand(name string, args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	e := sessionEnv(stdout, stderr)
	switch name {
	case "run":
		return sessioncmd.Run(ctx, e, args)
	case "attach":
		return sessioncmd.Attach(ctx, e, args)
	case "ls":
		return sessioncmd.List(ctx, e, args)
	}
	return sessioncmd.Open(ctx, e, args)
}
