package api

import (
	"errors"
	"net/http"

	"github.com/rainchan/webmux/internal/contract"
)

// TailscaleControl is injected by cmd/. HTTP handlers never construct the
// controller or touch its settings and ownership files.
type TailscaleControl interface {
	Status() contract.TailscaleStatus
	// Refresh bypasses the detection cache for ?refresh=1.
	Refresh()
	UpdateSettings(contract.TailscaleSettingsUpdate) (contract.TailscaleStatus, error)
	ResetSettings() (contract.TailscaleStatus, error)
}

func (s *Server) tailscaleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tailscale", func(w http.ResponseWriter, r *http.Request) {
		if s.tailscaleControl(w) == nil {
			return
		}
		if r.URL.Query().Get("refresh") == "1" {
			s.Tailscale.Refresh()
		}
		reply(w, http.StatusOK, s.Tailscale.Status())
	})
	mux.HandleFunc("PUT /api/tailscale/settings", func(w http.ResponseWriter, r *http.Request) {
		if s.tailscaleControl(w) == nil {
			return
		}
		var req contract.TailscaleSettingsUpdate
		if !decode(w, r, &req) {
			return
		}
		view, err := s.Tailscale.UpdateSettings(req)
		if err != nil {
			failure(w, tailscaleStatus(err), err)
			return
		}
		status := http.StatusOK
		if view.State == contract.TailscaleStarting {
			status = http.StatusAccepted
		}
		reply(w, status, view)
	})
	mux.HandleFunc("DELETE /api/tailscale/settings", func(w http.ResponseWriter, _ *http.Request) {
		if s.tailscaleControl(w) == nil {
			return
		}
		view, err := s.Tailscale.ResetSettings()
		if err != nil {
			failure(w, tailscaleStatus(err), err)
			return
		}
		reply(w, http.StatusOK, view)
	})
}

func (s *Server) tailscaleControl(w http.ResponseWriter) TailscaleControl {
	if s.Tailscale == nil {
		failure(w, http.StatusServiceUnavailable, errors.New("tailscale status unavailable"))
	}
	return s.Tailscale
}
