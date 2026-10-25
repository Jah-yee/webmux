package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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
	handler := New(testDeps(t, Deps{Config: c, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}))
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for _, path := range []string{contract.PathToken, contract.PathStatus, contract.PathShutdown, contract.PathTunnel, "/local/other"} {
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
			handler := NewLocal(testDeps(t, Deps{Config: config.Config{Token: row.token}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}), Local{
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

func TestLocalTunnelPolicy(t *testing.T) {
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	inside := map[int]bool{20: true}
	for _, row := range []struct {
		name, body string
		pid        int
		refuse     bool
		want       int
		calls      []bool
	}{
		{"start", `{"enabled":true}`, 10, false, 202, []bool{true}},
		{"stop", `{"enabled":false}`, 10, false, 202, []bool{false}},
		{"weak token", `{"enabled":true}`, 10, true, 409, []bool{true}},
		{"session cannot start", `{"enabled":true}`, 20, false, 403, nil},
		{"session cannot stop", `{"enabled":false}`, 20, false, 403, nil},
		{"unknown peer", `{"enabled":true}`, 0, false, 403, nil},
		{"missing enabled", `{}`, 10, false, 400, nil},
		{"empty body", ``, 10, false, 400, nil},
		{"wrong type", `{"enabled":"yes"}`, 10, false, 400, nil},
		{"unknown field", `{"enabled":true,"x":1}`, 10, false, 400, nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			var calls []bool
			handler := NewLocal(testDeps(t, Deps{Config: config.Config{}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}), Local{
				Status:    func() contract.StatusV1 { return contract.StatusV1{V: 1} },
				InSession: func(pid int) bool { return inside[pid] },
				Tunnel: func(enabled bool) (contract.TunnelStatus, error) {
					calls = append(calls, enabled)
					if row.refuse {
						return contract.TunnelStatus{State: contract.TunnelOff}, errors.New("token too weak")
					}
					if enabled {
						return contract.TunnelStatus{State: contract.TunnelStarting}, nil
					}
					return contract.TunnelStatus{State: contract.TunnelOff}, nil
				},
			})

			r := httptest.NewRequest(http.MethodPost, "http://webmux"+contract.PathTunnel, strings.NewReader(row.body))
			if row.pid != 0 {
				r = r.WithContext(context.WithValue(r.Context(), peerKey{}, row.pid))
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != row.want || !slices.Equal(calls, row.calls) {
				t.Fatalf("HTTP %d calls %v: %s", w.Code, calls, w.Body.String())
			}
			switch row.want {
			case 202:
				var status contract.TunnelStatus
				if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
					t.Fatal(err)
				}
				if want := map[bool]string{true: contract.TunnelStarting, false: contract.TunnelOff}[row.calls[0]]; status.State != want {
					t.Fatalf("state %q, want %q", status.State, want)
				}
			case 409:
				if !strings.Contains(w.Body.String(), "token too weak") {
					t.Fatalf("409 body %q", w.Body.String())
				}
			}
		})
	}
}

func TestLocalTunnelUnroutedWithoutController(t *testing.T) {
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := NewLocal(testDeps(t, Deps{Config: config.Config{}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}), Local{})
	r := httptest.NewRequest(http.MethodPost, "http://webmux"+contract.PathTunnel, strings.NewReader(`{"enabled":true}`))
	r = r.WithContext(context.WithValue(r.Context(), peerKey{}, 10))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
}

func TestLocalConfigReportsRuntimeTunnel(t *testing.T) {
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := NewLocal(testDeps(t, Deps{Config: config.Config{}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}), Local{
		Status: func() contract.StatusV1 {
			return contract.StatusV1{Tunnel: &contract.TunnelStatus{
				State:     contract.TunnelReady,
				PublicURL: "https://abc-def.trycloudflare.com",
			}}
		},
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://webmux/api/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var response struct {
		Tunnel contract.TunnelInfo `json:"tunnel"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := contract.TunnelInfo{Enabled: true, State: contract.TunnelReady, PublicURL: "https://abc-def.trycloudflare.com"}
	if response.Tunnel != want {
		t.Fatalf("tunnel = %+v, want %+v", response.Tunnel, want)
	}
}

func TestLocalSkipsHostOriginAndToken(t *testing.T) {
	c := config.Config{Listen: "127.0.0.1:7681", Token: "secret"}
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := NewLocal(testDeps(t, Deps{Config: c, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}), Local{})
	r := httptest.NewRequest(http.MethodGet, "http://webmux/api/sessions", nil)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("local /api/sessions: %d %s", w.Code, w.Body.String())
	}
}
