package contract

// TailscaleStatus is the credential-free view returned by GET /api/tailscale.
// It never contains the WebMux token, another entry's proxy target, or raw
// CLI output.
type TailscaleStatus struct {
	// Provider is the detected runtime: linux, wsl-native, windows or none.
	Provider string `json:"provider"`
	// ConfiguredProvider is the configured value: auto, linux or windows.
	ConfiguredProvider string `json:"configuredProvider"`
	// BackendState is running, stopped, needs-login, unavailable or unknown.
	BackendState string `json:"backendState"`
	// DNSName is the node's MagicDNS name without the trailing dot.
	DNSName string `json:"dnsName,omitempty"`
	// HTTPSReady reports whether CertDomains contains the node DNS name.
	HTTPSReady bool `json:"httpsReady"`
	// FunnelAvailable is available, unavailable or unknown.
	FunnelAvailable string `json:"funnelAvailable"`
	// Mode is off, serve or funnel.
	Mode string `json:"mode"`
	// Port is the configured port, 0 when automatic.
	Port int `json:"port"`
	// State is off, starting, ready or failed.
	State string `json:"state"`
	// PublicURL is the ready HTTPS entry point.
	PublicURL string `json:"publicUrl,omitempty"`
	// BlockedReason explains why the configured mode cannot apply.
	BlockedReason string `json:"blockedReason,omitempty"`
	// Error is the last runtime failure.
	Error string `json:"error,omitempty"`
	// Entries lists existing Serve/Funnel entries without proxy targets.
	Entries []TailscaleEntry `json:"entries"`
	// WSL carries WSL-specific detection details; nil on other platforms.
	WSL *TailscaleWSL `json:"wsl,omitempty"`
}

// TailscaleEntry is one existing Serve/Funnel entry. It deliberately omits the
// proxy target and any other application's local path.
type TailscaleEntry struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Public bool   `json:"public"`
}

// TailscaleWSL is the WSL-specific detection result.
type TailscaleWSL struct {
	// NetworkingMode is mirrored, nat or unknown.
	NetworkingMode string `json:"networkingMode"`
	// WindowsDetected reports whether a Windows tailscale.exe was usable.
	WindowsDetected bool `json:"windowsDetected"`
	// WindowsRunning reports whether the Windows node reports Running.
	WindowsRunning bool `json:"windowsRunning"`
	// WindowsDNSName is the Windows node's MagicDNS name without the dot.
	WindowsDNSName string `json:"windowsDnsName,omitempty"`
}
