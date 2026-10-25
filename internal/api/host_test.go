package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rainchan/webmux/internal/config"
)

func TestHostTrustBoundary(t *testing.T) {
	for _, row := range []struct {
		name, listen, host string
		origins            []string
		want               int
	}{
		{name: "rebound hostname", listen: "127.0.0.1:7681", host: "attacker.example:7681", want: 403},
		{name: "local IPv4", listen: "127.0.0.1:7681", host: "127.0.0.1:7681", want: 204},
		{name: "local hostname", listen: "127.0.0.1:7681", host: "localhost:7681", want: 204},
		{name: "local IPv6", listen: "[::1]:7681", host: "[::1]:7681", want: 204},
		{name: "loopback alias", listen: "localhost:7681", host: "127.0.0.1:7681", want: 204},
		{name: "other loopback address", listen: "127.0.0.1:7681", host: "127.0.0.2:7681", want: 204},
		{name: "VPN address", listen: "100.64.1.2:7681", host: "100.64.1.2:7681", want: 204},
		{name: "IPv6 VPN canonical address", listen: "[fd00:0:0::1]:7681", host: "[fd00::1]:7681", want: 204},
		{name: "VPN rejects local alias", listen: "100.64.1.2:7681", host: "localhost:7681", want: 403},
		{name: "VPN rejects rebound hostname", listen: "100.64.1.2:7681", host: "attacker.example:7681", want: 403},
		{name: "explicit proxy hostname", listen: "127.0.0.1:7681", host: "terminal.example", origins: []string{"https://terminal.example"}, want: 204},
		{name: "proxy hostname is case insensitive", listen: "127.0.0.1:7681", host: "TERMINAL.example", origins: []string{"https://terminal.example"}, want: 204},
		{name: "malformed origin is not trusted", listen: "127.0.0.1:7681", host: "attacker.example", origins: []string{"https://attacker.example/path"}, want: 403},
		{name: "wildcard accepts VPN IP", listen: "0.0.0.0:7681", host: "100.64.1.2:7681", want: 204},
		{name: "IPv6 wildcard accepts IP", listen: "[::]:7681", host: "[fd00::123]:7681", want: 204},
		{name: "wildcard accepts localhost", listen: ":7681", host: "localhost:7681", want: 204},
		{name: "IPv4 wildcard accepts localhost", listen: "0.0.0.0:7681", host: "localhost:7681", want: 204},
		{name: "IPv6 wildcard accepts localhost", listen: "[::]:7681", host: "LOCALHOST:7681", want: 204},
		{name: "wildcard rejects hostname", listen: ":7681", host: "attacker.example:7681", want: 403},
		{name: "wildcard accepts proxy hostname", listen: ":7681", host: "terminal.example", origins: []string{"https://terminal.example"}, want: 204},
		{name: "forwarded host cannot authorize", listen: "127.0.0.1:7681", host: "attacker.example:7681", want: 403},
	} {
		t.Run(row.name, func(t *testing.T) {
			s := &Server{Deps: Deps{Config: config.Config{Listen: row.listen, AllowedOrigins: row.origins}}}
			handler := s.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			// A rebound page sends a same-origin request, so Origin checks alone permit it.
			for _, path := range []string{"/", "/api/sessions", "/ws/sessions"} {
				r := httptest.NewRequest("POST", "http://"+row.host+path, nil)
				r.Header.Set("Origin", "http://"+row.host)
				r.Header.Set("X-Forwarded-Host", "localhost:7681")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != row.want {
					t.Errorf("%s status = %d, want %d", path, w.Code, row.want)
				}
			}
		})
	}
}
