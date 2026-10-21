package sessioncmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/rainchan/webmux/internal/contract"
)

// PrintStatus is the human-readable form of the status contract v1; it
// never includes the token.
func PrintStatus(w io.Writer, s contract.StatusV1) {
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
