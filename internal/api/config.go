package api

import "github.com/rainchan/webmux/internal/contract"

// tunnelInfo describes the runtime tunnel when a controller is wired in,
// otherwise only the configured mode. Named and Hostname come from the
// configuration so the page can explain a Named Tunnel even while it is off.
func (s *Server) tunnelInfo() contract.TunnelInfo {
	info := contract.TunnelInfo{
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
