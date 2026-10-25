//go:build linux || darwin

package forward

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/files"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

const token = "forward-secret-token"

type backend struct {
	addr     string
	requests atomic.Int32
	close    func()
}

// newBackend serves the real WebMux handler on loopback, as WebMux does in
// WSL behind localhost forwarding.
func newBackend(t *testing.T) *backend {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(t.TempDir()+"/absent.toml", false)
	if err != nil {
		t.Fatal(err)
	}
	c.Listen, c.Token, c.StateDir, c.DefaultCwd, c.Shell = l.Addr().String(), token, t.TempDir(), t.TempDir(), "/bin/sh"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	m := session.NewManager(session.Options{DefaultCwd: c.DefaultCwd, ScrollbackBytes: c.ScrollbackBytes})
	ws := workspace.New(c.StateDir, m, nil, c.DefaultCwd)
	b := &backend{addr: l.Addr().String()}
	handler := api.New(api.Deps{Config: c, Manager: m, Workspace: ws, Code: codeview.New(m), Files: files.New(c.DefaultCwd)})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.requests.Add(1)
		handler.ServeHTTP(w, r)
	})}
	go func() { _ = server.Serve(l) }()
	b.close = func() {
		_ = server.Close()
		m.Shutdown(context.Background())
		ws.Close()
	}
	t.Cleanup(b.close)
	return b
}

type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logBuffer) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(&l.buf, format+"\n", args...)
}

func (l *logBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func startRelay(t *testing.T, target string, maxConns int, logs *logBuffer) (*Forwarder, string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f := serve(l, Options{Listen: l.Addr().String(), Target: target, MaxConns: maxConns, Logf: logs.logf})
	t.Cleanup(func() { _ = f.Close() })
	return f, l.Addr().String()
}

func request(t *testing.T, method, url, host, origin, auth string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestRelayCarriesRESTAndTerminal(t *testing.T) {
	b := newBackend(t)
	logs := &logBuffer{}
	_, relay := startRelay(t, b.addr, 0, logs)
	base := "http://" + relay
	self := base

	resp := request(t, http.MethodPost, base+"/api/sessions", "", self, token, strings.NewReader(`{"preset":"custom","cmd":["cat"]}`))
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create through relay: %d %s", resp.StatusCode, body)
	}
	var info contract.SessionInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if resp := request(t, http.MethodGet, base+"/api/sessions", "", "", "wrong", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://"+relay+"/ws/s/"+info.ID+"?client=phone", &websocket.DialOptions{
		HTTPHeader:   http.Header{"Origin": {self}},
		Subprotocols: []string{"webmux", "webmux.token." + base64.RawURLEncoding.EncodeToString([]byte(token))},
		HTTPClient:   &http.Client{Transport: &http.Transport{Proxy: nil}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	if err := conn.Write(ctx, websocket.MessageBinary, append([]byte{0x00}, "through-relay\n"...)); err != nil {
		t.Fatal(err)
	}
	var seen []byte
	for !bytes.Contains(seen, []byte("through-relay")) {
		_, frame, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("terminal output %q: %v", seen, err)
		}
		if len(frame) > 0 && (frame[0] == 0x00 || frame[0] == 0x01) {
			seen = append(seen, frame[1:]...)
		}
	}
}

func TestRelayRejectsForeignHostAndOrigin(t *testing.T) {
	b := newBackend(t)
	logs := &logBuffer{}
	_, relay := startRelay(t, b.addr, 0, logs)
	base := "http://" + relay
	for name, row := range map[string]struct{ host, origin string }{
		"cross-site origin": {"", "https://evil.example"},
		"other scheme":      {"", "https://" + relay},
		"DNS rebinding":     {"evil.example:" + strings.Split(relay, ":")[1], ""},
		"backend host":      {b.addr, ""},
	} {
		before := b.requests.Load()
		resp := request(t, http.MethodGet, base+"/api/sessions", row.host, row.origin, token, nil)
		if resp.StatusCode != http.StatusForbidden || b.requests.Load() != before {
			t.Errorf("%s: %d, backend requests %d -> %d", name, resp.StatusCode, before, b.requests.Load())
		}
	}
}

func TestRelayLogsNoQueryTokens(t *testing.T) {
	b := newBackend(t)
	logs := &logBuffer{}
	_, relay := startRelay(t, b.addr, 0, logs)
	b.close() // every relayed request now fails and is logged
	resp := request(t, http.MethodGet, "http://"+relay+"/ws/sessions?token="+token, "", "", "", nil)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("closed backend: %d", resp.StatusCode)
	}
	if logs.String() == "" || strings.Contains(logs.String(), token) || strings.Contains(logs.String(), "?") {
		t.Fatalf("relay log %q", logs.String())
	}
}

func TestRelayLimitsConnectionsAndCloses(t *testing.T) {
	b := newBackend(t)
	logs := &logBuffer{}
	f, relay := startRelay(t, b.addr, 2, logs)
	var held []net.Conn
	for range 2 {
		c, err := net.Dial("tcp", relay)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	// Let the server accept both before the third arrives.
	deadline := time.Now().Add(5 * time.Second)
	for {
		f.listener.mu.Lock()
		n := len(f.listener.conns)
		f.listener.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("accepted %d connections", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	extra, err := net.Dial("tcp", relay)
	if err != nil {
		t.Fatal(err)
	}
	_ = extra.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := extra.Read(make([]byte, 1)); !errors.Is(err, io.EOF) && !strings.Contains(fmt.Sprint(err), "reset") {
		t.Fatalf("connection beyond the limit stayed open: %v", err)
	}
	_ = f.Close()
	for _, c := range held {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.Read(make([]byte, 1)); err == nil {
			t.Fatal("Close left a connection open")
		}
	}
}

func TestStartRejectsWildcardAndLoopback(t *testing.T) {
	for _, address := range []string{"0.0.0.0:7681", "127.0.0.1:7681", "[::]:7681", "localhost:7681", ":7681"} {
		if _, err := Start(Options{Listen: address, Target: "127.0.0.1:7681"}); !errors.Is(err, ErrAddress) {
			t.Errorf("%s: %v", address, err)
		}
	}
}
