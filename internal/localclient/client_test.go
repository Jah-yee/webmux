package localclient

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/contract"
)

// pipeListener hands the server half of each net.Pipe to http.Server.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	dials  atomic.Int32
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { close(l.closed); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.UnixAddr{Name: "pipe", Net: "unix"} }

func (l *pipeListener) DialContext(ctx context.Context) (net.Conn, error) {
	l.dials.Add(1)
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func serve(t *testing.T, handler http.Handler) *pipeListener {
	l := &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(l) }()
	t.Cleanup(func() { _ = server.Close() })
	return l
}

func TestClientOverDialer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+contract.PathStatus, func(w http.ResponseWriter, r *http.Request) {
		pid := 42
		_ = json.NewEncoder(w).Encode(contract.StatusV1{V: 1, State: contract.StateRunning, PID: &pid})
	})
	mux.HandleFunc("POST "+contract.PathShutdown, func(w http.ResponseWriter, r *http.Request) {
		var req contract.ShutdownRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Force {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(contract.ErrorResponse{Error: "use systemctl --user stop webmux"})
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("GET "+contract.PathToken, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("GET /ws/echo", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{WSProtocol}})
		if err != nil {
			return
		}
		kind, data, err := c.Read(r.Context())
		if err == nil {
			_ = c.Write(r.Context(), kind, data)
		}
		_ = c.Close(websocket.StatusNormalClosure, "")
	})
	l := serve(t, mux)
	client := New(l)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for range 3 {
		status, err := client.Status(ctx)
		if err != nil || status.PID == nil || *status.PID != 42 {
			t.Fatalf("status %+v %v", status, err)
		}
	}
	if n := l.dials.Load(); n != 1 {
		t.Fatalf("keep-alive not reused: %d dials", n)
	}
	var apiErr *APIError
	if err := client.Shutdown(ctx, false); !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Message != "use systemctl --user stop webmux" {
		t.Fatalf("refused shutdown: %v", err)
	}
	if err := client.Shutdown(ctx, true); err != nil {
		t.Fatalf("forced shutdown: %v", err)
	}
	if token, err := client.Token(ctx); token != "" || err != nil {
		t.Fatalf("absent token: %q %v", token, err)
	}
	conn, err := client.DialWebSocket(ctx, "/ws/echo")
	if err != nil {
		t.Fatal(err)
	}
	if conn.Subprotocol() != WSProtocol {
		t.Fatalf("subprotocol %q", conn.Subprotocol())
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	if _, data, err := conn.Read(ctx); err != nil || string(data) != "ping" {
		t.Fatalf("echo %q %v", data, err)
	}
	_ = conn.CloseNow()
}

func TestUnixDialerReportsNotRunning(t *testing.T) {
	// A short path: macOS rejects long socket paths with EINVAL, not ENOENT.
	dir, err := os.MkdirTemp("/tmp", "lc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	client := New(UnixDialer{Path: filepath.Join(dir, "missing.sock")})
	if _, err := client.Status(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("missing socket: %v", err)
	}
	if _, err := New(UnixDialer{}).Status(context.Background()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("empty path: %v", err)
	}
}
