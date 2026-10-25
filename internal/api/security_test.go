package api

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
	assets "github.com/rainchan/webmux/web"
)

func TestAuthenticationAndOriginBoundaries(t *testing.T) {
	c := config.Config{Token: "secret", AllowedOrigins: []string{"https://trusted.example"}}
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := New(testDeps(t, Deps{Config: c, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}))
	for _, row := range []struct {
		name, authorization, cookie, query, origin string
		want                                       int
	}{
		{"bearer", "Bearer secret", "", "", "http://webmux.example", 200},
		{"bearer case insensitive", "bearer secret", "", "", "", 200},
		{"cookie", "", "secret", "", "", 200},
		{"HTTP query forbidden", "", "", "?token=secret", "", 401},
		{"incorrect bearer", "Bearer wrong", "secret", "", "", 401},
		{"allowed cross origin", "Bearer secret", "", "", "https://trusted.example", 200},
		{"untrusted cross origin", "Bearer secret", "", "", "https://evil.example", 403},
		{"different scheme", "Bearer secret", "", "", "https://webmux.example", 403},
		{"opaque origin", "Bearer secret", "", "", "null", 403},
		{"origin query", "Bearer secret", "", "", "http://webmux.example?ignored", 403},
		{"empty origin query", "Bearer secret", "", "", "http://webmux.example?", 403},
		{"origin fragment", "Bearer secret", "", "", "http://webmux.example#ignored", 403},
		{"empty origin fragment", "Bearer secret", "", "", "http://webmux.example#", 403},
		{"origin credentials", "Bearer secret", "", "", "http://user@webmux.example", 403},
	} {
		t.Run(row.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://webmux.example/api/health"+row.query, nil)
			r.Header.Set("Authorization", row.authorization)
			r.Header.Set("Origin", row.origin)
			if row.cookie != "" {
				r.AddCookie(&http.Cookie{Name: "webmux_token", Value: row.cookie})
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != row.want {
				t.Fatalf("HTTP status %d, want %d: %s", w.Code, row.want, w.Body.String())
			}
			if w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("missing security response headers")
			}
		})
	}
}

// tokenProtocols is the subprotocol list the frontend offers for token.
func tokenProtocols(token string) []string {
	return []string{"webmux", "webmux.token." + base64.RawURLEncoding.EncodeToString([]byte(token))}
}

func TestWebSocketHandshakeBoundaries(t *testing.T) {
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	server := httptest.NewServer(New(testDeps(t, Deps{Config: config.Config{Token: "secret"}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())})))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	secret := tokenProtocols("secret")
	for _, row := range []struct {
		query, cookie, origin string
		protocols             []string
		want                  int
	}{
		{"", "", "", nil, 401},
		{"", "", "", tokenProtocols("wrong"), 401},
		{"?token=secret", "", server.URL, nil, 401},
		{"", "", "https://evil.example", secret, 403},
		{"", "", server.URL, secret, 101},
		{"", "webmux_token=secret", server.URL, nil, 101},
	} {
		header := make(http.Header)
		header.Set("Cookie", row.cookie)
		header.Set("Origin", row.origin)
		conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/s/missing"+row.query, &websocket.DialOptions{HTTPHeader: header, Subprotocols: row.protocols})
		if response == nil || response.StatusCode != row.want {
			t.Fatalf("handshake status: response=%v err=%v want=%d", response, err, row.want)
		}
		if row.want != 101 {
			if err == nil {
				_ = conn.CloseNow()
				t.Fatal("unauthorized websocket upgraded")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		// Only the fixed protocol is selected; the token protocol is never echoed.
		want := ""
		if row.protocols != nil {
			want = "webmux"
		}
		if conn.Subprotocol() != want {
			t.Fatalf("selected subprotocol %q, want %q", conn.Subprotocol(), want)
		}
		_, _, err = conn.Read(ctx)
		_ = conn.CloseNow()
		if websocket.CloseStatus(err) != 4404 {
			t.Fatalf("missing session close: %v", err)
		}
	}
}

func TestPublicOriginBoundaries(t *testing.T) {
	c := config.Config{Listen: "127.0.0.1:7681", Token: "secret"}
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	public := &PublicOrigin{}
	handler := New(testDeps(t, Deps{Config: c, Manager: m, Workspace: testWorkspace(t, m, t.TempDir()), Public: public}))
	request := func(host, origin string) int {
		r := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/fs/mkdir", strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	const host = "abc.trycloudflare.com"
	if got := request(host, "https://"+host); got != http.StatusForbidden {
		t.Fatalf("public host before Set: %d", got)
	}
	if err := public.Set("https://ABC.trycloudflare.com"); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name, host, origin string
		want               int
	}{
		// 400 means the request passed Host, auth, and Origin and reached the handler.
		{"public https origin", host, "https://" + host, http.StatusBadRequest},
		{"public host without origin", host, "", http.StatusBadRequest},
		{"public http origin", host, "http://" + host, http.StatusForbidden},
		{"public origin on direct host", "127.0.0.1:7681", "https://" + host, http.StatusForbidden},
		{"other public origin", host, "https://evil.trycloudflare.com", http.StatusForbidden},
		{"unrelated host", "evil.example", "https://evil.example", http.StatusForbidden},
		{"direct same origin", "127.0.0.1:7681", "http://127.0.0.1:7681", http.StatusBadRequest},
	} {
		if got := request(row.host, row.origin); got != row.want {
			t.Errorf("%s: status %d, want %d", row.name, got, row.want)
		}
	}
	public.Clear()
	if got := request(host, "https://"+host); got != http.StatusForbidden {
		t.Fatalf("public host after Clear: %d", got)
	}
	for _, invalid := range []string{"http://abc.example", "https://abc.example/x", "https://abc.example:8443", "https://u@abc.example", "https://abc.example?x"} {
		if public.Set(invalid) == nil {
			t.Errorf("accepted public origin %q", invalid)
		}
	}
}

func TestPublicOriginWebSocketHandshake(t *testing.T) {
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	public := &PublicOrigin{}
	server := httptest.NewServer(New(testDeps(t, Deps{Config: config.Config{Listen: "127.0.0.1:7681", Token: "secret"}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir()), Public: public})))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const host = "abc.trycloudflare.com"
	dial := func() (*websocket.Conn, *http.Response, error) {
		header := http.Header{"Origin": {"https://" + host}}
		return websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/sessions", &websocket.DialOptions{HTTPHeader: header, Host: host, Subprotocols: tokenProtocols("secret")})
	}
	if err := public.Set("https://" + host); err != nil {
		t.Fatal(err)
	}
	conn, _, err := dial()
	if err != nil {
		t.Fatalf("public handshake: %v", err)
	}
	if _, data, err := conn.Read(ctx); err != nil || !strings.Contains(string(data), `"snapshot"`) {
		t.Fatalf("snapshot: %q %v", data, err)
	}
	_ = conn.CloseNow()
	public.Clear()
	conn, response, err := dial()
	if err == nil {
		_ = conn.CloseNow()
		t.Fatal("handshake accepted after Clear")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("handshake after Clear: %v %v", response, err)
	}
}

func TestStaticFontFilesAreTypedAndImmutable(t *testing.T) {
	root, err := fs.Sub(assets.Assets, "dist")
	if err != nil {
		t.Fatal(err)
	}
	var name string
	err = fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || name != "" || entry.IsDir() || !strings.HasSuffix(path, ".woff2") {
			return err
		}
		name = path
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if name == "" {
		t.Skip("frontend build has no font files")
	}
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := New(testDeps(t, Deps{Config: config.Config{}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}))
	r := httptest.NewRequest(http.MethodGet, "/"+name, nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d for %s", w.Code, name)
	}
	if w.Header().Get("Content-Type") != "font/woff2" {
		t.Fatalf("content type %q", w.Header().Get("Content-Type"))
	}
	if w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("cache control %q", w.Header().Get("Cache-Control"))
	}
	if w.Header().Get("Content-Encoding") != "" {
		t.Fatal("woff2 responses are already compressed")
	}
	if w.Body.Len() < 100 {
		t.Fatalf("font response is %d bytes", w.Body.Len())
	}
}

func TestStaticStylesheetGzip(t *testing.T) {
	root, err := fs.Sub(assets.Assets, "dist")
	if err != nil {
		t.Fatal(err)
	}
	var name string
	var largest int64
	err = fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".css") {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > largest {
			largest = info.Size()
			name = path
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if name == "" {
		t.Skip("frontend build has no stylesheet")
	}
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := New(testDeps(t, Deps{Config: config.Config{}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}))
	r := httptest.NewRequest(http.MethodGet, "/"+name, nil)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d for %s", w.Code, name)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("content encoding %q", w.Header().Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 100 {
		t.Fatalf("decoded stylesheet is %d bytes", len(body))
	}
}

func TestStaticFallbackDoesNotMaskMissingResources(t *testing.T) {
	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	handler := New(testDeps(t, Deps{Config: config.Config{Token: "secret"}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}))
	for _, row := range []struct {
		path string
		want int
	}{
		{"/", 200},
		{"/s/example", 200},
		{"/assets/missing.js", 404},
		{"/assets/missing", 404},
		{"/missing.css", 404},
		{"/.env", 404},
		{"/nested/.env", 404},
		{"/api/missing", 404},
		{"/ws/missing", 404},
	} {
		r := httptest.NewRequest(http.MethodGet, row.path, nil)
		if strings.HasPrefix(row.path, "/api/") || strings.HasPrefix(row.path, "/ws/") {
			r.Header.Set("Authorization", "Bearer secret")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != row.want {
			t.Errorf("%s: status=%d want=%d", row.path, w.Code, row.want)
		}
		if row.want == 200 && !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s: fallback content type %q", row.path, w.Header().Get("Content-Type"))
		}
	}
}
