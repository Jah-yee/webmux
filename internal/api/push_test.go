package api

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/push"
	"github.com/rainchan/webmux/internal/session"
)

func TestPushOrigin(t *testing.T) {
	named := config.Config{Tunnel: config.TunnelConfig{Token: "cred", Hostname: "term.example.com"}}
	quick := &PublicOrigin{}
	if err := quick.Set("https://abc.trycloudflare.com"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name         string
		server       *Server
		host, origin string
		want, reason string
	}{
		{"named origin", &Server{Deps: Deps{Config: named}}, "term.example.com", "https://term.example.com", "https://term.example.com", ""},
		{"named without origin header", &Server{Deps: Deps{Config: named}}, "term.example.com", "", "https://term.example.com", ""},
		{"named http origin", &Server{Deps: Deps{Config: named}}, "term.example.com", "http://term.example.com", "", pushUnsupportedOrigin},
		{"quick tunnel", &Server{Deps: Deps{Public: quick}}, "abc.trycloudflare.com", "https://abc.trycloudflare.com", "", pushQuickTunnel},
		{"quick tunnel without origin header", &Server{Deps: Deps{Public: quick}}, "abc.trycloudflare.com", "", "", pushQuickTunnel},
		{"quick tunnel stopped", &Server{}, "abc.trycloudflare.com", "https://abc.trycloudflare.com", "", pushUnsupportedOrigin},
		{"ipv4 loopback", &Server{}, "127.0.0.1:7681", "http://127.0.0.1:7681", "http://127.0.0.1:7681", ""},
		{"ipv6 loopback", &Server{}, "[::1]:7681", "http://[::1]:7681", "http://[::1]:7681", ""},
		{"localhost without origin header", &Server{}, "localhost:7681", "", "http://localhost:7681", ""},
		{"vpn address", &Server{Deps: Deps{Config: config.Config{AllowedOrigins: []string{"http://100.64.0.2:7681"}}}}, "100.64.0.2:7681", "http://100.64.0.2:7681", "", pushUnsupportedOrigin},
		{"other https proxy", &Server{Deps: Deps{Config: named}}, "box.tail1234.ts.net", "https://box.tail1234.ts.net", "", pushUnsupportedOrigin},
		{"local socket", &Server{Deps: Deps{Config: named}, local: true}, "webmux", "", "", pushUnsupportedOrigin},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://"+row.host+"/api/push", nil)
		if row.origin != "" {
			r.Header.Set("Origin", row.origin)
		}
		origin, reason := row.server.pushOrigin(r)
		if origin != row.want || reason != row.reason {
			t.Errorf("%s: (%q, %q), want (%q, %q)", row.name, origin, reason, row.want, row.reason)
		}
	}
}

func browserSubscription(t *testing.T, endpoint string) string {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	// Same shape as PushSubscription.toJSON() in browsers, including the
	// always-present expirationTime.
	body, _ := json.Marshal(map[string]any{"endpoint": endpoint, "expirationTime": nil, "keys": map[string]string{
		"p256dh": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		"auth":   base64.RawURLEncoding.EncodeToString(auth),
	}})
	return string(body)
}

func TestPushAPI(t *testing.T) {
	pushService := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/gone") {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer pushService.Close()
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	cfg := config.Config{Listen: "127.0.0.1:7681", Token: "secret", Push: config.PushConfig{Enabled: true, Subject: config.DefaultPushSubject, Kinds: config.DefaultPushKinds()}}
	pushes := push.New(cfg.Push, t.TempDir(), m, slog.New(slog.NewTextHandler(io.Discard, nil)), pushService.Client().Transport)
	defer pushes.Close()
	public := &PublicOrigin{}
	if err := public.Set("https://abc.trycloudflare.com"); err != nil {
		t.Fatal(err)
	}
	handler := New(testDeps(t, Deps{Config: cfg, Manager: m, Workspace: testWorkspace(t, m, t.TempDir()), Public: public, Push: pushes}))
	var bodies strings.Builder
	call := func(method, path, origin, token, body string) (int, map[string]any) {
		t.Helper()
		host := "127.0.0.1:7681"
		if strings.Contains(origin, "trycloudflare") {
			host = "abc.trycloudflare.com"
		}
		r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		bodies.WriteString(w.Body.String())
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	const loopback = "http://127.0.0.1:7681"
	if code, _ := call("GET", "/api/push", loopback, "", ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status: %d", code)
	}
	code, status := call("GET", "/api/push", "", "secret", "")
	if code != 200 || status["enabled"] != true || status["available"] != true || status["subscriptions"] != float64(0) || len(status["publicKey"].(string)) != 87 || status["reason"] != nil {
		t.Fatalf("loopback status %d %v", code, status)
	}
	if code, status := call("GET", "/api/push", "https://abc.trycloudflare.com", "secret", ""); code != 200 || status["available"] != false || status["reason"] != pushQuickTunnel || status["publicKey"] != nil {
		t.Fatalf("quick tunnel status %d %v", code, status)
	}

	device := browserSubscription(t, pushService.URL+"/device/SECRET-PATH")
	if code, _ := call("POST", "/api/push/subscriptions", "https://evil.example", "secret", device); code != http.StatusForbidden {
		t.Fatalf("foreign origin: %d", code)
	}
	if code, out := call("POST", "/api/push/subscriptions", "https://abc.trycloudflare.com", "secret", device); code != http.StatusConflict || out["reason"] != pushQuickTunnel || !strings.Contains(out["error"].(string), "Named Tunnel") {
		t.Fatalf("quick tunnel registration: %d %v", code, out)
	}
	if code, out := call("POST", "/api/push/subscriptions", "https://abc.trycloudflare.com", "secret", `{}`); code != http.StatusBadRequest || out["reason"] != nil {
		t.Fatalf("malformed subscription must precede origin conflict: %d %v", code, out)
	}
	if code, _ := call("POST", "/api/push/subscriptions", loopback, "secret", browserSubscription(t, "http://push.example/x")); code != http.StatusBadRequest {
		t.Fatalf("http endpoint: %d", code)
	}
	_, first := call("POST", "/api/push/subscriptions", loopback, "secret", device)
	code, second := call("POST", "/api/push/subscriptions", loopback, "secret", device)
	if code != 200 || first["id"] == nil || first["id"] != second["id"] {
		t.Fatalf("idempotent registration: %v %v", first, second)
	}
	_, gone := call("POST", "/api/push/subscriptions", loopback, "secret", browserSubscription(t, pushService.URL+"/gone/SECRET-PATH"))
	if _, status := call("GET", "/api/push", "", "secret", ""); status["subscriptions"] != float64(2) {
		t.Fatalf("subscriptions %v", status)
	}

	// The test reaches every device: one accepts, one is expired and deleted.
	code, report := call("POST", "/api/push/test", loopback, "secret", "")
	removed, _ := report["removed"].([]any)
	if code != http.StatusOK || report["devices"] != float64(2) || report["accepted"] != float64(1) || len(removed) != 1 || removed[0] != gone["id"] || len(report["errors"].([]any)) != 0 {
		t.Fatalf("test notification: %d %v", code, report)
	}
	if _, status := call("GET", "/api/push", "", "secret", ""); status["subscriptions"] != float64(1) {
		t.Fatalf("gone subscription kept: %v", status)
	}
	for range 2 {
		if code, _ := call("DELETE", "/api/push/subscriptions/"+first["id"].(string), loopback, "secret", ""); code != http.StatusNoContent {
			t.Fatalf("delete: %d", code)
		}
	}
	if _, status := call("GET", "/api/push", "", "secret", ""); status["subscriptions"] != float64(0) {
		t.Fatalf("after delete: %v", status)
	}
	call("GET", "/api/config", "", "secret", "")
	var keys map[string]map[string]string
	_ = json.Unmarshal([]byte(device), &keys)
	for _, secret := range []string{"SECRET-PATH", keys["keys"]["p256dh"], keys["keys"]["auth"]} {
		if strings.Contains(bodies.String(), secret) {
			t.Fatalf("response leaked %q", secret)
		}
	}
}

func TestPushAPIWhenDisabled(t *testing.T) {
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := New(testDeps(t, Deps{Config: config.Config{}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}))
	for _, row := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/push", "", http.StatusOK},
		{"POST", "/api/push/subscriptions", browserSubscription(t, "https://push.example/x"), http.StatusConflict},
		{"POST", "/api/push/subscriptions", `{}`, http.StatusConflict},
		{"POST", "/api/push/test", "", http.StatusConflict},
		{"DELETE", "/api/push/subscriptions/x", "", http.StatusConflict},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(row.method, "http://127.0.0.1:7681"+row.path, strings.NewReader(row.body)))
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		if w.Code != row.want || out["reason"] != pushDisabled {
			t.Errorf("%s %s: %d %v", row.method, row.path, w.Code, out)
		}
		if row.path == "/api/push" && (out["enabled"] != false || out["available"] != false) {
			t.Errorf("disabled status %v", out)
		}
	}
}
