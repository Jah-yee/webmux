package api

import (
	"errors"
	"net/http"
)

// tailscaleRoutes serves the read-only detection and runtime status. There is
// deliberately no write route: [tailscale] is owned by TOML and a restart.
func (s *Server) tailscaleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/tailscale", func(w http.ResponseWriter, r *http.Request) {
		if s.Tailscale == nil {
			failure(w, http.StatusServiceUnavailable, errors.New("tailscale status unavailable"))
			return
		}
		if r.URL.Query().Get("refresh") == "1" && s.TailscaleRefresh != nil {
			s.TailscaleRefresh()
		}
		reply(w, http.StatusOK, s.Tailscale())
	})
}
