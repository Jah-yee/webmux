package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/rainchan/webmux/deploy"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/pathutil"
	"github.com/rainchan/webmux/internal/service"
)

func serviceCommand(args []string, out, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		_, _ = fmt.Fprintln(out, "Usage: webmux service <install|uninstall|start|stop|restart|status|logs> [options]")
		return contract.ExitOK
	}
	action := args[0]
	switch action {
	case "install", "uninstall", "start", "stop", "restart", "status", "logs":
	default:
		fmt.Fprintf(stderr, "webmux service: unknown command %q\n", action)
		return contract.ExitUsage
	}
	flags := flag.NewFlagSet("webmux service "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	var path string
	var force, dryRun, follow bool
	if action == "install" {
		flags.StringVar(&path, "config", config.DefaultPath(), "TOML configuration path (default is created if missing)")
		flags.BoolVar(&force, "force", false, "replace the service definition and restart (ends terminal sessions)")
		flags.BoolVar(&dryRun, "dry-run", false, "print the service definition without changing anything")
	}
	if action == "logs" {
		flags.BoolVar(&follow, "follow", false, "stream log output until interrupted")
		flags.BoolVar(&follow, "f", false, "stream log output until interrupted")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return contract.ExitOK
		}
		return contract.ExitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "webmux service: unexpected argument %q\n", flags.Arg(0))
		return contract.ExitUsage
	}
	if action == "install" {
		expanded, err := pathutil.Expand(path)
		if err != nil {
			fmt.Fprintln(stderr, "webmux service:", err)
			return contract.ExitUsage
		}
		path = expanded
	}
	manager, err := service.New(out, stderr, initializeServiceConfig)
	if err != nil {
		fmt.Fprintln(stderr, "webmux service:", err)
		return contract.ExitError
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if action == "install" {
		explicit := false
		flags.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "config" })
		err = manager.Install(ctx, path, !explicit, force, dryRun)
	} else {
		err = manager.Control(ctx, action, follow)
	}
	if err != nil {
		fmt.Fprintln(stderr, "webmux service:", err)
		return contract.ExitError
	}
	return contract.ExitOK
}

// Reuse the same commented template, token generator and exclusive writer as
// config init; installing a service never replaces an existing config.
func initializeServiceConfig(path string) error {
	content, err := renderConfigTemplate(deploy.ConfigExample, generateToken(), "")
	if err != nil {
		return err
	}
	return writeNewConfig(path, content, false)
}
