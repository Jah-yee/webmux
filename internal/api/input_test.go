package api

import (
	"bytes"
	"context"
	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDisconnectDuringBlockedInputDetachesClient(t *testing.T) {
	c := config.Config{Presets: []config.Preset{{Name: "blocked", Cmd: []string{"/bin/bash", "-c", "stty raw -echo; printf READY; sleep 30"}}}}
	m := session.NewManager(session.Options{DefaultCwd: t.TempDir(), Presets: testPresets(c.Presets)})
	server := httptest.NewServer(New(testDeps(t, Deps{Config: c, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())})))
	defer server.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	s, err := m.Create("", "blocked", nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/s/"+s.Info().ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	for {
		_, p, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if (p[0] == 0 || p[0] == 1) && bytes.Contains(p[1:], []byte("READY")) {
			break
		}
	}
	if err = conn.Write(ctx, websocket.MessageBinary, contract.Frame(0, bytes.Repeat([]byte("x"), 512*1024))); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = conn.CloseNow()
	until := time.Now().Add(2 * time.Second)
	for s.Info().Clients != 0 {
		if time.Now().After(until) {
			t.Fatal("disconnected input retained subscriber")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.Info().Status == session.StatusExited {
		t.Fatal("disconnect killed persistent session")
	}
}
