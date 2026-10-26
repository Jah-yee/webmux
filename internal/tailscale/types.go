// Package tailscale detects a Tailscale node and applies or removes the exact
// Serve/Funnel entry point owned by WebMux. It never runs through a shell and
// never resets another application's entries.
package tailscale

import (
	"context"
	"time"
)

// Detected providers. A WSL-native node uses the configured linux provider.
const (
	ProviderLinux     = "linux"
	ProviderWSLNative = "wsl-native"
	ProviderWindows   = "windows"
	ProviderNone      = "none"
)

// Backend states reported by tailscale status --json, plus detection states.
const (
	BackendRunning     = "running"
	BackendStopped     = "stopped"
	BackendNeedsLogin  = "needs-login"
	BackendUnavailable = "unavailable"
	BackendUnknown     = "unknown"
)

// FunnelAvailability is the node attribute detection result.
const (
	FunnelAvailable   = "available"
	FunnelUnavailable = "unavailable"
	FunnelUnknown     = "unknown"
)

// Status is the read-only detection result. It is credential-free and omits
// every other entry's proxy target.
type Status struct {
	Provider           string
	ConfiguredProvider string
	BackendState       string
	DNSName            string
	HTTPSReady         bool
	FunnelAvailable    string
	Entries            []Entry
	WSL                *WSLStatus
	Error              string
}

// Entry is one existing Serve/Funnel entry.
type Entry struct {
	Host   string
	Port   int
	Public bool
}

// WSLStatus is the WSL-specific detection result.
type WSLStatus struct {
	NetworkingMode  string
	WindowsDetected bool
	WindowsRunning  bool
	WindowsDNSName  string
}

// Options configure CLI discovery and detection without importing config.
type Options struct {
	// Command is an explicit CLI path; empty discovers one.
	Command string
	// Provider is auto, linux or windows.
	Provider string
	// Timeout bounds each CLI call.
	Timeout time.Duration
	// CacheTTL is how long detection is reused; Refresh invalidates it.
	CacheTTL time.Duration
	// LookPath finds an executable on PATH.
	LookPath func(string) (string, error)
	// IsWSL reports whether the process runs inside WSL.
	IsWSL func() bool
	// NetworkingMode returns mirrored, nat or unknown.
	NetworkingMode func(context.Context) string
	// Now supplies the cache clock.
	Now func() time.Time
}

// HandlerInfo describes the single handler at one Serve entry.
type HandlerInfo struct {
	Proxy    string
	Handlers int
	Public   bool
}

// ServeStatus is the documented subset of tailscale serve status --json.
type ServeStatus struct {
	Entries []Entry
	web     map[string]HandlerInfo
}

// Lookup returns the handler at host:port.
func (s ServeStatus) Lookup(host string, port int) (HandlerInfo, bool) {
	info, ok := s.web[authority(host, port)]
	return info, ok
}

func authority(host string, port int) string {
	return joinHostPort(host, port)
}
