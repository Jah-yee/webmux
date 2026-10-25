//go:build linux || darwin

package tray

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/files"
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
	ws := workspace.New(dir, m, nil, dir)
	defer ws.Close()
	c := config.Config{DefaultCwd: dir}
	handler := api.NewLocal(api.Deps{Config: c, Manager: m, Workspace: ws, Code: codeview.New(m), Files: files.New(dir)}, api.Local{Status: func() contract.StatusV1 {
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
	first, err := m.Create(dir, "custom", []string{"cat"}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(dir, "custom", []string{"cat"}, nil, "", nil); err != nil {
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

func TestWSLServiceCheck(t *testing.T) {
	const webmux = "/usr/bin/webmux"
	probe := Result{Stderr: []byte("  -log-file string\n"), Code: 1}
	for _, tc := range []struct {
		name   string
		runner scriptedRunner
		want   BuildInfo
		err    error
	}{
		{"version --json", scriptedRunner{webmux + " -h": probe, webmux + " version --json": {Stdout: []byte(`{"v":1,"version":"0.5.0","contract":2}` + "\n")}}, BuildInfo{"0.5.0", 2}, nil},
		{"text from a version without --json", scriptedRunner{webmux + " -h": probe, webmux + " version --json": {Stdout: []byte("webmux 0.2.0\n")}}, BuildInfo{"0.2.0", 1}, nil},
		{"no local API", scriptedRunner{webmux + " -h": {Stderr: []byte("  -listen string\n"), Code: 2}}, BuildInfo{}, ErrWebmuxTooOld},
		{"cannot execute", scriptedRunner{webmux + " -h": {Stderr: []byte("execvpe(/usr/bin/webmux) failed: No such file or directory"), Code: 1}}, BuildInfo{}, ErrWebmuxMissing},
	} {
		service := &WSLService{Runner: tc.runner, Target: Target{Webmux: webmux}}
		got, err := service.Check(context.Background())
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("%s: %v %v", tc.name, got, err)
		}
	}
}

// statusRunner answers `webmux status --json` with fixed output.
type statusRunner []byte

func (r statusRunner) Run(context.Context, []string, []string) (Result, error) {
	return Result{Stdout: r, Code: contract.ExitNotRunning}, nil
}
func (statusRunner) Start([]string, []string) (Process, error) { return nil, nil }

// A newer contract is recognized on both paths, keeping the version and
// local link so the supervisor can follow the instance by reachability.
func TestWSLServiceStatusRejectsUnsupportedContract(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "wst")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	socket := filepath.Join(dir, "webmux.sock")
	v2 := []byte(`{"v":2,"version":"0.5.0","state":{"name":"running"},"urls":{"local":"http://127.0.0.1:7690"}}`)
	service := &WSLService{Runner: statusRunner(v2), Dial: func(Target) localclient.Dialer { return localclient.UnixDialer{Path: socket} }}
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	want := ContractError{V: 2, Version: "0.5.0", Local: "http://127.0.0.1:7690"}
	check := func(path string) {
		t.Helper()
		_, err := service.Status(ctx)
		var got *ContractError
		if !errors.Is(err, ErrContractUnsupported) || !errors.As(err, &got) || *got != want {
			t.Fatalf("%s: %v", path, err)
		}
	}
	check("status --json")

	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(v2) })}
	go func() { _ = server.Serve(l) }()
	defer func() { _ = server.Close() }()
	check("local API")
}
