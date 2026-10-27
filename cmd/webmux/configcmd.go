package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/BurntSushi/toml"
	"github.com/rainchan/webmux/deploy"
	"github.com/rainchan/webmux/internal/client"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/pathutil"
	"github.com/rainchan/webmux/internal/statefile"
	"github.com/rainchan/webmux/internal/wsl"
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
	var asJSON, live *bool
	path, explicit, ok := configFlags("show", args, stderr, func(f *flag.FlagSet) {
		asJSON = f.Bool("json", false, "print the safe configuration view as JSON")
		live = f.Bool("live", false, "read the running process instead of disk")
	})
	if !ok {
		return contract.ExitUsage
	}
	var view contract.Configuration
	var err error
	if *live {
		view, err = liveConfiguration(path, explicit)
	} else {
		var c config.Config
		c, err = client.LoadConfig(path, explicit)
		if err == nil {
			view = configuredView(path, c)
		} else {
			err = fmt.Errorf("configuration unavailable: %s", path)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "webmux config show:", err)
		return contract.ExitError
	}
	if *asJSON {
		writeJSON(stdout, view)
		return contract.ExitOK
	}
	line := func(format string, args ...any) { _, _ = fmt.Fprintf(stdout, format+"\n", args...) }
	source := path
	if !view.Exists {
		source += " (not found; defaults)"
	}
	line("View:            %s", view.View)
	line("Config:          %s", source)
	line("Listen:          %s", view.Listen)
	line("Token:           %s", map[bool]string{true: "configured", false: "not configured"}[view.TokenConfigured])
	line("Default cwd:     %s", view.DefaultCwd)
	line("State directory: %s", view.StateDir)
	for _, origin := range view.AllowedOrigins {
		line("Allowed origin:  %s", origin)
	}
	keys := make([]string, 0, len(view.Fields))
	for key := range view.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		f := view.Fields[key]
		line("%s: %v [%s]", key, f.Value, f.Source)
	}
	for _, store := range view.Stores {
		line("Store %s: %s (%s; lock=%s)", store.ID, store.Status, store.Path, store.Lock)
	}
	return contract.ExitOK
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
	if err == nil && (c.Tunnel.Enabled || c.Tailscale.Enabled()) && c.Token != "" && len(c.Token) < minTunnelTokenLength {
		// The same check serve applies after loading.
		err = ensureToken(&c)
	}
	if err == nil && c.Tailscale.Provider == "windows" && !wsl.Detected() {
		err = errors.New(`tailscale.provider "windows" is only valid inside WSL; use "linux" or "auto"`)
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

// newConfigContent renders the shipped template with a fresh random token.
// Every entry point that creates a configuration uses it so the contents and
// permissions do not drift from `webmux config init`.
func newConfigContent(listen string) (string, error) {
	return renderConfigTemplate(deploy.ConfigExample, generateToken(), listen)
}

// ensureDefaultConfig creates path from the embedded template when it does not
// exist. It returns created=true only when this call published the file.
// Concurrent callers race on an atomic hard link: the loser sees the complete
// winner and reuses its token. An existing file is never opened for writing.
func ensureDefaultConfig(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	content, err := newConfigContent("")
	if err != nil {
		return false, err
	}
	err = publishConfig(path, content, false)
	if errors.Is(err, os.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
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
	content, err := newConfigContent(*listen)
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

// publishConfig stages content next to path and publishes it atomically.
// Without force an existing target is never replaced; with force the existing
// file is first kept as .bak. Staging means a concurrent reader sees either
// no file or the complete new one, never a partially written token.
func publishConfig(path, content string, force bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if force {
		if _, err := os.Lstat(path); err == nil {
			if err := os.Rename(path, path+".bak"); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return statefile.Write(path, []byte(content), statefile.Options{
		TempPattern: ".config-*",
		Exclusive:   !force,
	})
}

func writeNewConfig(path, content string, force bool) error {
	err := publishConfig(path, content, force)
	if !force && errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s: %w", path, errConfigExists)
	}
	return err
}
