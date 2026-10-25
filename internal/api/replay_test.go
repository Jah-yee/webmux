package api

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReplayRetainsNewestBytesAfterBufferWrap(t *testing.T) {
	presets := []session.Preset{{Name: "output", Cmd: []string{"/bin/bash", "-c", "printf abcde"}}}
	m := session.NewManager(session.Options{ScrollbackBytes: 4, DefaultCwd: t.TempDir(), Presets: presets})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer m.Shutdown(ctx)
	s, err := m.Create("", "output", nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-ctx.Done():
		t.Fatal("output process did not exit")
	}
	server := httptest.NewServer(New(testDeps(t, Deps{Config: config.Config{}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())})))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/s/"+s.Info().ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	if position := readPosition(ctx, t, conn, "reset"); position.Start != 1 || position.End != 5 || !position.Truncated {
		t.Fatalf("wrapped position: %+v", position)
	}
	kind, p, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || !bytes.Equal(p, []byte{1, 'b', 'c', 'd', 'e'}) {
		t.Fatalf("wrapped replay: kind=%v bytes=%v err=%v", kind, p, err)
	}
}

// readPosition consumes the control and position frames that precede every
// replay, in that order: observers need the grid before the bytes.
func readPosition(ctx context.Context, t *testing.T, conn *websocket.Conn, mode string) contract.StreamPosition {
	t.Helper()
	kind, p, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || len(p) == 0 || p[0] != 6 {
		t.Fatalf("control frame before replay: kind=%v bytes=%q err=%v", kind, p, err)
	}
	kind, p, err = conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || len(p) == 0 || p[0] != 4 {
		t.Fatalf("position frame: kind=%v bytes=%q err=%v", kind, p, err)
	}
	var position contract.StreamPosition
	if err = json.Unmarshal(p[1:], &position); err != nil || position.Mode != mode || position.Generation == "" {
		t.Fatalf("position %q: %+v %v", p, position, err)
	}
	return position
}
