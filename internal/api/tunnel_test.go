package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/tunnelsettings"
)

// tunnelControlFixture exercises the transport against an injected boundary.
// Persistence and process sequencing are tested with the real cmd controller.
type tunnelControlFixture struct {
	mu    sync.Mutex
	view  contract.TunnelSettings
	cfg   config.TunnelConfig
	err   error
	calls int
}

func newTunnelControlFixture() *tunnelControlFixture {
	return &tunnelControlFixture{view: contract.TunnelSettings{Mode: "quick", Source: "config", State: contract.TunnelOff, CanEnable: true, StorageAvailable: true}}
}

func (c *tunnelControlFixture) Settings() contract.TunnelSettings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view
}

func (c *tunnelControlFixture) UpdateSettings(in contract.TunnelSettingsUpdate) (contract.TunnelSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return c.view, c.err
	}
	next, err := tunnelsettings.Prepare(c.cfg, in.Mode, in.Hostname, in.Token)
	if err != nil {
		return c.view, err
	}
	c.cfg = next
	c.view.Mode, c.view.Hostname, c.view.TokenConfigured, c.view.Source = in.Mode, next.Hostname, next.Token != "", "web"
	if c.view.Enabled {
		c.view.State = contract.TunnelStarting
	}
	return c.view, nil
}

func (c *tunnelControlFixture) ResetSettings() (contract.TunnelSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return c.view, c.err
	}
	c.cfg = config.TunnelConfig{}
	c.view = contract.TunnelSettings{Mode: "quick", Source: "config", State: contract.TunnelOff, CanEnable: true, StorageAvailable: true}
	return c.view, nil
}

func (c *tunnelControlFixture) SetEnabled(enabled bool) (contract.TunnelStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return contract.TunnelStatus{}, c.err
	}
	c.view.Enabled, c.cfg.Enabled, c.view.Source = enabled, enabled, "web"
	c.view.State = contract.TunnelOff
	if enabled {
		c.view.State = contract.TunnelStarting
	}
	return contract.TunnelStatus{State: c.view.State, Enabled: &enabled, SettingsAvailable: true}, nil
}

func tunnelAPIHandler(t *testing.T, control TunnelSettingsControl) http.Handler {
	t.Helper()
	m := session.NewManager(session.Options{DefaultCwd: t.TempDir()})
	t.Cleanup(func() { m.Shutdown(context.Background()) })
	return New(testDeps(t, Deps{Config: config.Config{Token: "login-secret"}, Manager: m, TunnelSettings: control}))
}

func tunnelAPIRequest(h http.Handler, method, path, body, token, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:7681"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestTunnelEndpointsSecurityAndStrictBodies(t *testing.T) {
	control := newTunnelControlFixture()
	h := tunnelAPIHandler(t, control)
	for _, endpoint := range []struct{ method, path, body string }{
		{"GET", "/api/tunnel", ""},
		{"PUT", "/api/tunnel/settings", `{"mode":"quick","hostname":""}`},
		{"PUT", "/api/tunnel/enabled", `{"enabled":true}`},
		{"DELETE", "/api/tunnel/settings", ""},
	} {
		for _, auth := range []struct {
			token, origin string
			status        int
		}{
			{"", "", 401}, {"wrong", "", 401}, {"login-secret", "https://evil.example", 403},
		} {
			w := tunnelAPIRequest(h, endpoint.method, endpoint.path, endpoint.body, auth.token, auth.origin)
			if w.Code != auth.status {
				t.Fatalf("%s %s: %d", endpoint.method, endpoint.path, w.Code)
			}
		}
	}
	if control.calls != 0 {
		t.Fatal("unauthorized request reached control boundary")
	}
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"yes"}`, `{"enabled":true,"extra":1}`, `null`, `{"enabled":true}{}`} {
		w := tunnelAPIRequest(h, "PUT", "/api/tunnel/enabled", body, "login-secret", "")
		if w.Code != 400 {
			t.Fatalf("invalid switch body accepted: %s", body)
		}
	}
	if control.calls != 0 {
		t.Fatal("missing enabled changed current intent")
	}
	for _, body := range []string{
		`{"mode":"named","hostname":"example.com","token":""}`,
		`{"mode":"named","hostname":"example.com"}`,
		`{"mode":"named","hostname":"https://example.com","token":"named-private"}`,
		`{"mode":"quick","named-private":true}`,
		`{"mode":"quick","token":123}`,
		`{"mode":"quick"}{}`,
	} {
		w := tunnelAPIRequest(h, "PUT", "/api/tunnel/settings", body, "login-secret", "")
		if w.Code != 400 || strings.Contains(w.Body.String(), "named-private") {
			t.Fatalf("unsafe validation response %d %s", w.Code, w.Body.String())
		}
	}
}

func TestTunnelEndpointsViewsAndErrors(t *testing.T) {
	control := newTunnelControlFixture()
	h := tunnelAPIHandler(t, control)
	for _, row := range []struct {
		method, path, body string
		status             int
	}{
		{"GET", "/api/tunnel", "", 200},
		{"PUT", "/api/tunnel/settings", `{"mode":"named","hostname":"old.example.com","token":"named-private"}`, 200},
		{"PUT", "/api/tunnel/settings", `{"mode":"named","hostname":"new.example.com"}`, 200},
		{"PUT", "/api/tunnel/enabled", `{"enabled":true}`, 202},
		{"PUT", "/api/tunnel/settings", `{"mode":"quick","hostname":""}`, 202},
		{"PUT", "/api/tunnel/enabled", `{"enabled":false}`, 202},
		{"DELETE", "/api/tunnel/settings", "", 200},
	} {
		w := tunnelAPIRequest(h, row.method, row.path, row.body, "login-secret", "")
		if w.Code != row.status {
			t.Fatalf("%s %s: %d %s", row.method, row.path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "named-private") || strings.Contains(w.Body.String(), "login-secret") {
			t.Fatal("view leaked a credential")
		}
		var view contract.TunnelSettings
		if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if row.method == "DELETE" && (view.Source != "config" || view.Enabled || view.TokenConfigured) {
			t.Fatal("reset view did not reflect defaults")
		}
	}
	for _, row := range []struct {
		err    error
		status int
	}{
		{tunnelsettings.ErrInvalid, 400}, {tunnelsettings.ErrEnableBlocked, 409}, {tunnelsettings.ErrUnavailable, 503},
		{errors.Join(tunnelsettings.ErrNotPersisted, tunnelsettings.ErrUnavailable), 503}, {errors.New("untrusted-private-value"), 500},
	} {
		control.err = row.err
		w := tunnelAPIRequest(h, "PUT", "/api/tunnel/enabled", `{"enabled":false}`, "login-secret", "")
		if w.Code != row.status || strings.Contains(w.Body.String(), "untrusted-private-value") {
			t.Fatalf("unsafe error %d %s", w.Code, w.Body.String())
		}
		if errors.Is(row.err, tunnelsettings.ErrNotPersisted) && !strings.Contains(w.Body.String(), `"notPersisted":true`) {
			t.Fatal("failed-persistence result lost distinction")
		}
	}
	control.view.StorageAvailable, control.view.StorageError = false, "storage unavailable"
	w := tunnelAPIRequest(h, "GET", "/api/tunnel", "", "login-secret", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"storageAvailable":false`) {
		t.Fatal("storage fault hid readable status")
	}
}

func TestTunnelDynamicConfigAndPushOrigin(t *testing.T) {
	control := newTunnelControlFixture()
	control.view.Mode, control.view.Hostname = "named", "new.example.com"
	s := &Server{Deps: Deps{Config: config.Config{Tunnel: config.TunnelConfig{Token: "old-private", Hostname: "old.example.com"}}, TunnelSettings: control}}
	if got := s.tunnelInfo(); !got.Named || got.Hostname != "new.example.com" {
		t.Fatalf("stale config %+v", got)
	}
	for _, host := range []string{"new.example.com", "old.example.com"} {
		r := httptest.NewRequest("GET", "https://"+host+"/api/push", nil)
		r.Header.Set("Origin", "https://"+host)
		origin, reason := s.pushOrigin(r)
		if host == "new.example.com" && (origin != "https://new.example.com" || reason != "") {
			t.Fatal("new push origin unavailable")
		}
		if host == "old.example.com" && reason != pushUnsupportedOrigin {
			t.Fatal("old push origin remained eligible")
		}
	}
}

func TestWireSnapshotsTunnel(t *testing.T) {
	rec := &wireRecorder{t: t, norm: newWireNormalizer(), seen: map[string]bool{}}
	control := newTunnelControlFixture()
	h := tunnelAPIHandler(t, control)
	for _, row := range []struct {
		name, method, path, body, token, origin string
		err                                     error
	}{
		{"tunnel-get", "GET", "/api/tunnel", "", "login-secret", "", nil},
		{"tunnel-settings", "PUT", "/api/tunnel/settings", `{"mode":"named","hostname":"term.example.com","token":"private"}`, "login-secret", "", nil},
		{"tunnel-enabled", "PUT", "/api/tunnel/enabled", `{"enabled":true}`, "login-secret", "", nil},
		{"tunnel-reset", "DELETE", "/api/tunnel/settings", "", "login-secret", "", nil},
		{"tunnel-error-400", "PUT", "/api/tunnel/enabled", `{}`, "login-secret", "", nil},
		{"tunnel-error-401", "GET", "/api/tunnel", "", "", "", nil},
		{"tunnel-error-403", "GET", "/api/tunnel", "", "login-secret", "https://evil.example", nil},
		{"tunnel-error-409", "PUT", "/api/tunnel/enabled", `{"enabled":true}`, "login-secret", "", tunnelsettings.ErrEnableBlocked},
		{"tunnel-error-503", "PUT", "/api/tunnel/enabled", `{"enabled":false}`, "login-secret", "", errors.Join(tunnelsettings.ErrNotPersisted, tunnelsettings.ErrUnavailable)},
	} {
		control.err = row.err
		w := tunnelAPIRequest(h, row.method, row.path, row.body, row.token, row.origin)
		rec.record(row.name, w.Code, w.Body.Bytes())
	}
	m := session.NewManager(session.Options{DefaultCwd: t.TempDir()})
	t.Cleanup(func() { m.Shutdown(context.Background()) })
	enabled := false
	local := NewLocal(testDeps(t, Deps{Manager: m}), Local{Status: func() contract.StatusV1 {
		return contract.StatusV1{V: 1, State: contract.StateRunning, Record: true, LocalAPI: true,
			Tunnel: &contract.TunnelStatus{State: contract.TunnelOff, Enabled: &enabled, SettingsAvailable: true}}
	}})
	w := httptest.NewRecorder()
	local.ServeHTTP(w, httptest.NewRequest("GET", contract.PathStatus, nil))
	rec.record("local-status-tunnel-settings", w.Code, w.Body.Bytes())
}

func TestTunnelConcurrentAPISettings(t *testing.T) {
	control := newTunnelControlFixture()
	h := tunnelAPIHandler(t, control)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			w := tunnelAPIRequest(h, "PUT", "/api/tunnel/settings", `{"mode":"named","hostname":"term.example.com","token":"concurrent-private"}`, "login-secret", "")
			if w.Code != 200 || strings.Contains(w.Body.String(), "concurrent-private") {
				t.Errorf("concurrent response %d %s", w.Code, w.Body.String())
			}
		})
	}
	wg.Wait()
	if control.calls != 12 || !control.Settings().TokenConfigured {
		t.Fatal("concurrent requests lost updates")
	}
}
