package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/runtime"
)

const publicOrigin = "https://" + publicHost

// client talks to the in-process server's local API socket.
func (r *tunnelRun) client(t *testing.T) *localclient.Client {
	t.Helper()
	record, err := runtime.Read(runtime.Dir(r.state))
	if err != nil {
		t.Fatal(err)
	}
	if record.Socket == "" {
		t.Fatal("local API socket not recorded")
	}
	c := localclient.New(localclient.UnixDialer{Path: record.Socket})
	t.Cleanup(c.Close)
	return c
}

func (r *tunnelRun) setTunnel(t *testing.T, enabled bool) contract.TunnelStatus {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	status, err := r.client(t).SetTunnel(ctx, enabled)
	if err != nil {
		t.Fatalf("set tunnel %v: %v", enabled, err)
	}
	return status
}

// tunnel is the tunnel field of /local/v1/status; the encoded status must
// never carry the token.
func (r *tunnelRun) tunnel(t *testing.T) contract.TunnelStatus {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := r.client(t).Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Tunnel == nil {
		t.Fatal("status of a running server lacks the tunnel field")
	}
	encoded, _ := json.Marshal(s)
	if strings.Contains(string(encoded), tunnelTestToken) {
		t.Fatalf("status leaks the token: %s", encoded)
	}
	return *s.Tunnel
}

func (r *tunnelRun) waitTunnel(t *testing.T, state string) contract.TunnelStatus {
	t.Helper()
	var last contract.TunnelStatus
	eventually(t, "tunnel "+state, func() bool {
		last = r.tunnel(t)
		return last.State == state
	})
	return last
}

// configTunnel reads the browser-facing tunnel state from GET /api/config.
// The response must never carry the WebMux or Cloudflare token.
func (r *tunnelRun) configTunnel(t *testing.T) contract.TunnelInfo {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, r.local+"/api/config", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tunnelTestToken)
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("config status %d: %s", res.StatusCode, raw)
	}
	if strings.Contains(string(raw), tunnelTestToken) {
		t.Fatalf("config leaks the token: %s", raw)
	}
	var body struct {
		Tunnel contract.TunnelInfo `json:"tunnel"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	return body.Tunnel
}

// pids lists every fake cloudflared started so far.
func (r *tunnelRun) pids(t *testing.T) []int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.dir, "pids"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, field := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		pids = append(pids, pid)
	}
	return pids
}

func processGone(pid int) bool {
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

func TestRuntimeTunnelStartStop(t *testing.T) {
	r := startServe(t, false, tunnelTestToken)
	localHost := strings.TrimPrefix(r.local, "http://")
	if got := r.tunnel(t); got != (contract.TunnelStatus{State: contract.TunnelOff}) {
		t.Fatalf("initial tunnel %+v", got)
	}
	if got := r.configTunnel(t); got != (contract.TunnelInfo{State: contract.TunnelOff}) {
		t.Fatalf("initial config tunnel %+v", got)
	}
	if got := r.status(t, publicHost); got != http.StatusForbidden {
		t.Fatalf("public host before any tunnel: %d", got)
	}
	if got := r.setTunnel(t, true); got.State != contract.TunnelStarting && got.State != contract.TunnelReady {
		t.Fatalf("start reply %+v", got)
	}
	if got := r.waitTunnel(t, contract.TunnelReady); got != (contract.TunnelStatus{State: contract.TunnelReady, PublicURL: publicOrigin}) {
		t.Fatalf("ready tunnel %+v", got)
	}
	if got := r.configTunnel(t); got != (contract.TunnelInfo{Enabled: true, State: contract.TunnelReady, PublicURL: publicOrigin}) {
		t.Fatalf("ready config tunnel %+v", got)
	}
	if got := r.status(t, publicHost); got != http.StatusOK {
		t.Fatalf("public request while ready: %d", got)
	}
	// A repeated start keeps the running generation: a new one would report
	// starting and launch a second cloudflared.
	if got := r.setTunnel(t, true); got.State != contract.TunnelReady || got.PublicURL != publicOrigin {
		t.Fatalf("repeated start %+v", got)
	}
	eventually(t, "runtime announcement", func() bool {
		return strings.Contains(r.out.String(), "WebMux public: "+publicOrigin+"\n")
	})
	if strings.Contains(r.out.String(), "#token=") {
		t.Fatalf("runtime start printed the token: %q", r.out.String())
	}
	if got := r.setTunnel(t, false); got.State != contract.TunnelOff {
		t.Fatalf("stop reply %+v", got)
	}
	if got := r.configTunnel(t); got != (contract.TunnelInfo{State: contract.TunnelOff}) {
		t.Fatalf("stopped config tunnel %+v", got)
	}
	if got := r.status(t, publicHost); got != http.StatusForbidden {
		t.Fatalf("public request after stop: %d", got)
	}
	pids := r.pids(t)
	if len(pids) != 1 {
		t.Fatalf("cloudflared processes started: %v", pids)
	}
	if !processGone(pids[0]) {
		t.Fatalf("cloudflared %d still running after stop", pids[0])
	}
	if got := r.status(t, localHost); got != http.StatusOK {
		t.Fatalf("local request after stop: %d", got)
	}
	if got := r.setTunnel(t, false); got.State != contract.TunnelOff {
		t.Fatalf("repeated stop %+v", got)
	}
	if got := r.tunnel(t); got.State != contract.TunnelOff {
		t.Fatalf("tunnel after stop %+v", got)
	}
}

func TestRuntimeTunnelUnexpectedExitFails(t *testing.T) {
	r := startServe(t, false, tunnelTestToken)
	r.setTunnel(t, true)
	r.waitTunnel(t, contract.TunnelReady)
	exit := filepath.Join(r.dir, "exit")
	if err := os.WriteFile(exit, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	failed := r.waitTunnel(t, contract.TunnelFailed)
	if !strings.Contains(failed.Error, "cloudflared exited") || failed.PublicURL != "" {
		t.Fatalf("failed tunnel %+v", failed)
	}
	if got := r.status(t, publicHost); got != http.StatusForbidden {
		t.Fatalf("public request after cloudflared exit: %d", got)
	}
	if got := r.status(t, strings.TrimPrefix(r.local, "http://")); got != http.StatusOK {
		t.Fatalf("local request after cloudflared exit: %d", got)
	}
	if got := r.tunnel(t); got.State != contract.TunnelFailed || len(r.pids(t)) != 1 {
		t.Fatalf("failed tunnel retried: %+v pids %v", got, r.pids(t))
	}
	// The user may ask again once the cause is gone.
	if err := os.Remove(exit); err != nil {
		t.Fatal(err)
	}
	r.setTunnel(t, true)
	r.waitTunnel(t, contract.TunnelReady)
	if got := r.status(t, publicHost); got != http.StatusOK || len(r.pids(t)) != 2 {
		t.Fatalf("restart after failure: %d pids %v", got, r.pids(t))
	}
}

func TestRuntimeTunnelRequiresStrongToken(t *testing.T) {
	for _, token := range []string{"", "short-token"} {
		t.Run(strconv.Quote(token), func(t *testing.T) {
			r := startServe(t, false, token)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := r.client(t).SetTunnel(ctx, true)
			var apiErr *localclient.APIError
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || !strings.Contains(apiErr.Message, "webmux config init") {
				t.Fatalf("weak token start: %v", err)
			}
			if got := r.tunnel(t); got.State != contract.TunnelOff {
				t.Fatalf("tunnel after refusal %+v", got)
			}
			if pids := r.pids(t); pids != nil {
				t.Fatalf("cloudflared started despite refusal: %v", pids)
			}
		})
	}
}

func TestStartupTunnelStopThenStart(t *testing.T) {
	r := startTunnelServe(t)
	if got := r.waitTunnel(t, contract.TunnelReady); got.PublicURL != publicOrigin {
		t.Fatalf("startup tunnel %+v", got)
	}
	if got := r.setTunnel(t, false); got.State != contract.TunnelOff {
		t.Fatalf("stop reply %+v", got)
	}
	if got := r.status(t, publicHost); got != http.StatusForbidden {
		t.Fatalf("public request after stop: %d", got)
	}
	first := r.pids(t)
	if len(first) != 1 || !processGone(first[0]) {
		t.Fatalf("startup cloudflared not stopped: %v", first)
	}
	r.setTunnel(t, true)
	if got := r.waitTunnel(t, contract.TunnelReady); got.PublicURL != publicOrigin {
		t.Fatalf("restarted tunnel %+v", got)
	}
	if got := r.status(t, publicHost); got != http.StatusOK || len(r.pids(t)) != 2 {
		t.Fatalf("restart: %d pids %v", got, r.pids(t))
	}
	eventually(t, "runtime announcement", func() bool {
		return strings.Contains(r.out.String(), "WebMux public: "+publicOrigin+"\n")
	})
}

func TestServeShutdownStopsRuntimeTunnel(t *testing.T) {
	r := startServe(t, false, tunnelTestToken)
	r.setTunnel(t, true)
	r.waitTunnel(t, contract.TunnelReady)
	r.cancel()
	select {
	case err := <-r.result:
		if err != nil {
			t.Fatal(err)
		}
		r.result <- nil // for Cleanup
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not return")
	}
	if pids := r.pids(t); len(pids) != 1 || !processGone(pids[0]) {
		t.Fatalf("cloudflared not stopped with the server: %v", pids)
	}
}

func TestTunnelControllerStopCancelsLocate(t *testing.T) {
	public := &api.PublicOrigin{}
	entered := make(chan struct{})
	locate := func(ctx context.Context) (string, error) {
		close(entered)
		<-ctx.Done()
		return "", ctx.Err()
	}
	cfg := config.TunnelConfig{Token: "named-credential", Hostname: "webmux.example.com"}
	tc := newTunnelController(cfg, "http://127.0.0.1:1", tunnelTestToken, public, locate, io.Discard, slog.New(slog.DiscardHandler))
	status, err := tc.Start()
	if err != nil || status != (contract.TunnelStatus{State: contract.TunnelStarting, Named: true}) {
		t.Fatalf("start %+v %v", status, err)
	}
	<-entered
	tc.Stop() // returns only after the locating worker has exited
	if got := tc.Status(); got != (contract.TunnelStatus{State: contract.TunnelOff, Named: true}) {
		t.Fatalf("after stop %+v", got)
	}
	tc.Stop()
	tc.Close()
	if _, err := tc.Start(); err == nil {
		t.Fatal("start after close succeeded")
	}
	if got := tc.Status(); got.State != contract.TunnelOff {
		t.Fatalf("after close %+v", got)
	}
}
