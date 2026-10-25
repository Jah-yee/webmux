package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strconv"

	"github.com/BurntSushi/toml"
	"github.com/rainchan/webmux/deploy"
	"github.com/rainchan/webmux/internal/client"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/pathutil"
)

const configUsage = "usage: webmux config path|show|validate|check|init [--config PATH] [--json]"

func configCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, configUsage)
		return contract.ExitUsage
	}
	switch args[0] {
	case "--help", "-h", "help":
		_, _ = fmt.Fprintln(stdout, configUsage)
		return contract.ExitOK
	case "path":
		return configPath(args[1:], stdout, stderr)
	case "show":
		return configShow(args[1:], stdout, stderr)
	case "validate", "check":
		return configValidate(args[1:], stdout, stderr)
	case "init":
		return configInit(args[1:], stdout, stderr)
	}
	_, _ = fmt.Fprintf(stderr, "webmux config: unknown subcommand %q\n%s\n", args[0], configUsage)
	return contract.ExitUsage
}

// configFlags parses --config (with ~/ expansion) and returns the absolute
// path, whether it was explicit, and false after a usage error.
func configFlags(name string, args []string, stderr io.Writer, define func(*flag.FlagSet)) (string, bool, bool) {
	flags, path := clientFlags("config "+name, stderr)
	if define != nil {
		define(flags)
	}
	if err := flags.Parse(args); err != nil {
		return "", false, false
	}
	if flags.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "webmux config %s: unexpected argument %q\n", name, flags.Arg(0))
		return "", false, false
	}
	explicit := false
	flags.Visit(func(f *flag.Flag) { explicit = explicit || f.Name == "config" })
	expanded, err := pathutil.Expand(*path)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "webmux config:", err)
		return "", false, false
	}
	return expanded, explicit, true
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func configPath(args []string, stdout, stderr io.Writer) int {
	var asJSON *bool
	path, _, ok := configFlags("path", args, stderr, func(f *flag.FlagSet) { asJSON = f.Bool("json", false, "print JSON") })
	if !ok {
		return contract.ExitUsage
	}
	if *asJSON {
		writeJSON(stdout, contract.ConfigPathV1{V: contract.V, Path: path, Exists: exists(path)})
		return contract.ExitOK
	}
	state := "exists"
	if !exists(path) {
		state = "not found"
	}
	_, _ = fmt.Fprintf(stdout, "%s (%s)\n", path, state)
	return contract.ExitOK
}

func configShow(args []string, stdout, stderr io.Writer) int {
	var asJSON *bool
	path, explicit, ok := configFlags("show", args, stderr, func(f *flag.FlagSet) { asJSON = f.Bool("json", false, "print the config contract v1 as JSON") })
	if !ok {
		return contract.ExitUsage
	}
	c, err := client.LoadConfig(path, explicit)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "webmux config show:", describeConfigError(path, err))
		return contract.ExitError
	}
	view := configView(path, c)
	if *asJSON {
		writeJSON(stdout, view)
		return contract.ExitOK
	}
	line := func(format string, args ...any) { _, _ = fmt.Fprintf(stdout, format+"\n", args...) }
	source := path
	if !view.Exists {
		source += " (not found; defaults)"
	}
	line("Config:          %s", source)
	line("Listen:          %s", view.Listen)
	line("Token:           %s", map[bool]string{true: "configured", false: "not configured"}[view.TokenConfigured])
	line("Default cwd:     %s", view.DefaultCwd)
	line("State directory: %s", view.StateDir)
	for _, origin := range view.AllowedOrigins {
		line("Allowed origin:  %s", origin)
	}
	for _, preset := range view.Presets {
		extra := ""
		if preset.AllowArgs {
			extra = " (accepts arguments)"
		}
		line("Preset %s: %s %q%s", preset.Name, preset.Label, preset.Argv, extra)
	}
	return contract.ExitOK
}

func configView(path string, c config.Config) contract.ConfigV1 {
	view := contract.ConfigV1{
		V:               contract.V,
		Path:            path,
		Exists:          exists(path),
		Listen:          c.Listen,
		TokenConfigured: c.Token != "",
		DefaultCwd:      c.DefaultCwd,
		StateDir:        c.StateDir,
		AllowedOrigins:  append([]string{}, c.AllowedOrigins...),
		Presets:         []contract.PresetV1{},
	}
	for _, p := range c.Presets {
		view.Presets = append(view.Presets, contract.PresetV1{Name: p.Name, Label: p.Label, Argv: append([]string{}, p.Cmd...), AllowArgs: p.AllowArgs})
	}
	return view
}

// describeConfigError adds the file, line and column to TOML syntax errors.
func describeConfigError(path string, err error) string {
	var parse toml.ParseError
	if errors.As(err, &parse) {
		return fmt.Sprintf("%s:%d:%d: %s", path, parse.Position.Line, parse.Position.Col, parse.Message)
	}
	return err.Error()
}

func configValidate(args []string, stdout, stderr io.Writer) int {
	var asJSON *bool
	path, explicit, ok := configFlags("validate", args, stderr, func(f *flag.FlagSet) { asJSON = f.Bool("json", false, "print JSON") })
	if !ok {
		return contract.ExitUsage
	}
	result := contract.ValidateV1{V: contract.V, OK: true, Errors: []string{}}
	c, err := client.LoadConfig(path, explicit)
	if err == nil && c.Tunnel.Enabled && c.Token != "" && len(c.Token) < minTunnelTokenLength {
		// The same check serve applies after loading.
		err = ensureToken(&c)
	}
	if err != nil {
		result.OK = false
		result.Errors = append(result.Errors, describeConfigError(path, err))
	}
	if *asJSON {
		writeJSON(stdout, result)
	} else if result.OK {
		_, _ = fmt.Fprintf(stdout, "%s: OK\n", path)
	}
	for _, message := range result.Errors {
		_, _ = fmt.Fprintln(stderr, message)
	}
	if !result.OK {
		return contract.ExitError
	}
	return contract.ExitOK
}

var (
	tokenLine  = regexp.MustCompile(`(?m)^token = .*$`)
	listenLine = regexp.MustCompile(`(?m)^listen = .*$`)
)

// renderConfigTemplate replaces only the token and, if set, listen lines.
func renderConfigTemplate(template, token, listen string) (string, error) {
	if len(tokenLine.FindAllStringIndex(template, -1)) != 1 || len(listenLine.FindAllStringIndex(template, -1)) != 1 {
		return "", errors.New("embedded config template has no unique token or listen line")
	}
	out := tokenLine.ReplaceAllLiteralString(template, "token = "+strconv.Quote(token))
	if listen != "" {
		out = listenLine.ReplaceAllLiteralString(out, "listen = "+strconv.Quote(listen))
	}
	return out, nil
}

func configInit(args []string, stdout, stderr io.Writer) int {
	var listen *string
	var force *bool
	path, _, ok := configFlags("init", args, stderr, func(f *flag.FlagSet) {
		listen = f.String("listen", "", "listen address to write")
		force = f.Bool("force", false, "keep an existing file as .bak and replace it")
	})
	if !ok {
		return contract.ExitUsage
	}
	if *listen != "" {
		if _, _, err := net.SplitHostPort(*listen); err != nil {
			_, _ = fmt.Fprintln(stderr, "webmux config init: --listen:", err)
			return contract.ExitUsage
		}
	}
	content, err := renderConfigTemplate(deploy.ConfigExample, generateToken(), *listen)
	if err == nil {
		err = writeNewConfig(path, content, *force)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "webmux config init:", err)
		return contract.ExitError
	}
	_, _ = fmt.Fprintln(stdout, path)
	return contract.ExitOK
}

var errConfigExists = errors.New("configuration file already exists; use --force to replace it (the old file is kept as .bak)")

func writeNewConfig(path, content string, force bool) error {
	aside := ""
	if force {
		aside = ".bak"
	}
	f, err := createFresh(path, aside, 0)
	if !force && errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s: %w", path, errConfigExists)
	}
	if err != nil {
		return err
	}
	if _, err = f.WriteString(content); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return err
}
