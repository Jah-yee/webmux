// Package sessioncmd implements the session subcommands shared by the Linux
// CLI and the Windows console client: run, attach, ls and open. Each client
// supplies its local API client, console and working-directory rules.
package sessioncmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/rainchan/webmux/internal/attach"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
)

// Console is a raw terminal that can be restored.
type Console interface {
	attach.Terminal
	Restore() error
}

// Env is what a client provides to the commands.
type Env struct {
	// Flags registers client-specific flags (for example --config) on every
	// subcommand before parsing; Connect runs after parsing.
	Flags   func(*flag.FlagSet)
	Connect func() (*localclient.Client, error)
	Stdout  io.Writer
	Stderr  io.Writer
	// OpenConsole enters raw mode; it returns attach.ErrNotTerminal when
	// standard input or output is not a terminal.
	OpenConsole func() (Console, error)
	// OpenBrowser opens a URL in the default browser.
	OpenBrowser func(url string) error
	// ResolveCwd maps --cwd (or "" for the current directory) to a path
	// on the server.
	ResolveCwd func(ctx context.Context, cwd string) (string, error)
	// NotRunningHint tells how to start the server.
	NotRunningHint string
	// Label names this client in the browser's control indicator.
	Label string
}

// Fail prints err and returns its exit code: 3 when the server is not
// running, 1 otherwise.
func (e Env) Fail(err error) int {
	if errors.Is(err, localclient.ErrNotRunning) {
		_, _ = fmt.Fprintln(e.Stderr, "webmux: WebMux is not running;", e.NotRunningHint)
		return contract.ExitNotRunning
	}
	_, _ = fmt.Fprintln(e.Stderr, "webmux:", err)
	return contract.ExitError
}

func (e Env) usage(flags *flag.FlagSet, format string, args ...any) int {
	_, _ = fmt.Fprintf(e.Stderr, "webmux %s: %s\n", flags.Name(), fmt.Sprintf(format, args...))
	flags.Usage()
	return contract.ExitUsage
}

func (e Env) flags(name, synopsis string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(e.Stderr)
	flags.Usage = func() {
		_, _ = fmt.Fprintf(e.Stderr, "usage: webmux %s %s\n", name, synopsis)
		flags.PrintDefaults()
	}
	if e.Flags != nil {
		e.Flags(flags)
	}
	return flags
}

func (e Env) connect() (*localclient.Client, int) {
	client, err := e.Connect()
	if err != nil {
		return nil, e.Fail(err)
	}
	return client, contract.ExitOK
}

// envFlag collects repeated --env NAME=VALUE.
type envFlag map[string]string

func (f envFlag) String() string { return "" }
func (f envFlag) Set(value string) error {
	name, v, ok := strings.Cut(value, "=")
	if !ok || name == "" {
		return fmt.Errorf("--env expects NAME=VALUE, got %q", value)
	}
	f[name] = v
	return nil
}

// Run implements `run [--name] [--cwd] [--env K=V]... [--detach] -- argv...`.
func Run(ctx context.Context, e Env, args []string) int {
	flags := e.flags("run", "[--name NAME] [--cwd DIR] [--env NAME=VALUE]... [--detach] -- COMMAND [ARG]...")
	name := flags.String("name", "", "session name")
	cwd := flags.String("cwd", "", "working directory on the server (default: the current directory)")
	detach := flags.Bool("detach", false, "create the session and print its ID and link without attaching")
	env := envFlag{}
	flags.Var(env, "env", "set NAME=VALUE in the session (repeatable); nothing else is copied")
	if err := flags.Parse(args); err != nil {
		return contract.ExitUsage
	}
	argv := flags.Args()
	if len(argv) == 0 || argv[0] == "" {
		return e.usage(flags, "a command is required after --")
	}
	client, code := e.connect()
	if client == nil {
		return code
	}
	defer client.Close()
	dir, err := e.ResolveCwd(ctx, *cwd)
	if err != nil {
		_, _ = fmt.Fprintln(e.Stderr, "webmux run:", err)
		return contract.ExitUsage
	}
	request := contract.CreateSessionRequest{Preset: "custom", Cmd: argv, Cwd: dir, Name: *name}
	if len(env) > 0 {
		request.Env = map[string]string(env)
	}
	// An attached run needs a terminal: check before creating anything.
	var console Console
	if !*detach {
		if console, code = e.openConsole(); console == nil {
			return code
		}
	}
	var created contract.SessionInfo
	if err := client.Do(ctx, http.MethodPost, "/api/sessions", request, &created); err != nil {
		if console != nil {
			_ = console.Restore()
		}
		return e.Fail(err)
	}
	if *detach {
		link, err := link(ctx, client, created.ID)
		if err != nil {
			return e.Fail(err)
		}
		_, _ = fmt.Fprintf(e.Stdout, "%s\t%s\n", created.ID, link)
		return contract.ExitOK
	}
	// Tell the user where to find the session (for example on a phone)
	// before the session's output starts; stdout stays the session's. The
	// terminal is already raw, so the line ends with CR LF.
	if link, err := link(ctx, client, created.ID); err == nil {
		_, _ = fmt.Fprintf(e.Stderr, "webmux: session %s · %s · Ctrl+] to detach\r\n", created.ID, link)
	} else {
		_, _ = fmt.Fprintf(e.Stderr, "webmux: session %s · Ctrl+] to detach\r\n", created.ID)
	}
	return e.attach(ctx, client, created.ID, false, console)
}

// Attach implements `attach <id> [--observe]`.
func Attach(ctx context.Context, e Env, args []string) int {
	flags := e.flags("attach", "[--observe] SESSION")
	observe := flags.Bool("observe", false, "watch without taking control, sending input or resizing")
	if err := flags.Parse(args); err != nil {
		return contract.ExitUsage
	}
	if flags.NArg() != 1 {
		return e.usage(flags, "exactly one session ID is required")
	}
	client, code := e.connect()
	if client == nil {
		return code
	}
	defer client.Close()
	console, code := e.openConsole()
	if console == nil {
		return code
	}
	return e.attach(ctx, client, flags.Arg(0), *observe, console)
}

// openConsole enters raw mode; a missing terminal is a usage error.
func (e Env) openConsole() (Console, int) {
	console, err := e.OpenConsole()
	if errors.Is(err, attach.ErrNotTerminal) {
		_, _ = fmt.Fprintln(e.Stderr, "webmux:", err)
		return nil, contract.ExitUsage
	}
	if err != nil {
		return nil, e.Fail(err)
	}
	return console, contract.ExitOK
}

// attach runs until detach or exit and restores console before returning.
func (e Env) attach(ctx context.Context, client *localclient.Client, id string, observe bool, console Console) int {
	result, err := attach.Run(ctx, client, id, console, attach.Options{Observe: observe, Label: e.Label})
	_ = console.Restore()
	switch {
	case err != nil:
		return e.Fail(err)
	case result.Detached:
		_, _ = fmt.Fprintf(e.Stderr, "\r\n[detached from %s; it keeps running]\n", id)
		return contract.ExitOK
	}
	return result.ExitCode
}

// List implements `ls [--json]`.
func List(ctx context.Context, e Env, args []string) int {
	flags := e.flags("ls", "[--json]")
	asJSON := flags.Bool("json", false, "print the server's session list as JSON")
	if err := flags.Parse(args); err != nil {
		return contract.ExitUsage
	}
	if flags.NArg() > 0 {
		return e.usage(flags, "unexpected argument %q", flags.Arg(0))
	}
	client, code := e.connect()
	if client == nil {
		return code
	}
	defer client.Close()
	var raw json.RawMessage
	if err := client.Do(ctx, http.MethodGet, "/api/sessions", nil, &raw); err != nil {
		return e.Fail(err)
	}
	if *asJSON {
		_, _ = fmt.Fprintf(e.Stdout, "%s\n", raw)
		return contract.ExitOK
	}
	var list []contract.SessionInfo
	if err := json.Unmarshal(raw, &list); err != nil {
		return e.Fail(err)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	w := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tSTATUS\tCOMMAND\tCWD")
	for _, s := range list {
		status := s.Status
		if s.ExitCode != nil {
			status = fmt.Sprintf("%s (%d)", s.Status, *s.ExitCode)
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Title, status, strings.Join(s.Cmd, " "), s.Cwd)
	}
	_ = w.Flush()
	return contract.ExitOK
}

// Open implements `open <id> [--browser]`.
func Open(ctx context.Context, e Env, args []string) int {
	flags := e.flags("open", "[--browser] SESSION")
	browser := flags.Bool("browser", false, "also open the link in the default browser")
	if err := flags.Parse(args); err != nil {
		return contract.ExitUsage
	}
	if flags.NArg() != 1 {
		return e.usage(flags, "exactly one session ID is required")
	}
	client, code := e.connect()
	if client == nil {
		return code
	}
	defer client.Close()
	id := flags.Arg(0)
	if err := client.Do(ctx, http.MethodGet, "/api/sessions/"+url.PathEscape(id), nil, nil); err != nil {
		return e.Fail(err)
	}
	link, err := link(ctx, client, id)
	if err != nil {
		return e.Fail(err)
	}
	_, _ = fmt.Fprintln(e.Stdout, link)
	if *browser {
		if err := e.OpenBrowser(link); err != nil {
			return e.Fail(fmt.Errorf("open browser: %w", err))
		}
	}
	return contract.ExitOK
}

// link is the session's token-free local link.
func link(ctx context.Context, client *localclient.Client, id string) (string, error) {
	status, err := client.Status(ctx)
	if err != nil {
		return "", err
	}
	if status.URLs.Local == "" {
		return "", errors.New("the server reported no local link")
	}
	return strings.TrimSuffix(status.URLs.Local, "/") + "/s/" + url.PathEscape(id), nil
}
