package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rainchan/webmux/cmd/internal/assemble"
	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/files"
	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/pathutil"
	"github.com/rainchan/webmux/internal/push"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/tunnel"
	"github.com/rainchan/webmux/internal/workspace"
)

var version = "dev"

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

const subcommands = "serve, version, status, stop, bridge, config, service, run, attach, ls, open, help"

const cliHelp = `Usage: webmux [command] [options]

Commands:
  serve             Run the server in the foreground (default)
  version           Print the version; --json adds the control contract
  status            Show server health and access URLs
  stop              Stop the server through its local control API
  config            Initialize, locate, show or validate configuration
  service install   Install and start a service for the current user
  service uninstall Stop and remove the user service; keep configuration
  service start     Start the installed user service
  service stop      Stop the user service (ends all terminal sessions)
  service restart   Restart the user service (ends all terminal sessions)
  service status    Show the user service manager's status
  service logs      Show recent logs; add --follow (-f) to stream
  run               Create a terminal session
  attach            Attach to a terminal session
  ls                List terminal sessions
  open              Open WebMux in the browser
  bridge            Bridge the local socket over standard input/output
  help              Show this help

Server options: --config PATH, --listen HOST:PORT, --token TOKEN,
                --tunnel, --log-file PATH
Service install options: --config PATH, --force, --dry-run

Linux uses systemd --user; macOS uses a launchd LaunchAgent. Do not use sudo.
Run "webmux <command> --help" for command options.
`

// dispatch routes by the first argument. No argument or a flag first keeps
// the original server invocation used by existing service templates.
func dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "help", "--help", "-help", "-h":
			_, _ = fmt.Fprint(stdout, cliHelp)
			return contract.ExitOK
		case "--version":
			_, _ = fmt.Fprintln(stdout, "webmux", version)
			return contract.ExitOK
		}
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runServe(args)
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "version":
		return versionCommand(args[1:], stdout, stderr)
	case "status":
		return statusCommand(args[1:], stdout, stderr)
	case "stop":
		return stopCommand(args[1:], stdout, stderr)
	case "bridge":
		return bridgeCommand(args[1:], os.Stdin, stdout, stderr)
	case "run", "attach", "ls", "open":
		return sessionCommand(args[0], args[1:], stdout, stderr)
	case "config":
		return configCommand(args[1:], stdout, stderr)
	case "service":
		return serviceCommand(args[1:], stdout, stderr)
	}
	_, _ = fmt.Fprintf(stderr, "webmux: unknown subcommand %q\navailable subcommands: %s\n", args[0], subcommands)
	return contract.ExitUsage
}

// versionCommand never reads configuration or starts anything: the Windows
// tray runs it to decide compatibility before any other call.
func versionCommand(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("webmux version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the version and control contract as JSON")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return contract.ExitOK
		}
		return contract.ExitUsage
	}
	if flags.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "webmux: unexpected argument %q\n", flags.Arg(0))
		return contract.ExitUsage
	}
	if *asJSON {
		writeJSON(stdout, contract.VersionV1{V: contract.V, Version: version, Contract: contract.V})
		return contract.ExitOK
	}
	_, _ = fmt.Fprintln(stdout, "webmux", version)
	return contract.ExitOK
}

// serverRun is the validated server configuration plus how it was obtained.
type serverRun struct {
	config.Config
	ConfigPath     string
	ConfigExplicit bool
	// ConfigFound reports whether the configuration file existed at start.
	ConfigFound bool
	LogFile     string
	// TokenGenerated marks a per-process token absent from the config.
	TokenGenerated bool
}

func loadConfig(args []string) (serverRun, error) {
	flags := flag.NewFlagSet("webmux serve", flag.ContinueOnError)
	path := flags.String("config", config.DefaultPath(), "TOML configuration path")
	listen := flags.String("listen", "", "HTTP listen address")
	token := flags.String("token", "", "optional shared token")
	tunnelFlag := flags.Bool("tunnel", false, "expose webmux through a Cloudflare Tunnel")
	logFile := flags.String("log-file", "", "write stdout and stderr to this file, keeping the previous one as .1")
	if err := flags.Parse(args); err != nil {
		return serverRun{}, err
	}
	if flags.NArg() != 0 {
		return serverRun{}, fmt.Errorf("unexpected argument %q; see webmux help", flags.Arg(0))
	}
	explicit := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	run := serverRun{ConfigExplicit: explicit["config"]}
	var err error
	if run.ConfigPath, err = pathutil.Expand(*path); err != nil {
		return run, err
	}
	if *logFile != "" {
		if run.LogFile, err = pathutil.Expand(*logFile); err != nil {
			return run, err
		}
	}
	_, statErr := os.Stat(run.ConfigPath)
	run.ConfigFound = statErr == nil
	c, err := config.Load(run.ConfigPath, run.ConfigExplicit)
	if err != nil {
		return run, err
	}
	if explicit["listen"] {
		c.Listen = *listen
	}
	if explicit["token"] {
		c.Token = *token
	}
	if explicit["tunnel"] {
		c.Tunnel.Enabled = *tunnelFlag
	}
	if err = c.Validate(); err != nil {
		return run, err
	}
	hadToken := c.Token != ""
	if err = ensureToken(&c); err != nil {
		return run, err
	}
	run.Config = c
	run.TokenGenerated = !hadToken && c.Token != ""
	return run, nil
}

// ensureToken keeps every network entry point authenticated: an empty token
// is generated for this process unless the server listens only on loopback
// without a tunnel, and a tunnel token must not be weaker than that form.
func ensureToken(c *config.Config) error {
	if c.Token == "" {
		if c.Tunnel.Enabled || !loopbackListen(c.Listen) {
			c.Token = generateToken()
		}
		return nil
	}
	if c.Tunnel.Enabled && len(c.Token) < minTunnelTokenLength {
		return fmt.Errorf("tunnel mode requires the webmux token to be at least %d characters; leave it empty to generate one, or create one with openssl rand -base64 32", minTunnelTokenLength)
	}
	return nil
}

func runServe(args []string) int {
	if err := run(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return contract.ExitOK
		}
		slog.Error("webmux failed", "error", err)
		return contract.ExitError
	}
	return contract.ExitOK
}

func run(args []string) error {
	c, err := loadConfig(args)
	if err != nil {
		return err
	}
	if c.LogFile != "" {
		if err := redirectOutput(c.LogFile); err != nil {
			return fmt.Errorf("log file: %w", err)
		}
	}
	applyLaunchEnv()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, c, os.Stdout)
}

// serve runs until ctx ends. c must come from loadConfig, which generates or
// vets the token. Access URLs go to out, never to slog, so tokens stay out of
// structured logs.
func serve(ctx context.Context, c serverRun, out io.Writer) error {
	locator := tunnel.Locator{Explicit: c.Tunnel.Cloudflared, StateDir: c.StateDir, Mirrors: c.Tunnel.DownloadMirrors}
	var cloudflared string
	if c.Tunnel.Enabled {
		path, err := locator.Find(ctx)
		if err != nil {
			return fmt.Errorf("cloudflare tunnel: %w", err)
		}
		cloudflared = path
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return err
	}
	m := session.NewManager(assemble.SessionOptions(c.Config))
	// Opening never fails: a locked, corrupt or unreadable store only
	// disables launch configs, and the endpoints answer 503 with the reason.
	launches := launch.Open(c.StateDir)
	defer launches.Close()
	if err := launches.Unavailable(); err != nil {
		slog.Warn("launch configs unavailable", "error", err)
	}
	workspaces := workspace.New(c.StateDir, m, launches, c.DefaultCwd)
	defer workspaces.Close()
	codes := codeview.New(m)
	defer codes.Close()
	pushes := push.New(c.Push, c.StateDir, m, slog.Default(), nil)
	// The public origin always exists: it allows no host until a tunnel
	// reports ready, so a server without a tunnel behaves as before.
	public := &api.PublicOrigin{}
	local := localOrigin(listener.Addr())
	tunnels := newTunnelController(c.Tunnel, local, c.Token, public, locator.Find, out, slog.Default())
	deps := api.Deps{
		Config: c.Config, Manager: m, Workspace: workspaces, Code: codes,
		Files: files.New(c.DefaultCwd), Push: pushes, Launch: launches,
	}
	rt := startRuntime(ctx, c, listener.Addr(), deps, cancel, tunnels)
	deps.Public, deps.Tunnel = public, tunnels.Status
	server := &http.Server{
		Handler:           api.New(deps),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	ended := make(chan error, 1)
	go func() { ended <- server.Serve(listener) }()
	rt.serve()
	slog.Info("webmux listening", "address", listener.Addr(), "version", version)
	localLink := local
	if c.Tunnel.Enabled || c.TokenGenerated {
		// Tunnel mode and generated tokens print the token because the
		// operator cannot otherwise know it. A configured non-tunnel token
		// stays out of captured stdout, which a service manager persists.
		localLink = contract.AccessURL(local, c.Token)
	}
	_, _ = fmt.Fprintln(out, "WebMux local:  "+localLink)
	if c.Tunnel.Enabled {
		tunnels.startLocated(cloudflared)
	}
	select {
	case <-ctx.Done():
	case err = <-ended:
	}
	cancel() // Cancel HTTP and hijacked WebSocket handlers before joining sessions.
	tunnels.Close()
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	pushes.Close() // Stop sending before sessions exit.
	m.Shutdown(shutdown)
	if shutdownErr := server.Shutdown(shutdown); shutdownErr != nil {
		_ = server.Close()
		slog.Warn("forced HTTP shutdown", "error", shutdownErr)
	}
	rt.close(shutdown)
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
