package api

import "github.com/rainchan/webmux/internal/contract"

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

// tunnelInfo describes the runtime tunnel when a controller is wired in,
// otherwise only the configured mode. Named and Hostname come from the
// configuration so the page can explain a Named Tunnel even while it is off.
func (s *Server) tunnelInfo() TunnelInfo {
	info := TunnelInfo{
		State:    contract.TunnelOff,
		Named:    s.Config.Tunnel.Named(),
		Hostname: s.Config.Tunnel.NamedHostname(),
	}
	if s.Tunnel == nil {
		return info
	}
	status := s.Tunnel()
	info.State = status.State
	if info.State == "" {
		info.State = contract.TunnelOff
	}
	info.PublicURL = status.PublicURL
	info.Enabled = info.State == contract.TunnelStarting || info.State == contract.TunnelReady
	return info
}
