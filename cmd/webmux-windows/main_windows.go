//go:build windows

// Command webmux-windows (installed as webmux.exe) runs agents in WebMux
// inside WSL from PowerShell or Windows Terminal. Every connection is a
// `wsl.exe --exec webmux bridge` process speaking the local API, so no token
// or WSL networking setting is involved.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/rainchan/webmux/internal/attach"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/sessioncmd"
	"github.com/rainchan/webmux/internal/wslclient"
)

var version = "dev"

const usage = `usage: webmux [--distro NAME] [--webmux PATH] COMMAND [ARGS]

commands:
  run [--name N] [--cwd DIR] [--env K=V]... [--detach] -- COMMAND [ARG]...
  attach [--observe] SESSION     (Ctrl+] detaches)
  ls [--json]
  open [--browser] SESSION
  status [--json]
  version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("webmux", flag.ContinueOnError)
	global.SetOutput(stderr)
	global.Usage = func() { _, _ = fmt.Fprint(stderr, usage) }
	distro := global.String("distro", "", "WSL distribution (default: WEBMUX_DISTRO, the tray setting, then WSL's default)")
	webmux := global.String("webmux", "", "webmux inside WSL (default: WEBMUX_WSL_PATH, the tray setting, then ~/.local/bin/webmux)")
	if err := global.Parse(args); err != nil {
		return contract.ExitUsage
	}
	if global.NArg() == 0 {
		global.Usage()
		return contract.ExitUsage
	}
	command, rest := global.Arg(0), global.Args()[1:]
	if command == "version" {
		_, _ = fmt.Fprintln(stdout, "webmux-windows", version)
		return contract.ExitOK
	}
	target := wslclient.ResolveTarget(*distro, *webmux, os.Getenv, wslclient.TraySettingsPath(os.Getenv("APPDATA")))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c := &client{target: target, stdout: stdout, stderr: stderr}
	e := sessioncmd.Env{
		Connect:        c.connect,
		Stdout:         stdout,
		Stderr:         stderr,
		OpenConsole:    func() (sessioncmd.Console, error) { return attach.OpenConsole(func() { os.Exit(1) }) },
		OpenBrowser:    openBrowser,
		ResolveCwd:     c.resolveCwd,
		NotRunningHint: "start it from the WebMux tray or run `webmux serve` inside WSL",
		Label:          "Windows console",
	}
	switch command {
	case "run":
		return sessioncmd.Run(ctx, e, rest)
	case "attach":
		return sessioncmd.Attach(ctx, e, rest)
	case "ls":
		return sessioncmd.List(ctx, e, rest)
	case "open":
		return sessioncmd.Open(ctx, e, rest)
	case "status":
		return c.status(ctx, rest)
	}
	_, _ = fmt.Fprintf(stderr, "webmux: unknown command %q\n", command)
	global.Usage()
	return contract.ExitUsage
}

type client struct {
	target         wslclient.Target
	stdout, stderr io.Writer
}

// connect verifies webmux once, then dials through bridges. An older
// webmux treats every unknown first argument as `serve`, so `bridge` must
// never reach one: its flag list is checked first.
func (c *client) connect() (*localclient.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := wslclient.NewCommand(c.target.Command("-h"))
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := runContext(ctx, cmd); err != nil && output.Len() == 0 {
		return nil, fmt.Errorf("cannot run wsl.exe: %w", err)
	}
	text := wslclient.DecodeOutput(output.Bytes())
	switch {
	case strings.Contains(text, "-log-file"):
	case strings.Contains(strings.ToLower(text), "no such file") || strings.Contains(text, "execvpe"):
		return nil, fmt.Errorf("webmux not found in WSL at %s (set --webmux or WEBMUX_WSL_PATH)", c.target.Webmux)
	default:
		return nil, errors.New("the webmux inside WSL is too old for the local API; upgrade it")
	}
	return localclient.New(&wslclient.BridgeDialer{Command: func() *exec.Cmd {
		return wslclient.NewCommand(c.target.Bridge())
	}}), nil
}

func runContext(ctx context.Context, cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		<-done
		return ctx.Err()
	}
}

// resolveCwd maps the Windows working directory (or --cwd) into the
// target distribution; unmappable directories are a usage error.
func (c *client) resolveCwd(ctx context.Context, cwd string) (string, error) {
	base, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if cwd == "" {
		cwd = base
	}
	distro := c.target.Distro
	if distro == "" && strings.HasPrefix(cwd, `\\`) {
		if distro, err = defaultDistro(ctx); err != nil {
			return "", err
		}
	}
	linux, drive, err := wslclient.MapPath(cwd, base, distro)
	if err != nil || linux != "" {
		return linux, err
	}
	cmd := wslclient.NewCommand(c.target.Wslpath(drive))
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := runContext(ctx, cmd); err != nil {
		return "", fmt.Errorf("wslpath %s: %v %s", drive, err, strings.TrimSpace(wslclient.DecodeOutput(errOut.Bytes())))
	}
	return strings.TrimSpace(wslclient.DecodeOutput(out.Bytes())), nil
}

func defaultDistro(ctx context.Context) (string, error) {
	cmd := wslclient.NewCommand(wslclient.ListVerbose())
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := runContext(ctx, cmd); err != nil {
		return "", fmt.Errorf("wsl.exe --list --verbose: %w", err)
	}
	for _, d := range wslclient.ParseListVerbose(out.Bytes()) {
		if d.Default {
			return d.Name, nil
		}
	}
	return "", errors.New("no default WSL distribution")
}

// status uses the local API, or `webmux status --json` inside WSL when the
// server is not running.
func (c *client) status(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(c.stderr)
	asJSON := flags.Bool("json", false, "print the status contract v1 as JSON")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return contract.ExitUsage
	}
	local, err := c.connect()
	if err != nil {
		_, _ = fmt.Fprintln(c.stderr, "webmux:", err)
		return contract.ExitError
	}
	defer local.Close()
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	status, err := local.Status(requestCtx)
	cancel()
	code := contract.ExitOK
	if errors.Is(err, localclient.ErrNotRunning) {
		cmd := wslclient.NewCommand(c.target.Command("status", "--json"))
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, c.stderr
		runErr := runContext(ctx, cmd)
		var exit *exec.ExitError
		if runErr != nil && !errors.As(runErr, &exit) {
			_, _ = fmt.Fprintln(c.stderr, "webmux:", runErr)
			return contract.ExitError
		}
		code = cmd.ProcessState.ExitCode()
		if *asJSON {
			_, _ = c.stdout.Write(out.Bytes())
			return code
		}
		if err := json.Unmarshal(out.Bytes(), &status); err != nil {
			_, _ = fmt.Fprintln(c.stderr, "webmux: unreadable status:", err)
			return contract.ExitError
		}
	} else if err != nil {
		_, _ = fmt.Fprintln(c.stderr, "webmux:", err)
		return contract.ExitError
	} else if status.State != contract.StateRunning || status.Health.Status != contract.HealthOK {
		code = contract.ExitError
	}
	if *asJSON {
		encoder := json.NewEncoder(c.stdout)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(status)
		return code
	}
	sessioncmd.PrintStatus(c.stdout, status)
	return code
}

func openBrowser(url string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	target, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}
