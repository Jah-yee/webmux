package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/rainchan/webmux/internal/launch"
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
		reply(w, http.StatusOK, list)
	})
	mux.HandleFunc("GET /api/launch-configs/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, err := s.Launch.Get(r.PathValue("id"))
		if err != nil {
			launchFailure(w, err)
			return
		}
		reply(w, http.StatusOK, item)
	})
	mux.HandleFunc("POST /api/launch-configs", func(w http.ResponseWriter, r *http.Request) {
		var req launch.Input
		if !decode(w, r, &req) {
			return
		}
		item, err := s.Launch.Create(req)
		if err != nil {
			launchFailure(w, err)
			return
		}
		reply(w, http.StatusCreated, item)
	})
	mux.HandleFunc("PUT /api/launch-configs/{id}", func(w http.ResponseWriter, r *http.Request) {
		var req launch.Input
		if !decode(w, r, &req) {
			return
		}
		item, err := s.Launch.Update(r.PathValue("id"), req)
		if err != nil {
			launchFailure(w, err)
			return
		}
		reply(w, http.StatusOK, item)
	})
	mux.HandleFunc("DELETE /api/launch-configs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Launch.Delete(r.PathValue("id")); err != nil {
			launchFailure(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// launchStatus maps launch store errors to HTTP statuses; anything else is a
// failed save.
func launchStatus(err error) int {
	switch {
	case errors.Is(err, launch.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, launch.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, launch.ErrDuplicate):
		return http.StatusConflict
	case errors.Is(err, launch.ErrUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// launchFailure answers err. Store errors name fields and paths, never
// environment values.
func launchFailure(w http.ResponseWriter, err error) {
	status := launchStatus(err)
	if status == http.StatusInternalServerError {
		slog.Error("launch config save failed", "error", err)
	}
	failure(w, status, err)
}
