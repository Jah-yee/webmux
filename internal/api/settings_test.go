package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/settings"
)

func settingsHandler(t *testing.T) http.Handler {
	t.Helper()
	prefs := settings.Open(t.TempDir())
	t.Cleanup(prefs.Close)
	m := session.NewManager(session.Options{DefaultCwd: t.TempDir()})
	t.Cleanup(func() { m.Shutdown(context.Background()) })
	return New(testDeps(t, Deps{Config: config.Config{Token: "login-secret"}, Manager: m, Settings: prefs}))
}
func TestSettingsHTTPContract(t *testing.T) {
	h := settingsHandler(t)
	for _, row := range []struct {
		method, body, token, origin string
		status                      int
	}{
		{"GET", "", "", "", 401}, {"PATCH", `{"baseRevision":0,"changes":{"theme":"dark"}}`, "login-secret", "https://foreign.example", 403},
		{"GET", "", "login-secret", "", 200},
		{"PATCH", `{"baseRevision":0,"changes":{"theme":"dark"}}`, "login-secret", "", 200},
		{"PATCH", `{"baseRevision":0,"changes":{"theme":"light"}}`, "login-secret", "", 409},
		{"PATCH", `{"baseRevision":1,"changes":{"theme":null,"quickCommands":[]}}`, "login-secret", "", 200},
		{"PATCH", `{"baseRevision":2,"changes":{"unknown":true}}`, "login-secret", "", 400},
		{"PATCH", `{"baseRevision":2,"changes":{"theme":"dark","theme":"light"}}`, "login-secret", "", 400},
		{"PATCH", `{"baseRevision":2,"changes":{"Theme":"dark"}}`, "login-secret", "", 400},
		{"PATCH", `{"changes":{"theme":"dark"}}`, "login-secret", "", 400},
		{"PATCH", `{"baseRevision":2,"changes":{"fontSize":"14"}}`, "login-secret", "", 400},
		{"PATCH", `{"baseRevision":2,"changes":{"quickCommands":[{"id":"a","name":"a","command":"pwd"}]}}`, "login-secret", "", 400},
		{"PATCH", `{"baseRevision":2,"changes":{"quickCommands":[{"id":"a","name":"a","command":"pwd","enter":null}]}}`, "login-secret", "", 400},
		{"PATCH", `{"baseRevision":2,"changes":{"preview":{"lines":10}}}`, "login-secret", "", 400},
		{"PATCH", strings.Repeat(" ", 2<<20) + `{}`, "login-secret", "", 400},
	} {
		w := tunnelAPIRequest(h, row.method, "/api/settings", row.body, row.token, row.origin)
		if w.Code != row.status {
			t.Fatalf("%s: status %d want %d: %s", row.method, w.Code, row.status, w.Body.String())
		}
	}
	w := tunnelAPIRequest(h, "GET", "/api/settings", "", "login-secret", "")
	var v contract.SettingsSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Revision != 2 || v.Values.Theme != nil || v.Values.QuickCommands == nil || len(*v.Values.QuickCommands) != 0 {
		t.Fatalf("snapshot: %+v", v)
	}
}
func TestSettingsWebSocketSnapshotThenUpdates(t *testing.T) {
	h := settingsHandler(t)
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/settings", &websocket.DialOptions{Subprotocols: []string{"webmux", "webmux.token.bG9naW4tc2VjcmV0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.CloseNow() }()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var first contract.SettingsEvent
	if err = json.Unmarshal(data, &first); err != nil {
		t.Fatal(err)
	}
	if first.Type != "settings" || first.Settings == nil || first.Settings.Revision != 0 {
		t.Fatalf("first: %s", data)
	}
	w := tunnelAPIRequest(h, "PATCH", "/api/settings", `{"baseRevision":0,"changes":{"theme":"dark"}}`, "login-secret", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	_, data, err = c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var update contract.SettingsEvent
	if err = json.Unmarshal(data, &update); err != nil {
		t.Fatal(err)
	}
	if update.Type != "settings.update" || update.Revision != 1 || update.Changes["theme"] != "dark" || len(update.Changes) != 1 {
		t.Fatalf("update: %s", data)
	}
}
