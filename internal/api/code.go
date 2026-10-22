package api

import (
	"context"
	"github.com/rainchan/webmux/internal/codeview"
	"net/http"
	"time"
)

func (s *Server) codeRoutes(mux *http.ServeMux) {
	handle := func(pattern string, fn func(context.Context, *http.Request) (any, error)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			value, err := fn(ctx, r)
			if err != nil {
				failure(w, codeview.Status(err), err)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			reply(w, 200, value)
		})
	}
	base := " /api/sessions/{id}/code"
	handle("GET"+base+"/context", func(ctx context.Context, r *http.Request) (any, error) { return s.Code.Context(ctx, r.PathValue("id")) })
	handle("GET"+base+"/tree", func(ctx context.Context, r *http.Request) (any, error) {
		q := r.URL.Query()
		return s.Code.Tree(ctx, r.PathValue("id"), q.Get("contextId"), q.Get("nodeId"), q.Get("includeIgnored") == "true", q.Get("cursor"))
	})
	handle("GET"+base+"/files", func(ctx context.Context, r *http.Request) (any, error) {
		q := r.URL.Query()
		return s.Code.Search(ctx, r.PathValue("id"), q.Get("contextId"), q.Get("q"), q.Get("includeIgnored") == "true")
	})
	handle("GET"+base+"/locate", func(ctx context.Context, r *http.Request) (any, error) {
		q := r.URL.Query()
		return s.Code.Locate(ctx, r.PathValue("id"), q.Get("contextId"), q.Get("path"))
	})
	handle("GET"+base+"/file", func(ctx context.Context, r *http.Request) (any, error) {
		q := r.URL.Query()
		source := q.Get("source")
		if source == "" {
			source = "worktree"
		}
		return s.Code.FileSource(ctx, r.PathValue("id"), q.Get("contextId"), q.Get("nodeId"), source)
	})
	handle("GET"+base+"/changes", func(ctx context.Context, r *http.Request) (any, error) {
		q := r.URL.Query()
		return s.Code.Changes(ctx, r.PathValue("id"), q.Get("contextId"), codeview.ChangeQuery{Cursor: q.Get("cursor"), Q: q.Get("q"), NodeID: q.Get("nodeId"), Group: q.Get("group")})
	})
	handle("GET"+base+"/diff", func(ctx context.Context, r *http.Request) (any, error) {
		return s.Code.Diff(ctx, r.PathValue("id"), r.URL.Query().Get("contextId"), r.URL.Query().Get("changeId"))
	})
	handle("GET"+base+"/snapshots/{snapshotId}/file", func(ctx context.Context, r *http.Request) (any, error) {
		return s.Code.SnapshotFile(ctx, r.PathValue("id"), r.PathValue("snapshotId"), r.URL.Query().Get("side"))
	})
	mux.HandleFunc("POST"+base+"/check", func(w http.ResponseWriter, r *http.Request) {
		var req codeview.CheckRequest
		if !decode(w, r, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		v, e := s.Code.Check(ctx, r.PathValue("id"), req)
		if e != nil {
			failure(w, codeview.Status(e), e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		reply(w, 200, v)
	})
}
