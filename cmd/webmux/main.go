package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/tunnel"
	"github.com/rainchan/webmux/internal/workspace"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("webmux", version)
		return
	}
	if err := run(); err != nil {
		slog.Error("webmux failed", "error", err)
		os.Exit(1)
	}
}
func loadConfig(args []string) (config.Config, error) {
	flags := flag.NewFlagSet("webmux", flag.ContinueOnError)
	path := flags.String("config", config.DefaultPath(), "TOML configuration path")
	listen := flags.String("listen", "", "HTTP listen address")
	token := flags.String("token", "", "optional shared token")
	tunnelFlag := flags.Bool("tunnel", false, "expose webmux through a Cloudflare Tunnel")
	if err := flags.Parse(args); err != nil {
		return config.Config{}, err
	}
	explicit := map[string]bool{}
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	c, err := config.Load(*path, explicit["config"])
	if err != nil {
		return c, err
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
		return c, err
	}
	if err = ensureTunnelToken(&c); err != nil {
		return c, err
	}
	return c, nil
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

func run() error {
	c, err := loadConfig(os.Args[1:])
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, c, os.Stdout)
}

// serve runs until ctx ends. c must come from loadConfig, which generates or
// vets the tunnel token. Access URLs go to out, never to slog, so tokens stay
// out of structured logs.
func serve(ctx context.Context, c config.Config, out io.Writer) error {
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
	server := &http.Server{
		Handler:           api.New(c, m, workspaces, public, codes),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	ended := make(chan error, 1)
	go func() { ended <- server.Serve(listener) }()
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
