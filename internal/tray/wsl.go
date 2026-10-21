package tray

import (
	"context"
	"strings"

	"github.com/rainchan/webmux/internal/wslclient"
)

// Result is a finished short-lived command.
type Result struct {
	Stdout, Stderr []byte
	Code           int
}

// Process is a detached long-lived command.
type Process interface {
	PID() int
	// Done closes when the process exits; ExitCode is valid afterwards.
	Done() <-chan struct{}
	ExitCode() int
}

// Runner creates processes. Every invocation is an argv; nothing goes
// through a shell. Run returns an error only when the command could not run
// or ctx ended; a non-zero exit is Result.Code.
type Runner interface {
	Run(ctx context.Context, argv, env []string) (Result, error)
	// Start runs a process that outlives the tray: detached, with NUL
	// standard streams and outside any job object.
	Start(argv, env []string) (Process, error)
}

// Invocation is one command with its extra environment.
type Invocation struct {
	Argv []string
	Env  []string
}

// Target is the distribution and the webmux paths inside it.
type Target struct {
	Distro string
	Webmux string
	Config string
}

// TargetOf extracts the WSL target from settings.
func TargetOf(s Settings) Target {
	return Target{Distro: s.Distro, Webmux: s.Webmux, Config: s.Config}
}

const wslExe = "wsl.exe"

func (t Target) base() []string {
	argv := []string{wslExe}
	if t.Distro != "" {
		argv = append(argv, "-d", t.Distro)
	}
	return argv
}

// Exec runs a program with --exec, which never involves a shell.
func (t Target) Exec(program string, args ...string) Invocation {
	return Invocation{Argv: append(append(t.base(), "--exec", program), args...)}
}

// Command runs a webmux subcommand from the WSL home directory. Config and
// log paths keep "~/"; webmux expands them itself.
func (t Target) Command(args ...string) Invocation {
	argv := append(t.base(), "--cd", "~", "--exec", wslclient.WebmuxPath(t.Webmux))
	argv = append(argv, args...)
	if t.Config != "" {
		argv = append(argv, "--config", t.Config)
	}
	return Invocation{Argv: argv}
}

func (t Target) Bridge() Invocation     { return t.Command("bridge") }
func (t Target) Status() Invocation     { return t.Command("status", "--json") }
func (t Target) ForceStop() Invocation  { return t.Command("stop", "--json", "--timeout", "5s") }
func (t Target) ConfigInit() Invocation { return t.Command("config", "init") }

// Probe asks for the server flags; only versions with the local API list
// -log-file. A flag-first invocation is safe with older versions, where any
// other first argument would start a server.
func (t Target) Probe() Invocation {
	inv := Invocation{Argv: append(t.base(), "--cd", "~", "--exec", wslclient.WebmuxPath(t.Webmux), "-h")}
	return inv
}

// Version prints "webmux <version>". Call it only after Probe succeeded:
// older versions would treat the unknown argument as a server start.
func (t Target) Version() Invocation {
	return Invocation{Argv: append(t.base(), "--cd", "~", "--exec", wslclient.WebmuxPath(t.Webmux), "version")}
}

// Arch prints the distribution's machine name, e.g. x86_64.
func (t Target) Arch() Invocation { return t.Exec("uname", "-m") }

// Wslpath converts a Windows path to its WSL mount path.
func (t Target) Wslpath(windows string) Invocation {
	return t.Exec("wslpath", "-a", "-u", windows)
}

// Install copies src (a WSL path) to the webmux path as 0755, creating the
// parent directories. install(1) replaces the file instead of rewriting it,
// so a running server keeps its old executable.
func (t Target) Install(src string) Invocation {
	return Invocation{Argv: append(t.base(), "--cd", "~", "--exec", "install", "-D", "-m", "0755", src, wslclient.WebmuxPath(t.Webmux))}
}

// Serve starts the tray-managed server, recorded as launched by the tray.
func (t Target) Serve(logFile string, environ []string) Invocation {
	inv := t.Command("serve", "--log-file", logFile)
	inv.Env = []string{"WEBMUX_LAUNCHER=windows-tray", "WSLENV=" + MergeWSLENV(lookupEnv(environ, "WSLENV"), "WEBMUX_LAUNCHER")}
	return inv
}

func (t Target) NetworkingMode() Invocation { return t.Exec("wslinfo", "--networking-mode") }
func (t Target) InitComm() Invocation       { return t.Exec("cat", "/proc/1/comm") }
func (t Target) SystemdUnit() Invocation {
	return t.Exec("systemctl", "--user", "cat", "webmux.service")
}
func (t Target) Systemctl(action string) Invocation {
	return t.Exec("systemctl", "--user", action, "webmux.service")
}
func (t Target) UnitFailed() Invocation {
	return t.Exec("systemctl", "--user", "is-failed", "--quiet", "webmux.service")
}
func (t Target) Keepalive() Invocation { return t.Exec("sleep", "infinity") }
func (t Target) Journal() Invocation {
	return t.Exec("journalctl", "--user", "-u", "webmux.service", "-n", "1000", "--no-pager")
}

func ListQuiet() Invocation   { return Invocation{Argv: []string{wslExe, "--list", "--quiet"}} }
func ListVerbose() Invocation { return Invocation{Argv: []string{wslExe, "--list", "--verbose"}} }
func WSLVersion() Invocation  { return Invocation{Argv: []string{wslExe, "--version"}} }

func lookupEnv(environ []string, name string) string {
	for _, entry := range environ {
		if key, value, ok := strings.Cut(entry, "="); ok && strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

// MergeWSLENV appends name to a WSLENV list unless it is already shared.
func MergeWSLENV(existing, name string) string {
	for _, entry := range strings.Split(existing, ":") {
		key, _, _ := strings.Cut(entry, "/")
		if key == name {
			return existing
		}
	}
	if existing == "" {
		return name
	}
	return existing + ":" + name
}
