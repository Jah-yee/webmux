// Package contract holds the versioned JSON shapes shared by the webmux
// server, its CLI and the Windows clients. It depends only on the standard
// library so every client platform can import it.
package contract

// V is the contract version of every object in this package. Within one
// version only optional fields are added; incompatible changes bump it.
const V = 1

// Local-only endpoints. They are served on the Unix socket and never on TCP.
const (
	PathStatus   = "/local/v1/status"
	PathShutdown = "/local/v1/shutdown"
	PathToken    = "/local/v1/token"
	PathTunnel   = "/local/v1/tunnel"
)

// State is the overall result of `webmux status`.
type State string

const (
	StateRunning      State = "running"
	StateStopped      State = "stopped"
	StateUnresponsive State = "unresponsive"
	StateError        State = "error"
)

// HealthStatus classifies one health probe.
type HealthStatus string

const (
	HealthOK           HealthStatus = "ok"
	HealthUnauthorized HealthStatus = "unauthorized"
	HealthRefused      HealthStatus = "refused"
	HealthTimeout      HealthStatus = "timeout"
	HealthNotWebMux    HealthStatus = "not-webmux"
	HealthError        HealthStatus = "error"
)

// Launchers recorded by the server.
const (
	LauncherManual      = "manual"
	LauncherSystemd     = "systemd"
	LauncherLaunchd     = "launchd"
	LauncherWindowsTray = "windows-tray"
)

type Health struct {
	Status  HealthStatus `json:"status"`
	Message string       `json:"message,omitempty"`
}

// RemoteURL is one address other devices may use.
type RemoteURL struct {
	URL       string `json:"url"`
	Interface string `json:"interface"`
	// Kind is "tailscale" (100.64.0.0/10) or "lan".
	Kind string `json:"kind"`
}

// URLs never carry a token, path or query.
type URLs struct {
	Local  string      `json:"local"`
	Remote []RemoteURL `json:"remote"`
}

type ConfigRef struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Error  string `json:"error,omitempty"`
}

// StatusV1 is printed by `webmux status --json` and returned by
// GET /local/v1/status. Token is present only with --reveal-token.
type StatusV1 struct {
	V               int       `json:"v"`
	State           State     `json:"state"`
	Record          bool      `json:"record"`
	LocalAPI        bool      `json:"localApi"`
	PID             *int      `json:"pid"`
	Version         string    `json:"version"`
	Launcher        string    `json:"launcher"`
	Listen          string    `json:"listen"`
	Sessions        *int      `json:"sessions"`
	Health          Health    `json:"health"`
	URLs            URLs      `json:"urls"`
	TokenConfigured bool      `json:"tokenConfigured"`
	Drift           []string  `json:"drift"`
	Warnings        []string  `json:"warnings"`
	Config          ConfigRef `json:"config"`
	LogFile         string    `json:"logFile"`
	Socket          string    `json:"socket"`
	StartedAt       string    `json:"startedAt,omitempty"`
	Token           string    `json:"token,omitempty"`
	// Tunnel is present when the running server can start and stop its
	// Cloudflare Tunnel at runtime; older servers omit it.
	Tunnel *TunnelStatus `json:"tunnel,omitempty"`
	// Push is present when the running server supports attention push;
	// older servers omit it.
	Push *PushStatus `json:"push,omitempty"`
}

// PushStatus describes Web Push on a running server. It never carries
// subscription endpoints or keys. PublicOrigin is https://<tunnel.hostname>
// when a Named Tunnel is configured, the only origin phones can subscribe from.
type PushStatus struct {
	Enabled       bool   `json:"enabled"`
	Available     bool   `json:"available"`
	Error         string `json:"error,omitempty"`
	Subscriptions int    `json:"subscriptions"`
	PublicOrigin  string `json:"publicOrigin,omitempty"`
}

// Tunnel states.
const (
	TunnelOff      = "off"
	TunnelStarting = "starting"
	TunnelReady    = "ready"
	TunnelFailed   = "failed"
)

// TunnelStatus describes the Cloudflare Tunnel of a running server.
// PublicURL (ready only) never carries a token.
type TunnelStatus struct {
	State     string `json:"state"`
	PublicURL string `json:"publicUrl,omitempty"`
	Error     string `json:"error,omitempty"`
	Named     bool   `json:"named"`
	// Enabled is the effective process desire, including command-line
	// overrides. Nil identifies older servers: fall back to runtime state.
	Enabled *bool `json:"enabled,omitempty"`
	// SettingsAvailable explicitly advertises the web settings page/API.
	SettingsAvailable bool `json:"settingsAvailable,omitempty"`
}

// TunnelRequest is the body of POST /local/v1/tunnel.
type TunnelRequest struct {
	Enabled *bool `json:"enabled"`
}

// Stop results.
const (
	StopStopped    = "stopped"
	StopKilled     = "killed"
	StopNotRunning = "not-running"
	StopRefused    = "refused"
	StopError      = "error"
)

// StopV1 is printed by `webmux stop --json`.
type StopV1 struct {
	V       int    `json:"v"`
	Result  string `json:"result"`
	Message string `json:"message,omitempty"`
}

// ShutdownRequest is the optional body of POST /local/v1/shutdown.
type ShutdownRequest struct {
	Force bool `json:"force"`
}

// TokenResponse is the body of GET /local/v1/token.
type TokenResponse struct {
	Token string `json:"token"`
}

// ErrorResponse is the existing API error body.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ConfigV1 is printed by `webmux config show --json`; it never has a token.
type ConfigV1 struct {
	V               int      `json:"v"`
	Path            string   `json:"path"`
	Exists          bool     `json:"exists"`
	Listen          string   `json:"listen"`
	TokenConfigured bool     `json:"tokenConfigured"`
	DefaultCwd      string   `json:"defaultCwd"`
	StateDir        string   `json:"stateDir"`
	AllowedOrigins  []string `json:"allowedOrigins"`
}

// ConfigPathV1 is printed by `webmux config path --json`.
type ConfigPathV1 struct {
	V      int    `json:"v"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// ValidateV1 is printed by `webmux config validate --json`.
type ValidateV1 struct {
	V      int      `json:"v"`
	OK     bool     `json:"ok"`
	Errors []string `json:"errors"`
}

// VersionV1 is printed by `webmux version --json`. Contract is the control
// contract version this build speaks, so clients can check compatibility
// before talking to a server.
type VersionV1 struct {
	V        int    `json:"v"`
	Version  string `json:"version"`
	Contract int    `json:"contract"`
}

// Exit codes shared by every client command.
const (
	ExitOK         = 0
	ExitError      = 1
	ExitUsage      = 2
	ExitNotRunning = 3
)
