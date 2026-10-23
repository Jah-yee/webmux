package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/hub"
	"github.com/rainchan/webmux/internal/push"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Server struct {
	Config    config.Config
	Manager   *session.Manager
	Workspace *workspace.Service
	Code      *codeview.Service
	Public    *PublicOrigin
	// Push is nil when [push] enabled = false.
	Push *push.Service
	// Tunnel reports the runtime tunnel state; nil leaves GET /api/config
	// describing only the configured mode.
	Tunnel func() contract.TunnelStatus
	// local marks the Unix-socket handler, which serves no browser page.
	local bool
}

// New builds the TCP handler. public is the runtime tunnel origin; nil means
// no tunnel. pushes is nil when push is disabled. tunnel reports the runtime
// tunnel state; nil describes the configured mode only. Local-only endpoints
// are not found here.
func New(c config.Config, m *session.Manager, workspaces *workspace.Service, public *PublicOrigin, pushes *push.Service, tunnel func() contract.TunnelStatus, codes ...*codeview.Service) http.Handler {
	s := newServer(c, m, workspaces, public, pushes, tunnel, codes)
	mux := s.routes()
	return s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/local/") {
			http.NotFound(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

func newServer(c config.Config, m *session.Manager, workspaces *workspace.Service, public *PublicOrigin, pushes *push.Service, tunnel func() contract.TunnelStatus, codes []*codeview.Service) *Server {
	s := &Server{Config: c, Manager: m, Workspace: workspaces, Public: public, Push: pushes, Tunnel: tunnel}
	if len(codes) > 0 {
		s.Code = codes[0]
	} else {
		s.Code = codeview.New(m)
	}
	return s
}

// routes registers the /api, /ws and static routes shared by both listeners.
func (s *Server) routes() *http.ServeMux {
	c, m, workspaces := s.Config, s.Manager, s.Workspace
	mux := http.NewServeMux()
	s.codeRoutes(mux)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"ok": true, "sessions": len(m.List())})
	})
	mux.HandleFunc("GET /api/presets", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, c.Presets) })
	mux.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		hostname, _ := os.Hostname()
		reply(w, 200, map[string]any{"defaultCwd": c.DefaultCwd, "hostname": hostname, "workspace": workspaces.Capability(), "tunnel": s.tunnelInfo()})
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) { reply(w, 200, m.List()) })
	mux.HandleFunc("POST /api/sessions", s.create)
	mux.HandleFunc("GET /api/sessions/{id}", s.get)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.delete)
	mux.HandleFunc("GET /api/sessions/{id}/buffer", s.buffer)
	mux.HandleFunc("POST /api/sessions/{id}/upload", s.upload)
	mux.HandleFunc("GET /api/fs/dirs", s.dirs)
	mux.HandleFunc("POST /api/fs/mkdir", s.mkdir)
	s.workspaceRoutes(mux)
	s.pushRoutes(mux)
	mux.HandleFunc("GET /ws/sessions", s.listWS)
	mux.HandleFunc("GET /ws/s/{id}", s.terminalWS)
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("GET /ws/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("GET /", s.static)
	return mux
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, status int, err error) {
	reply(w, status, map[string]string{"error": err.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		failure(w, 400, err)
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		failure(w, 400, errors.New("expected one JSON object"))
		return false
	}
	return true
}

// baseHeaders sets the response headers and panic recovery both listeners
// share; it returns the deferred recovery.
func baseHeaders(w http.ResponseWriter) func() {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	return func() {
		if p := recover(); p != nil {
			slog.Error("request panic", "panic", p)
			http.Error(w, "internal error", 500)
		}
	}
}
func (s *Server) middleware(next http.Handler) http.Handler {
	hostAllowed := hostCheck(s.Config)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer baseHeaders(w)()
		if !hostAllowed(r.Host) && !s.Public.hostAllowed(authorityHostname(r.Host)) {
			failure(w, http.StatusForbidden, errors.New("host not allowed"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
			if !s.authorized(r) {
				failure(w, 401, errors.New("authentication required"))
				return
			}
			if !s.originAllowed(r) {
				failure(w, 403, errors.New("origin not allowed"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) authorized(r *http.Request) bool {
	if s.Config.Token == "" {
		return true
	}
	token := ""
	scheme, credential, found := strings.Cut(r.Header.Get("Authorization"), " ")
	if found && strings.EqualFold(scheme, "Bearer") {
		token = credential
	}
	if token == "" {
		if cookie, err := r.Cookie("webmux_token"); err == nil {
			token = cookie.Value
		}
	}
	if token == "" && strings.HasPrefix(r.URL.Path, "/ws/") {
		token = protocolToken(r)
	}
	want := sha256.Sum256([]byte(s.Config.Token))
	got := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

// wsProtocol is the subprotocol the server selects. Browsers cannot set
// Authorization on a WebSocket handshake, so the frontend also offers
// wsTokenProtocol+base64url(token). That keeps the token out of request URLs
// and out of cookies, which browsers share across every port of a host; the
// server never selects or echoes the token protocol.
const (
	wsProtocol      = "webmux"
	wsTokenProtocol = "webmux.token."
)

func protocolToken(r *http.Request) string {
	for _, header := range r.Header.Values("Sec-WebSocket-Protocol") {
		for offered := range strings.SplitSeq(header, ",") {
			encoded, ok := strings.CutPrefix(strings.TrimSpace(offered), wsTokenProtocol)
			if !ok {
				continue
			}
			if token, err := base64.RawURLEncoding.DecodeString(encoded); err == nil {
				return string(token)
			}
		}
	}
	return ""
}
func (s *Server) originAllowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Host == "" || u.Path != "" || strings.ContainsAny(origin, "?#") || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Tunnel traffic arrives over plain HTTP, so the scheme inferred from
	// r.TLS cannot vouch for the public hostname; only its https origin can.
	if s.Public.hostAllowed(authorityHostname(r.Host)) {
		if s.Public.originAllowed(origin, r.Host) {
			return true
		}
	} else if u.Host == r.Host && u.Scheme == scheme {
		return true
	}
	for _, allowed := range s.Config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cwd       string               `json:"cwd"`
		Preset    string               `json:"preset"`
		Cmd       []string             `json:"cmd"`
		Args      []string             `json:"args"`
		Name      string               `json:"name"`
		Workspace *workspace.Reference `json:"workspace"`
		Env       map[string]string    `json:"env"`
	}
	if !decode(w, r, &req) {
		return
	}
	// Rejected before any filesystem or Git work.
	if err := session.ValidateEnv(req.Env); err != nil {
		failure(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.Workspace.CreateSession(r.Context(), req.Cwd, req.Preset, req.Cmd, req.Args, req.Name, req.Workspace, req.Env)
	if err != nil {
		failure(w, workspace.Status(err), err)
		return
	}
	reply(w, 201, item.Info())
}
func (s *Server) find(w http.ResponseWriter, r *http.Request) *session.Session {
	item, err := s.Manager.Get(r.PathValue("id"))
	if err != nil {
		failure(w, 404, err)
		return nil
	}
	return item
}
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	if item := s.find(w, r); item != nil {
		reply(w, 200, item.Info())
	}
}
func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	var err error
	if r.URL.Query().Get("remove") == "1" {
		err = s.Manager.Remove(r.PathValue("id"))
	} else {
		err = s.Manager.Close(r.PathValue("id"))
	}
	if err != nil {
		status := 404
		if errors.Is(err, session.ErrRunning) {
			status = 409
		}
		failure(w, status, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) buffer(w http.ResponseWriter, r *http.Request) {
	if item := s.find(w, r); item != nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(item.Buffer())
	}
}
func (s *Server) dirs(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	if prefix == "" {
		prefix = s.Config.DefaultCwd + string(os.PathSeparator)
	}
	expanded, err := session.ExpandPath(prefix)
	if err != nil {
		failure(w, 400, err)
		return
	}
	dir, base := filepath.Split(expanded)
	if strings.HasSuffix(prefix, "/") || prefix == "~" {
		dir = expanded
		base = ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		failure(w, 400, err)
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i].Name(), entries[j].Name()
		leftHidden, rightHidden := strings.HasPrefix(left, "."), strings.HasPrefix(right, ".")
		if leftHidden != rightHidden {
			return !leftHidden
		}
		return left < right
	})
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), base) {
			continue
		}
		full := filepath.Join(dir, entry.Name())
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			stat, err := os.Stat(full)
			if err != nil {
				continue
			}
			isDir = stat.IsDir()
		}
		if isDir {
			out = append(out, full)
		}
	}
	reply(w, 200, out)
}
func (s *Server) mkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Path == "" {
		failure(w, 400, errors.New("path required"))
		return
	}
	p, err := session.ExpandPath(req.Path)
	if err == nil {
		err = os.MkdirAll(p, 0755)
	}
	if err != nil {
		failure(w, 400, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) accept(w http.ResponseWriter, r *http.Request) *websocket.Conn {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{wsProtocol}})
	if err != nil {
		return nil
	}
	return c
}
func (s *Server) listWS(w http.ResponseWriter, r *http.Request) {
	if c := s.accept(w, r); c != nil {
		hub.List(r.Context(), s.Manager, c)
	}
}
func (s *Server) terminalWS(w http.ResponseWriter, r *http.Request) {
	item, err := s.Manager.Get(r.PathValue("id"))
	c := s.accept(w, r)
	if c == nil {
		return
	}
	if err != nil {
		_ = c.Close(4404, "session not found")
		return
	}
	hub.Attach(r.Context(), item, c, hub.AttachOptions(r.URL.Query()))
}
