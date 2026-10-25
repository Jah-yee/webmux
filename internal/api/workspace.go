package api

import (
	"net/http"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/wire"
)

func (s *Server) workspaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/repositories", func(w http.ResponseWriter, r *http.Request) {
		repositories, err := s.Workspace.List(r.Context())
		if !workspaceFailure(w, err) {
			reply(w, 200, wire.Repositories(repositories))
		}
	})
	mux.HandleFunc("POST /api/repositories", func(w http.ResponseWriter, r *http.Request) {
		var req contract.RegisterRepositoryRequest
		if !decode(w, r, &req) {
			return
		}
		repository, created, err := s.Workspace.Register(r.Context(), req.Path)
		if workspaceFailure(w, err) {
			return
		}
		status := 200
		if created {
			status = 201
		}
		reply(w, status, wire.Repository(repository))
	})
	mux.HandleFunc("DELETE /api/repositories/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !workspaceFailure(w, s.Workspace.Unregister(r.PathValue("id"))) {
			w.WriteHeader(204)
		}
	})
	mux.HandleFunc("GET /api/repositories/{id}/worktrees", func(w http.ResponseWriter, r *http.Request) {
		repository, err := s.Workspace.Refresh(r.Context(), r.PathValue("id"))
		if !workspaceFailure(w, err) {
			reply(w, 200, wire.Repository(repository))
		}
	})
	mux.HandleFunc("GET /api/repositories/{id}/branches", func(w http.ResponseWriter, r *http.Request) {
		branches, err := s.Workspace.Branches(r.Context(), r.PathValue("id"))
		if !workspaceFailure(w, err) {
			reply(w, 200, wire.Branches(branches))
		}
	})
	mux.HandleFunc("POST /api/repositories/{id}/worktrees", func(w http.ResponseWriter, r *http.Request) {
		var req contract.CreateWorktreeRequest
		if !decode(w, r, &req) {
			return
		}
		tree, err := s.Workspace.CreateWorktree(r.Context(), r.PathValue("id"), wire.CreateWorktree(req))
		if !workspaceFailure(w, err) {
			reply(w, 201, wire.Worktree(tree))
		}
	})
	mux.HandleFunc("POST /api/repositories/{id}/worktrees/{worktreeId}/removal-preview", func(w http.ResponseWriter, r *http.Request) {
		var req contract.RemovalPreviewRequest
		if !decode(w, r, &req) {
			return
		}
		preview, err := s.Workspace.PreviewRemoval(r.Context(), r.PathValue("id"), r.PathValue("worktreeId"))
		if !workspaceFailure(w, err) {
			reply(w, 200, wire.Preview(preview))
		}
	})
	mux.HandleFunc("POST /api/repositories/{id}/worktrees/{worktreeId}/remove", func(w http.ResponseWriter, r *http.Request) {
		var req contract.RemoveWorktreeRequest
		if !decode(w, r, &req) {
			return
		}
		err := s.Workspace.RemoveWorktree(r.Context(), r.PathValue("id"), r.PathValue("worktreeId"), wire.RemoveWorktree(req))
		if !workspaceFailure(w, err) {
			w.WriteHeader(204)
		}
	})
}
