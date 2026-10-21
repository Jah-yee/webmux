package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
)

func TestLocalEndpointsAreNotOnTCP(t *testing.T) {
	c := config.Config{Token: "secret"}
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := New(c, m, testWorkspace(t, m, t.TempDir()), nil)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for _, path := range []string{contract.PathToken, contract.PathStatus, contract.PathShutdown, "/local/other"} {
			r := httptest.NewRequest(method, "http://webmux.example"+path, nil)
			r.Header.Set("Authorization", "Bearer secret")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "secret") {
				t.Errorf("%s %s over TCP: %d %q", method, path, w.Code, w.Body.String())
			}
		}
	}
}

func TestLocalShutdownAndTokenPolicy(t *testing.T) {
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	inside := map[int]bool{20: true}
	for _, row := range []struct {
		name, launcher, token, method, path, body string
		pid                                       int
		want                                      int
		stopped                                   bool
	}{
		{"manual stop", "manual", "", http.MethodPost, contract.PathShutdown, "", 10, 202, true},
		{"systemd refuses", "systemd", "", http.MethodPost, contract.PathShutdown, "{}", 10, 409, false},
		{"launchd refuses", "launchd", "", http.MethodPost, contract.PathShutdown, `{"force":false}`, 10, 409, false},
		{"systemd forced", "systemd", "", http.MethodPost, contract.PathShutdown, `{"force":true}`, 10, 202, true},
		{"session cannot stop", "manual", "", http.MethodPost, contract.PathShutdown, `{"force":true}`, 20, 403, false},
		{"unknown peer cannot stop", "manual", "", http.MethodPost, contract.PathShutdown, "", 0, 403, false},
		{"bad body", "manual", "", http.MethodPost, contract.PathShutdown, `{"force":1}`, 10, 400, false},
		{"token", "manual", "secret", http.MethodGet, contract.PathToken, "", 10, 200, false},
		{"no token", "manual", "", http.MethodGet, contract.PathToken, "", 10, 404, false},
		{"session cannot read token", "manual", "secret", http.MethodGet, contract.PathToken, "", 20, 403, false},
		{"unknown local path", "manual", "", http.MethodGet, "/local/v2/status", "", 10, 404, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			stopped := false
			handler := NewLocal(config.Config{Token: row.token}, m, testWorkspace(t, m, t.TempDir()), nil, Local{
				Status:    func() contract.StatusV1 { return contract.StatusV1{V: 1} },
				Launcher:  row.launcher,
				Shutdown:  func() { stopped = true },
				InSession: func(pid int) bool { return inside[pid] },
			})
			r := httptest.NewRequest(row.method, "http://webmux"+row.path, strings.NewReader(row.body))
			if row.pid != 0 {
				r = r.WithContext(context.WithValue(r.Context(), peerKey{}, row.pid))
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != row.want || stopped != row.stopped {
				t.Fatalf("HTTP %d stopped=%v: %s", w.Code, stopped, w.Body.String())
			}
			if row.want == 200 && !strings.Contains(w.Body.String(), `"token":"secret"`) {
				t.Fatalf("token body %q", w.Body.String())
			}
			if row.want == 409 && !strings.Contains(w.Body.String(), "stop webmux") && !strings.Contains(w.Body.String(), "launchctl bootout") {
				t.Fatalf("missing manager hint: %s", w.Body.String())
			}
		})
	}
}

func TestLocalSkipsHostOriginAndToken(t *testing.T) {
	c := config.Config{Listen: "127.0.0.1:7681", Token: "secret"}
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := NewLocal(c, m, testWorkspace(t, m, t.TempDir()), nil, Local{})
	r := httptest.NewRequest(http.MethodGet, "http://webmux/api/sessions", nil)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("local /api/sessions: %d %s", w.Code, w.Body.String())
	}
}
