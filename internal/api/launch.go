package api

import (
	"net/http"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/wire"
)

// launchRoutes serves the saved launch configs. The list carries environment
// names only; values leave the server only for a single config.
func (s *Server) launchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/launch-configs", func(w http.ResponseWriter, r *http.Request) {
		list, err := s.Launch.List()
		if err != nil {
			launchFailure(w, err)
			return
		}
		reply(w, http.StatusOK, wire.LaunchSummaries(list))
	})
	mux.HandleFunc("GET /api/launch-configs/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, err := s.Launch.Get(r.PathValue("id"))
		if err != nil {
			launchFailure(w, err)
			return
		}
		reply(w, http.StatusOK, wire.LaunchConfig(item))
	})
	mux.HandleFunc("POST /api/launch-configs", func(w http.ResponseWriter, r *http.Request) {
		var req contract.LaunchConfigInput
		if !decode(w, r, &req) {
			return
		}
		item, err := s.Launch.Create(wire.LaunchInput(req))
		if err != nil {
			launchFailure(w, err)
			return
		}
		reply(w, http.StatusCreated, wire.LaunchConfig(item))
	})
	mux.HandleFunc("PUT /api/launch-configs/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req contract.LaunchConfigInput
		if !decode(w, r, &req) {
			return
		}
		item, err := s.Launch.Update(r.PathValue("id"), wire.LaunchInput(req))
		if err != nil {
			launchFailure(w, err)
			return
		}
		reply(w, http.StatusOK, wire.LaunchConfig(item))
	})
	mux.HandleFunc("DELETE /api/launch-configs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Launch.Delete(r.PathValue("id")); err != nil {
			launchFailure(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
