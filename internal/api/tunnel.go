package api

import (
	"errors"
	"net/http"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tunnelsettings"
)

// TunnelSettingsControl is injected by cmd/. HTTP handlers never construct
// the controller or read/write its private settings file.
type TunnelSettingsControl interface {
	Settings() contract.TunnelSettings
	UpdateSettings(contract.TunnelSettingsUpdate) (contract.TunnelSettings, error)
	ResetSettings() (contract.TunnelSettings, error)
	SetEnabled(bool) (contract.TunnelStatus, error)
}

func (s *Server) tunnelRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tunnel", s.getTunnel)
	mux.HandleFunc("PUT /api/tunnel/settings", s.updateTunnel)
	mux.HandleFunc("PUT /api/tunnel/enabled", s.enableTunnel)
	mux.HandleFunc("DELETE /api/tunnel/settings", s.resetTunnel)
}

func (s *Server) tunnelControl(w http.ResponseWriter) TunnelSettingsControl {
	if s.TunnelSettings == nil {
		tunnelFailure(w, tunnelsettings.ErrUnavailable)
	}
	return s.TunnelSettings
}

func (s *Server) getTunnel(w http.ResponseWriter, _ *http.Request) {
	if control := s.tunnelControl(w); control != nil {
		reply(w, http.StatusOK, control.Settings())
	}
}

func (s *Server) updateTunnel(w http.ResponseWriter, r *http.Request) {
	control := s.tunnelControl(w)
	if control == nil {
		return
	}
	var req contract.TunnelSettingsUpdate
	if !decode(w, r, &req) {
		return
	}
	view, err := control.UpdateSettings(req)
	if err != nil {
		tunnelFailure(w, err)
		return
	}
	status := http.StatusOK
	if view.State == contract.TunnelStarting {
		status = http.StatusAccepted
	}
	reply(w, status, view)
}

func (s *Server) enableTunnel(w http.ResponseWriter, r *http.Request) {
	control := s.tunnelControl(w)
	if control == nil {
		return
	}
	var req contract.TunnelRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		failure(w, http.StatusBadRequest, errors.New("enabled is required"))
		return
	}
	if _, err := control.SetEnabled(*req.Enabled); err != nil {
		tunnelFailure(w, err)
		return
	}
	reply(w, http.StatusAccepted, control.Settings())
}

func (s *Server) resetTunnel(w http.ResponseWriter, _ *http.Request) {
	control := s.tunnelControl(w)
	if control == nil {
		return
	}
	view, err := control.ResetSettings()
	if err != nil {
		tunnelFailure(w, err)
		return
	}
	reply(w, http.StatusOK, view)
}
