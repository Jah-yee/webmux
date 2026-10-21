// Package service manages a single WebMux service for the current user.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/rainchan/webmux/internal/config"
)

type commandRunner func(context.Context, string, []string, io.Writer, io.Writer) error

type Manager struct {
	platform, home, configHome, executable, domain string
	environment                                    map[string]string
	out, stderr                                    io.Writer
	run                                            commandRunner
	lookPath                                       func(string) (string, error)
	initConfig                                     func(string) error
}

func New(out, stderr io.Writer, initConfig func(string) error) (*Manager, error) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("user services are unsupported on %s", runtime.GOOS)
	}
	if os.Geteuid() == 0 {
		return nil, errors.New("run webmux service as your regular user, without sudo")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	// Resolve symlinks so the service does not depend on the install shell's cwd.
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, err
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(configHome) {
		configHome = filepath.Join(home, ".config")
	}
	return &Manager{
		platform: runtime.GOOS, home: home, configHome: configHome,
		executable: executable, domain: "gui/" + strconv.Itoa(os.Getuid()),
		environment: serviceEnvironment(home), out: out, stderr: stderr,
		run: runCommand, lookPath: exec.LookPath, initConfig: initConfig,
	}, nil
}

func runCommand(ctx context.Context, name string, args []string, out, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, out, stderr
	return cmd.Run()
}

func (m *Manager) definitionPath() string {
	if m.platform == "linux" {
		return filepath.Join(m.configHome, "systemd", "user", "webmux.service")
	}
	return filepath.Join(m.home, "Library", "LaunchAgents", "com.webmux.plist")
}

func (m *Manager) logPath() string {
	return filepath.Join(m.home, "Library", "Logs", "webmux.log")
}

func (m *Manager) target() string { return m.domain + "/com.webmux" }

func (m *Manager) command(ctx context.Context, name string, args ...string) error {
	if err := m.run(ctx, name, args, m.out, m.stderr); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

// Install creates and starts the service. Only the default config may be
// created implicitly. dryRun performs no writes or service-manager calls.
func (m *Manager) Install(ctx context.Context, path string, createConfig, force, dryRun bool) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	c, err := config.Load(abs, !createConfig)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if err := c.Validate(); err != nil {
		return fmt.Errorf("validate configuration: %w", err)
	}
	body, err := m.render(abs)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintf(m.out, "# Service definition: %s\n# Executable must remain at: %s\n%s", m.definitionPath(), m.executable, body)
		return nil
	}
	tool := "systemctl"
	if m.platform == "darwin" {
		tool = "launchctl"
	}
	if _, err := m.lookPath(tool); err != nil {
		return fmt.Errorf("%s is required for user services: %w", tool, err)
	}
	if _, err := os.Lstat(m.definitionPath()); err == nil && !force {
		return fmt.Errorf("service already installed at %s; use --force to replace and restart it", m.definitionPath())
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if createConfig {
		if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
			if err := m.initConfig(abs); err != nil {
				return err
			}
			fmt.Fprintf(m.out, "Created %s (0600). Read its token to sign in.\n", abs)
		} else if err != nil {
			return err
		}
	}
	if m.platform == "darwin" {
		if err := os.MkdirAll(filepath.Dir(m.logPath()), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(m.logPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	if err := writeDefinition(m.definitionPath(), body, force); err != nil {
		return err
	}
	// Keep the definition on manager failure: it is useful for diagnosis and
	// can be retried with --force after fixing the user-manager environment.
	fmt.Fprintf(m.out, "Installed %s\nExecutable: %s (keep this path available)\n", m.definitionPath(), m.executable)
	if m.platform == "linux" {
		if err := m.command(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		args := []string{"--user", "enable"}
		if !force {
			args = append(args, "--now")
		}
		if err := m.command(ctx, "systemctl", append(args, "webmux.service")...); err != nil {
			return err
		}
		if force {
			return m.command(ctx, "systemctl", "--user", "restart", "webmux.service")
		}
		return nil
	}
	if force {
		if err := m.stopLaunchd(ctx); err != nil {
			return err
		}
	}
	return m.startLaunchd(ctx)
}

func writeDefinition(path string, body []byte, force bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".webmux-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(body)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if force {
		return os.Rename(f.Name(), path)
	}
	// Link provides atomic, exclusive publication without overwriting a file
	// installed by another invocation between the existence check and write.
	return os.Link(f.Name(), path)
}

func (m *Manager) Control(ctx context.Context, action string, follow bool) error {
	if action != "logs" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	if action == "logs" {
		var err error
		if m.platform == "linux" {
			args := []string{"--user", "--unit", "webmux.service", "--lines", "100", "--no-pager"}
			if follow {
				args = append(args, "--follow")
			}
			err = m.command(ctx, "journalctl", args...)
		} else {
			args := []string{"-n", "100"}
			if follow {
				args = append(args, "-F")
			}
			err = m.command(ctx, "tail", append(args, m.logPath())...)
		}
		if ctx.Err() != nil {
			return nil // Interrupting a log stream is a normal exit.
		}
		return err
	}
	switch action {
	case "uninstall", "start", "stop", "restart", "status":
	default:
		return fmt.Errorf("unknown service action %q", action)
	}
	if _, err := os.Stat(m.definitionPath()); err != nil {
		return fmt.Errorf("service definition unavailable; run webmux service install: %w", err)
	}
	if m.platform == "linux" {
		if action != "uninstall" {
			return m.command(ctx, "systemctl", "--user", "--no-pager", action, "webmux.service")
		}
		if err := m.command(ctx, "systemctl", "--user", "disable", "--now", "webmux.service"); err != nil {
			return err
		}
	} else {
		switch action {
		case "status":
			return m.command(ctx, "launchctl", "print", m.target())
		case "start":
			return m.startLaunchd(ctx)
		case "stop", "restart", "uninstall":
			if err := m.stopLaunchd(ctx); err != nil {
				return err
			}
			if action == "restart" {
				return m.startLaunchd(ctx)
			}
			if action == "stop" {
				return nil
			}
		}
	}
	if err := os.Remove(m.definitionPath()); err != nil {
		return err
	}
	fmt.Fprintln(m.out, "Removed user service. Configuration, logs and state were kept.")
	if m.platform == "linux" {
		return m.command(ctx, "systemctl", "--user", "daemon-reload")
	}
	return nil
}

func (m *Manager) launchdLoaded(ctx context.Context) (bool, error) {
	err := m.run(ctx, "launchctl", []string{"print", m.target()}, io.Discard, io.Discard)
	if err == nil {
		return true, nil
	}
	// launchctl print returns ESRCH (113 in its bootstrap error domain) for a
	// missing service. Other failures, including an unavailable GUI domain,
	// must not be mistaken for an already-stopped service.
	var status interface{ ExitCode() int }
	if errors.As(err, &status) && status.ExitCode() == 113 {
		return false, nil
	}
	return false, fmt.Errorf("query launchd service in %s (requires a logged-in GUI session): %w", m.domain, err)
}

func (m *Manager) stopLaunchd(ctx context.Context) error {
	loaded, err := m.launchdLoaded(ctx)
	if err != nil || !loaded {
		return err
	}
	// bootout uses SIGTERM and ExitTimeOut, preserving the server's graceful
	// PTY cleanup. kickstart -k would kill the process before restarting.
	return m.command(ctx, "launchctl", "bootout", m.target())
}

func (m *Manager) startLaunchd(ctx context.Context) error {
	loaded, err := m.launchdLoaded(ctx)
	if err != nil {
		return err
	}
	if err := m.command(ctx, "launchctl", "enable", m.target()); err != nil {
		return err
	}
	if loaded {
		return m.command(ctx, "launchctl", "kickstart", m.target())
	}
	return m.command(ctx, "launchctl", "bootstrap", m.domain, m.definitionPath())
}
