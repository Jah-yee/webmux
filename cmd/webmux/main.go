package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/client"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/tunnel"
	"github.com/rainchan/webmux/internal/workspace"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

const subcommands = "serve, version, status, stop, bridge, config"

// dispatch routes by the first argument. No argument or a flag first keeps
// the original server invocation used by existing service templates.
func dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return runServe(args)
	}
	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "version":
		_, _ = fmt.Fprintln(stdout, "webmux", version)
		return contract.ExitOK
	case "status":
		return statusCommand(args[1:], stdout, stderr)
	case "stop":
		return stopCommand(args[1:], stdout, stderr)
	case "bridge":
		return bridgeCommand(args[1:], os.Stdin, stdout, stderr)
	case "config":
		return configCommand(args[1:], stdout, stderr)
	}
	_, _ = fmt.Fprintf(stderr, "webmux: unknown subcommand %q\navailable subcommands: %s\n", args[0], subcommands)
	return contract.ExitUsage
}

// serverRun is the validated server configuration plus how it was obtained.
type serverRun struct {
	config.Config
	ConfigPath     string
	ConfigExplicit bool
	// ConfigFound reports whether the configuration file existed at start.
	ConfigFound bool
	LogFile     string
	// TokenGenerated marks a per-process tunnel token.
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
	explicit := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	run := serverRun{ConfigExplicit: explicit["config"]}
	var err error
	if run.ConfigPath, err = client.ExpandHome(*path); err != nil {
		return run, err
	}
	if *logFile != "" {
		if run.LogFile, err = client.ExpandHome(*logFile); err != nil {
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
	if err = ensureTunnelToken(&c); err != nil {
		return run, err
	}
	run.Config = c
	run.TokenGenerated = !hadToken && c.Token != ""
	return run, nil
}

// ensureTunnelToken keeps a public entry point authenticated: an empty token
// is generated for this process, and a configured one must not be weaker than
// that generated form.
func ensureTunnelToken(c *config.Config) error {
	if !c.Tunnel.Enabled {
		return nil
	}
	if c.Token == "" {
		c.Token = generateToken()
		return nil
	}
	if len(c.Token) < minTunnelTokenLength {
		return fmt.Errorf("tunnel mode requires the webmux token to be at least %d characters; leave it empty to generate one, or create one with openssl rand -base64 32", minTunnelTokenLength)
	}
	return nil
}

func runServe(args []string) int {
	if err := run(args); err != nil {
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, c, os.Stdout)
}

// serve runs until ctx ends. c must come from loadConfig, which generates or
// vets the tunnel token. Access URLs go to out, never to slog, so tokens stay
// out of structured logs.
func serve(ctx context.Context, c serverRun, out io.Writer) error {
	host, _, _ := net.SplitHostPort(c.Listen)
	if (host == "0.0.0.0" || host == "::" || host == "") && c.Token == "" {
		slog.Warn("SHELL EXPOSED: listening on all interfaces without token; isolate with Tailscale/WireGuard")
	}
	var cloudflared string
	if c.Tunnel.Enabled {
		locator := tunnel.Locator{Explicit: c.Tunnel.Cloudflared, StateDir: c.StateDir, Mirrors: c.Tunnel.DownloadMirrors}
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
	m := session.NewManager(c.SessionOptions())
	workspaces := workspace.New(c.StateDir, m, c.DefaultCwd)
	defer workspaces.Close()
	codes := codeview.New(m)
	defer codes.Close()
	var public *api.PublicOrigin
	if c.Tunnel.Enabled {
		public = &api.PublicOrigin{}
	}
	rt := startRuntime(ctx, c, listener.Addr(), m, workspaces, codes, cancel)
	server := &http.Server{
		Handler:           api.New(c.Config, m, workspaces, public, codes),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	ended := make(chan error, 1)
	go func() { ended <- server.Serve(listener) }()
	rt.serve()
	slog.Info("webmux listening", "address", listener.Addr(), "version", version)
	local := localOrigin(listener.Addr())
	localLink := local
	if c.Tunnel.Enabled {
		// Tunnel mode always prints the token because the operator cannot
		// otherwise know the generated one. A configured non-tunnel token
		// stays out of captured stdout, which a service manager persists.
		localLink = accessURL(local, c.Token)
	}
	_, _ = fmt.Fprintln(out, "WebMux local:  "+localLink)
	var tun *tunnel.Tunnel
	var watcher sync.WaitGroup
	if c.Tunnel.Enabled {
		tun, err = tunnel.Start(ctx, tunnel.Options{Binary: cloudflared, Local: local, Token: c.Tunnel.Token, Hostname: c.Tunnel.Hostname})
		if err != nil {
			slog.Error("cloudflare tunnel unavailable; serving locally only", "error", err)
		} else {
			watcher.Go(func() { watchTunnel(tun, public, c.Token, out) })
		}
	}
	select {
	case <-ctx.Done():
	case err = <-ended:
	}
	cancel() // Cancel HTTP and hijacked WebSocket handlers before joining sessions.
	if tun != nil {
		tun.Close()
		watcher.Wait()
	}
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
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

// watchTunnel authorizes and announces the public origin once cloudflared
// connects, and revokes it when cloudflared exits.
func watchTunnel(tun *tunnel.Tunnel, public *api.PublicOrigin, token string, out io.Writer) {
	for {
		select {
		case origin := <-tun.Ready():
			if err := public.Set(origin); err != nil {
				slog.Error("cloudflare tunnel reported an unusable origin", "error", err)
				continue
			}
			link := accessURL(origin, token)
			code, err := renderQR(link)
			if err != nil {
				slog.Error("render QR code", "error", err)
			}
			_, _ = fmt.Fprint(out, "WebMux public: "+link+"\n"+code)
		case <-tun.Done():
			public.Clear()
			if err := tun.Err(); err != nil {
				slog.Error("cloudflare tunnel stopped; public URL revoked, local access continues", "error", err)
			}
			return
		}
	}
}
