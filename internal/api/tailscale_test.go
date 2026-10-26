package api

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/tailscalesettings"
)

type tailscaleControlFixture struct {
	mu    sync.Mutex
	view  contract.TailscaleStatus
	err   error
	calls int
	last  contract.TailscaleSettingsUpdate
}

func (c *tailscaleControlFixture) Status() contract.TailscaleStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.view
}

func (c *tailscaleControlFixture) Refresh() {}

func (c *tailscaleControlFixture) UpdateSettings(in contract.TailscaleSettingsUpdate) (contract.TailscaleStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.last = in
	if c.err != nil {
		return c.view, c.err
	}
	c.view.Mode, c.view.Port, c.view.State, c.view.Source = in.Mode, in.Port, contract.TailscaleStarting, "web"
	return c.view, nil
}

func (c *tailscaleControlFixture) ResetSettings() (contract.TailscaleStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.view.Mode, c.view.State, c.view.Source = "off", contract.TailscaleOff, "config"
	return c.view, c.err
}

func tailscaleAPIHandler(t *testing.T, control TailscaleControl) *Server {
	t.Helper()
	m := session.NewManager(session.Options{DefaultCwd: t.TempDir()})
	t.Cleanup(func() { m.Shutdown(context.Background()) })
	deps := testDeps(t, Deps{Manager: m, Tailscale: control})
	deps.Config.Token = "login-secret"
	return &Server{Deps: deps}
}

func TestTailscaleSettingsEndpointsRequireAuthAndOrigin(t *testing.T) {
	control := &tailscaleControlFixture{view: contract.TailscaleStatus{Mode: "off", State: contract.TailscaleOff}}
	h := New(tailscaleAPIHandler(t, control).Deps)
	for _, endpoint := range []struct{ method, path, body string }{
		{"PUT", "/api/tailscale/settings", `{"mode":"serve","port":0,"provider":"auto"}`},
		{"DELETE", "/api/tailscale/settings", ""},
	} {
		for _, auth := range []struct {
			token, origin string
			status        int
		}{
			{"", "", 401}, {"wrong", "", 401}, {"login-secret", "https://evil.example", 403},
		} {
			if w := tunnelAPIRequest(h, endpoint.method, endpoint.path, endpoint.body, auth.token, auth.origin); w.Code != auth.status {
				t.Fatalf("%s %s with %+v: %d", endpoint.method, endpoint.path, auth, w.Code)
			}
		}
	}
	for _, body := range []string{`{"mode":"serve","command":"/tmp/evil"}`, `{"mode":"serve","port":"443"}`, `null{}`} {
		if w := tunnelAPIRequest(h, "PUT", "/api/tailscale/settings", body, "login-secret", ""); w.Code != 400 {
			t.Fatalf("body %s: %d", body, w.Code)
		}
	}
	if control.calls != 0 {
		t.Fatal("rejected request reached the controller")
	}
}

func TestTailscaleSettingsStatusMapping(t *testing.T) {
	control := &tailscaleControlFixture{view: contract.TailscaleStatus{Mode: "off", State: contract.TailscaleOff}}
	h := New(tailscaleAPIHandler(t, control).Deps)
	body := `{"mode":"funnel","port":443,"provider":"auto","confirmFunnel":true}`
	w := tunnelAPIRequest(h, "PUT", "/api/tailscale/settings", body, "login-secret", "")
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"source":"web"`) || !control.last.ConfirmFunnel {
		t.Fatalf("accepted update = %d %s", w.Code, w.Body.String())
	}
	for _, row := range []struct {
		err    error
		status int
	}{
		{tailscalesettings.ErrFunnelUnconfirmed, 400},
		{fmt.Errorf("%w: bad port", tailscalesettings.ErrInvalid), 400},
		{fmt.Errorf("%w: weak token", tailscalesettings.ErrEnableBlocked), 409},
		{fmt.Errorf("%w: locked", tailscalesettings.ErrUnavailable), 503},
	} {
		control.err = row.err
		if w := tunnelAPIRequest(h, "PUT", "/api/tailscale/settings", body, "login-secret", ""); w.Code != row.status {
			t.Fatalf("%v: %d", row.err, w.Code)
		}
	}
	control.err = nil
	if w := tunnelAPIRequest(h, "DELETE", "/api/tailscale/settings", "", "login-secret", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"source":"config"`) {
		t.Fatalf("reset = %d %s", w.Code, w.Body.String())
	}
}

func TestLocalTailscaleSettingsCannotBypassSessionPolicy(t *testing.T) {
	control := &tailscaleControlFixture{}
	s := tailscaleAPIHandler(t, control)
	h := NewLocal(s.Deps, Local{InSession: func(pid int) bool { return pid == 20 }})
	for _, endpoint := range []struct{ method, path, body string }{
		{"PUT", "/api/tailscale/settings", `{"mode":"serve","port":0,"provider":"auto"}`},
		{"DELETE", "/api/tailscale/settings", ""},
	} {
		for _, pid := range []int{0, 20} {
			r := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body))
			if pid != 0 {
				r = r.WithContext(context.WithValue(r.Context(), peerKey{}, pid))
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 403 {
				t.Fatalf("session bypass via %s %s: %d", endpoint.method, endpoint.path, w.Code)
			}
		}
	}
	if control.calls != 0 {
		t.Fatal("session reached the Tailscale controller")
	}
}
