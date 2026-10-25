package contract

// APIHealth is the body of GET /api/health. (Health is the CLI probe.)
type APIHealth struct {
	OK       bool `json:"ok"`
	Sessions int  `json:"sessions"`
}

// ServerConfig is the body of GET /api/config.
type ServerConfig struct {
	DefaultCwd string              `json:"defaultCwd"`
	Hostname   string              `json:"hostname"`
	Workspace  WorkspaceCapability `json:"workspace"`
	Tunnel     TunnelInfo          `json:"tunnel"`
}

// TunnelInfo is the read-only tunnel state in GET /api/config. It never
// carries the Cloudflare credential.
type TunnelInfo struct {
	// Enabled reports whether the runtime tunnel is starting or serving. A
	// failed tunnel stays disabled so the page does not present it as usable.
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	Named     bool   `json:"named"`
	Hostname  string `json:"hostname"`
	PublicURL string `json:"publicUrl,omitempty"`
}

// DirectoryList is the body of GET /api/fs/dirs.
type DirectoryList []string

// MkdirRequest is the body of POST /api/fs/mkdir.
type MkdirRequest struct {
	Path string `json:"path"`
}

// UploadResult is the body of a successful POST /api/sessions/{id}/upload.
type UploadResult struct {
	Path string `json:"path"`
}

// Ack is a bare acknowledgement.
type Ack struct {
	OK bool `json:"ok"`
}
