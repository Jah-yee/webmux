package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
)

// TestConfigTunnelStatus checks the read-only tunnel object exposed to the
// settings page. Neither credential may reach the browser.
func TestConfigTunnelStatus(t *testing.T) {
	const (
		loginToken = "webmux-login-token"
		credential = "cloudflare-tunnel-credential"
	)
	quick := config.Config{Token: loginToken}
	named := config.Config{
		Token:  loginToken,
		Tunnel: config.TunnelConfig{Token: credential, Hostname: "term.example.com"},
	}
	for _, row := range []struct {
		name string
		cfg  config.Config
		load func() contract.TunnelStatus
		want contract.TunnelInfo
	}{
		{
			name: "not configured",
			cfg:  quick,
			want: contract.TunnelInfo{State: contract.TunnelOff},
		},
		{
			name: "quick starting",
			cfg:  quick,
			load: func() contract.TunnelStatus { return contract.TunnelStatus{State: contract.TunnelStarting} },
			want: contract.TunnelInfo{Enabled: true, State: contract.TunnelStarting},
		},
		{
			name: "quick ready",
			cfg:  quick,
			load: func() contract.TunnelStatus {
				return contract.TunnelStatus{State: contract.TunnelReady, PublicURL: "https://abc-def.trycloudflare.com"}
			},
			want: contract.TunnelInfo{Enabled: true, State: contract.TunnelReady, PublicURL: "https://abc-def.trycloudflare.com"},
		},
		{
			name: "quick failed",
			cfg:  quick,
			load: func() contract.TunnelStatus {
				return contract.TunnelStatus{State: contract.TunnelFailed, Error: "cloudflared exited"}
			},
			want: contract.TunnelInfo{State: contract.TunnelFailed},
		},
		{
			name: "named ready",
			cfg:  named,
			load: func() contract.TunnelStatus {
				return contract.TunnelStatus{State: contract.TunnelReady, Named: true, PublicURL: "https://term.example.com"}
			},
			want: contract.TunnelInfo{
				Enabled:   true,
				State:     contract.TunnelReady,
				Named:     true,
				Hostname:  "term.example.com",
				PublicURL: "https://term.example.com",
			},
		},
		{
			name: "named missing hostname",
			cfg: config.Config{
				Token:  loginToken,
				Tunnel: config.TunnelConfig{Token: credential},
			},
			want: contract.TunnelInfo{State: contract.TunnelOff, Named: true},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			m := session.NewManager(session.Options{DefaultCwd: t.TempDir()})
			defer m.Shutdown(context.Background())
			handler := New(testDeps(t, Deps{Config: row.cfg, Manager: m, Workspace: testWorkspace(t, m, t.TempDir()), Tunnel: row.load}))
			req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
			req.Header.Set("Authorization", "Bearer "+loginToken)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			for _, secret := range []string{loginToken, credential} {
				if strings.Contains(body, secret) {
					t.Fatalf("response leaks %q: %s", secret, body)
				}
			}
			var response struct {
				Tunnel contract.TunnelInfo `json:"tunnel"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Tunnel != row.want {
				t.Fatalf("tunnel = %+v, want %+v", response.Tunnel, row.want)
			}
		})
	}
}
