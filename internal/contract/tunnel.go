package contract

// TunnelSettings is the credential-free view returned by /api/tunnel.
// Enabled describes current process intent, not readiness or disk state.
type TunnelSettings struct {
	Enabled             bool   `json:"enabled"`
	Mode                string `json:"mode"`
	Hostname            string `json:"hostname"`
	TokenConfigured     bool   `json:"tokenConfigured"`
	Source              string `json:"source"`
	State               string `json:"state"`
	PublicURL           string `json:"publicUrl,omitempty"`
	Error               string `json:"error,omitempty"`
	CanEnable           bool   `json:"canEnable"`
	EnableBlockedReason string `json:"enableBlockedReason,omitempty"`
	StorageAvailable    bool   `json:"storageAvailable"`
	StorageError        string `json:"storageError,omitempty"`
}

// TunnelSettingsUpdate replaces the mode and hostname. An omitted token
// preserves the existing credential; an empty named token is invalid.
type TunnelSettingsUpdate struct {
	Mode     string  `json:"mode"`
	Hostname string  `json:"hostname"`
	Token    *string `json:"token,omitempty"`
}

// TunnelSettingsError distinguishes an emergency stop whose persisted
// desire could not be updated. The current process is nevertheless off.
type TunnelSettingsError struct {
	Error        string `json:"error"`
	NotPersisted bool   `json:"notPersisted,omitempty"`
}
