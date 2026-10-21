package tray

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
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

// webmuxPath turns "~/x" into "./x" because --exec does not expand "~";
// --cd ~ makes it relative to the WSL home directory.
func webmuxPath(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		return "./" + rest
	}
	return path
}

// Command runs a webmux subcommand from the WSL home directory. Config and
// log paths keep "~/"; webmux expands them itself.
func (t Target) Command(args ...string) Invocation {
	argv := append(t.base(), "--cd", "~", "--exec", webmuxPath(t.Webmux))
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
	inv := Invocation{Argv: append(t.base(), "--cd", "~", "--exec", webmuxPath(t.Webmux), "-h")}
	return inv
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

// DecodeWSLOutput handles wsl.exe's UTF-16LE output (with or without BOM)
// and the UTF-8 output selected by WSL_UTF8=1.
func DecodeWSLOutput(data []byte) string {
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
		return decodeUTF16(data[2:])
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return string(data[3:])
	}
	if len(data) >= 2 && len(data)%2 == 0 && looksUTF16(data) {
		return decodeUTF16(data)
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return decodeUTF16(data)
}

// looksUTF16 detects ASCII-heavy UTF-16LE: most odd bytes are zero.
func looksUTF16(data []byte) bool {
	zeros := 0
	for i := 1; i < len(data); i += 2 {
		if data[i] == 0 {
			zeros++
		}
	}
	return zeros*2 >= len(data)/2
}

func decodeUTF16(data []byte) string {
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(data[2*i:])
	}
	return string(utf16.Decode(units))
}

// Distro is one installed distribution.
type Distro struct {
	Name    string
	Default bool
	Version int
}

// ParseListQuiet parses `wsl.exe --list --quiet`.
func ParseListQuiet(data []byte) []string {
	var names []string
	for line := range strings.SplitSeq(DecodeWSLOutput(data), "\n") {
		if name := strings.Trim(line, " \t\r\x00"); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// ParseListVerbose parses `wsl.exe --list --verbose`: a header, then rows
// "[*] NAME STATE VERSION" with '*' marking the default distribution.
func ParseListVerbose(data []byte) []Distro {
	var distros []Distro
	lines := strings.Split(DecodeWSLOutput(data), "\n")
	for i, line := range lines {
		line = strings.Trim(line, " \t\r\x00")
		if i == 0 || line == "" {
			continue
		}
		d := Distro{}
		if rest, ok := strings.CutPrefix(line, "*"); ok {
			d.Default = true
			line = strings.TrimSpace(rest)
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		d.Name = strings.Join(fields[:len(fields)-2], " ")
		switch fields[len(fields)-1] {
		case "1":
			d.Version = 1
		case "2":
			d.Version = 2
		}
		distros = append(distros, d)
	}
	return distros
}
