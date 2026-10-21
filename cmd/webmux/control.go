package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"

	"github.com/rainchan/webmux/internal/client"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
)

// clientFlags creates a flag set with the shared --config flag.
func clientFlags(name string, stderr io.Writer) (*flag.FlagSet, *string) {
	flags := flag.NewFlagSet("webmux "+name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("config", config.DefaultPath(), "TOML configuration path")
	return flags, path
}

// parseClient parses args and locates the configured instance. ok is false
// after a usage error.
func parseClient(flags *flag.FlagSet, path *string, args []string) (client.Target, bool) {
	if err := flags.Parse(args); err != nil {
		return client.Target{}, false
	}
	if flags.NArg() > 0 {
		_, _ = fmt.Fprintf(flags.Output(), "webmux: unexpected argument %q\n", flags.Arg(0))
		return client.Target{}, false
	}
	explicit := false
	flags.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "config" })
	return client.Locate(*path, explicit), true
}

func writeJSON(w io.Writer, v any) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(v)
}

func statusCommand(args []string, stdout, stderr io.Writer) int {
	flags, path := clientFlags("status", stderr)
	asJSON := flags.Bool("json", false, "print the status contract v1 as JSON")
	reveal := flags.Bool("reveal-token", false, "include the token in the JSON output")
	target, ok := parseClient(flags, path, args)
	if !ok {
		return contract.ExitUsage
	}
	status, code := client.Status(context.Background(), target, *reveal && *asJSON, client.NetInterfaces)
	if *asJSON {
		writeJSON(stdout, status)
	} else {
		printStatus(stdout, status)
	}
	if status.Config.Error != "" {
		_, _ = fmt.Fprintln(stderr, "webmux: configuration:", status.Config.Error)
	}
	return code
}

func printStatus(w io.Writer, s contract.StatusV1) {
	line := func(format string, args ...any) { _, _ = fmt.Fprintf(w, format+"\n", args...) }
	switch s.State {
	case contract.StateRunning:
		pid := "unknown"
		if s.PID != nil {
			pid = fmt.Sprint(*s.PID)
		}
		line("WebMux is running (PID %s, version %s, launcher %s)", pid, valueOr(s.Version, "unknown"), valueOr(s.Launcher, "unknown"))
		if s.Sessions != nil {
			line("Sessions: %d", *s.Sessions)
		}
		if s.Health.Status != contract.HealthOK {
			line("Health:   %s %s", s.Health.Status, s.Health.Message)
		}
	case contract.StateStopped:
		line("WebMux is not running")
	case contract.StateUnresponsive:
		line("WebMux process exists but does not respond: %s", s.Health.Message)
	default:
		line("WebMux status unknown: %s", valueOr(s.Health.Message, s.Config.Error))
	}
	if s.State == contract.StateRunning {
		line("Listen:   %s", s.Listen)
		line("Local:    %s", s.URLs.Local)
		for _, remote := range s.URLs.Remote {
			line("Remote:   %s (%s, %s)", remote.URL, remote.Interface, remote.Kind)
		}
	}
	if s.Config.Path != "" {
		exists := ""
		if !s.Config.Exists {
			exists = " (not found; defaults)"
		}
		line("Config:   %s%s", s.Config.Path, exists)
	}
	if s.LogFile != "" {
		line("Log file: %s", s.LogFile)
	}
	if s.Socket != "" && s.State != contract.StateStopped {
		line("Socket:   %s", s.Socket)
	}
	if len(s.Drift) > 0 {
		line("Changed since start (restart to apply): %s", strings.Join(s.Drift, ", "))
	}
	for _, warning := range s.Warnings {
		line("Warning:  %s", warning)
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func stopCommand(args []string, stdout, stderr io.Writer) int {
	flags, path := clientFlags("stop", stderr)
	asJSON := flags.Bool("json", false, "print the stop result as JSON")
	force := flags.Bool("force", false, "stop an instance managed by systemd or launchd")
	timeout := flags.Duration("timeout", client.DefaultStopTimeout, "wait this long before SIGKILL")
	target, ok := parseClient(flags, path, args)
	if !ok {
		return contract.ExitUsage
	}
	result, code := client.Stop(context.Background(), target, *force, *timeout)
	switch {
	case *asJSON:
		writeJSON(stdout, result)
	case code == contract.ExitOK:
		_, _ = fmt.Fprintln(stdout, stopMessage(result))
	default:
		_, _ = fmt.Fprintln(stderr, "webmux:", result.Message)
	}
	return code
}

func stopMessage(r contract.StopV1) string {
	switch r.Result {
	case contract.StopStopped:
		return "WebMux stopped"
	case contract.StopKilled:
		return "WebMux was forcibly killed: " + r.Message
	case contract.StopNotRunning:
		return "WebMux is not running"
	}
	return r.Message
}

// bridgeCommand copies bytes between stdio and one local API connection.
// Nothing reaches stdout unless the connection succeeds.
func bridgeCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	signal.Ignore(syscall.SIGPIPE)
	flags, path := clientFlags("bridge", stderr)
	target, ok := parseClient(flags, path, args)
	if !ok {
		return contract.ExitUsage
	}
	if target.ConfigErr != nil {
		_, _ = fmt.Fprintln(stderr, "webmux bridge: configuration:", target.ConfigErr)
		return contract.ExitError
	}
	ctx, cancel := context.WithTimeout(context.Background(), client.RequestTimeout)
	conn, err := localclient.UnixDialer{Path: target.Socket}.DialContext(ctx)
	cancel()
	if errors.Is(err, localclient.ErrNotRunning) {
		_, _ = fmt.Fprintln(stderr, "webmux bridge: WebMux is not running:", err)
		return contract.ExitNotRunning
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "webmux bridge:", err)
		return contract.ExitError
	}
	defer func() { _ = conn.Close() }()
	go func() {
		_, _ = io.Copy(conn, stdin)
		// End of input: let the server finish its responses, then close.
		if half, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = half.CloseWrite()
		}
	}()
	// Returns when the server closes or stdout is closed (EPIPE, not SIGPIPE).
	_, _ = io.Copy(stdout, conn)
	return contract.ExitOK
}
