//go:build linux || darwin

package tray

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

// stoppedRunner answers `webmux status --json` like an installed webmux
// with no server running.
type stoppedRunner struct{}

func (stoppedRunner) Run(_ context.Context, argv, _ []string) (Result, error) {
	out, _ := json.Marshal(contract.StatusV1{V: 1, State: contract.StateStopped, Config: contract.ConfigRef{Path: "/home/u/.config/webmux/config.toml"}})
	return Result{Stdout: out, Code: contract.ExitNotRunning}, nil
}
func (stoppedRunner) Start([]string, []string) (Process, error) { return nil, nil }

func TestWSLServiceStatusAndWatch(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "wst")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	socket := filepath.Join(dir, "webmux.sock")
	service := &WSLService{Runner: stoppedRunner{}, Dial: func(Target) localclient.Dialer { return localclient.UnixDialer{Path: socket} }}
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, err := service.Status(ctx)
	if err != nil || status.State != contract.StateStopped {
		t.Fatalf("fallback status %+v %v", status, err)
	}

	m := session.NewManager(session.Options{})
	defer m.Shutdown(context.Background())
	ws := workspace.New(dir, m, dir)
	defer ws.Close()
	c := config.Config{DefaultCwd: dir}
	handler := api.NewLocal(c, m, ws, nil, api.Local{Status: func() contract.StatusV1 {
		return contract.StatusV1{V: 1, State: contract.StateRunning, LocalAPI: true, Record: true}
	}})
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(l) }()
	defer func() { _ = server.Close() }()

	if status, err := service.Status(ctx); err != nil || !status.LocalAPI {
		t.Fatalf("local status %+v %v", status, err)
	}
	counts := make(chan int, 16)
	watchCtx, stopWatch := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- service.Watch(watchCtx, func(n int) { counts <- n }) }()
	expect := func(want int) {
		t.Helper()
		for {
			select {
			case n := <-counts:
				if n == want {
					return
				}
			case <-ctx.Done():
				t.Fatalf("session count never reached %d", want)
			}
		}
	}
	expect(0)
	first, err := m.Create(dir, "custom", []string{"cat"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(dir, "custom", []string{"cat"}, nil, ""); err != nil {
		t.Fatal(err)
	}
	expect(2)
	if err := m.Close(first.Info().ID); err != nil {
		t.Fatal(err)
	}
	for m.Remove(first.Info().ID) != nil {
		if ctx.Err() != nil {
			t.Fatal("session never exited")
		}
		time.Sleep(10 * time.Millisecond)
	}
	expect(1)
	stopWatch()
	<-done
}
