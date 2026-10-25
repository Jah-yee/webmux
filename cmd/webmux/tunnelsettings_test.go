package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/tunnelsettings"
)

func settingsController(t *testing.T, cfg config.TunnelConfig, locate func(context.Context) (string, error)) (*tunnelController, string) {
	t.Helper()
	dir := t.TempDir()
	store := tunnelsettings.Open(dir, cfg)
	t.Cleanup(store.Close)
	tc := newTunnelController(cfg, store, "http://127.0.0.1:1", tunnelTestToken, &api.PublicOrigin{}, locate, io.Discard, slog.New(slog.DiscardHandler))
	t.Cleanup(tc.Close)
	return tc, dir
}

func TestTunnelApplyWaitsForOldWorkerAndRejectsStaleEvents(t *testing.T) {
	entered, canceled, release, second := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	tc, _ := settingsController(t, config.TunnelConfig{}, func(ctx context.Context) (string, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release
		} else {
			close(second)
			<-ctx.Done()
		}
		return "", ctx.Err()
	})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	if _, err := tc.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	<-entered
	tc.mu.Lock()
	oldGeneration := tc.gen
	tc.mu.Unlock()
	credential := "named-private"
	view, err := tc.UpdateSettings(contract.TunnelSettingsUpdate{Mode: "named", Hostname: "new.example.com", Token: &credential})
	if err != nil || view.State != contract.TunnelStarting || !view.Enabled {
		t.Fatalf("update %+v %v", view, err)
	}
	<-canceled
	select {
	case <-second:
		t.Fatal("replacement started before canceled worker finished")
	default:
	}
	if tc.ready(oldGeneration, "https://old.example.com") {
		t.Fatal("stale generation reauthorized origin")
	}
	tc.fail(oldGeneration, errors.New("stale failure"), credential)
	if tc.Settings().State != contract.TunnelStarting {
		t.Fatal("stale failure replaced current state")
	}
	once.Do(func() { close(release) })
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("replacement did not start after old worker finished")
	}
	if _, err := tc.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if tc.Settings().State != contract.TunnelOff {
		t.Fatal("disable not immediately visible")
	}
}

func TestTunnelFailedPersistenceStopsButOtherUpdatesDoNotApply(t *testing.T) {
	tc, dir := settingsController(t, config.TunnelConfig{}, func(ctx context.Context) (string, error) { <-ctx.Done(); return "", ctx.Err() })
	if _, err := tc.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	tc.mu.Lock()
	generation := tc.gen
	tc.mu.Unlock()
	if !tc.ready(generation, "https://old.example.com") {
		t.Fatal("could not authorize test origin")
	}
	path := filepath.Join(dir, "tunnel.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	credential := "named-private"
	if _, err := tc.UpdateSettings(contract.TunnelSettingsUpdate{Mode: "named", Hostname: "new.example.com", Token: &credential}); !errors.Is(err, tunnelsettings.ErrUnavailable) {
		t.Fatalf("save error %v", err)
	}
	if view := tc.Settings(); view.State != contract.TunnelReady || view.Mode != "quick" || !view.Enabled {
		t.Fatalf("failed save applied %+v", view)
	}
	status, err := tc.SetEnabled(false)
	if !errors.Is(err, tunnelsettings.ErrNotPersisted) || status.State != contract.TunnelOff || status.Enabled == nil || *status.Enabled {
		t.Fatalf("emergency stop %+v %v", status, err)
	}
	if tc.ready(generation, "https://old.example.com") {
		t.Fatal("emergency stop did not invalidate generation")
	}
	view := tc.Settings()
	if view.PublicURL != "" || view.StorageAvailable || view.Enabled || view.CanEnable {
		t.Fatalf("fault view %+v", view)
	}
	if _, err := tc.SetEnabled(true); !errors.Is(err, tunnelsettings.ErrUnavailable) {
		t.Fatal("storage fault permitted enable")
	}
}

func TestTunnelConcurrentUpdatesAndReset(t *testing.T) {
	defaults := config.TunnelConfig{Enabled: true, Token: "toml-private", Hostname: "toml.example.com"}
	tc, _ := settingsController(t, defaults, func(context.Context) (string, error) { return "", errors.New("not installed") })
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			credential := fmt.Sprintf("private-%d", i)
			_, err := tc.UpdateSettings(contract.TunnelSettingsUpdate{Mode: "named", Hostname: fmt.Sprintf("host%d.example.com", i), Token: &credential})
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	view := tc.Settings()
	cfg, web := tc.store.Effective()
	if !web || view.Hostname != cfg.Hostname || !view.TokenConfigured {
		t.Fatal("concurrent saves diverged from effective config")
	}
	reset, err := tc.ResetSettings()
	if err != nil || reset.Enabled || reset.State != contract.TunnelOff || reset.Source != "config" || reset.Hostname != defaults.Hostname {
		t.Fatalf("reset %+v %v", reset, err)
	}
	cfg, web = tc.store.Effective()
	if web || !cfg.Enabled {
		t.Fatal("reset lost next-start TOML desire")
	}
}

func writeTunnelStartupConfig(t *testing.T, state string, enabled bool, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	body := fmt.Sprintf("listen = '127.0.0.1:0'\nstate_dir = %q\ntoken = %q\n[tunnel]\nenabled = %v\ncloudflared = '/definitely/missing/cloudflared'\n", state, token, enabled)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTunnelStartupWebPrecedenceFlagsAndShortToken(t *testing.T) {
	for _, row := range []struct {
		name                            string
		toml, saved                     bool
		flag                            string
		token                           string
		wantEnabled, wantWeb, wantError bool
	}{
		{"saved off overrides TOML", true, false, "", tunnelTestToken, false, true, false},
		{"saved on restores", false, true, "", tunnelTestToken, true, true, false},
		{"short web token soft failure", false, true, "", "short", true, true, false},
		{"empty web token generated", false, true, "", "", true, true, false},
		{"explicit enable overrides saved off", false, false, "--tunnel", tunnelTestToken, true, false, false},
		{"explicit disable overrides saved on", true, true, "--tunnel=false", tunnelTestToken, false, false, false},
		{"explicit enable keeps legacy token rejection", false, false, "--tunnel", "short", false, false, true},
	} {
		t.Run(row.name, func(t *testing.T) {
			state := t.TempDir()
			store := tunnelsettings.Open(state, config.TunnelConfig{})
			if err := store.Save(config.TunnelConfig{Enabled: row.saved}); err != nil {
				t.Fatal(err)
			}
			store.Close()
			before, _ := os.ReadFile(filepath.Join(state, "tunnel.json"))
			args := []string{"--config", writeTunnelStartupConfig(t, state, row.toml, row.token)}
			if row.flag != "" {
				args = append(args, row.flag)
			}
			c, err := loadConfig(args)
			if (err != nil) != row.wantError {
				t.Fatalf("load error %v", err)
			}
			if err == nil {
				defer c.TunnelSettings.Close()
				if c.Tunnel.Enabled != row.wantEnabled || c.TunnelWebManaged != row.wantWeb {
					t.Fatalf("startup flags enabled=%v web=%v", c.Tunnel.Enabled, c.TunnelWebManaged)
				}
				if row.token == "" && len(c.Token) != minTunnelTokenLength {
					t.Fatal("web startup did not generate login token")
				}
				tc := newTunnelController(c.Tunnel, c.TunnelSettings, "http://127.0.0.1:1", c.Token, &api.PublicOrigin{}, func(context.Context) (string, error) { return "", errors.New("missing") }, io.Discard, slog.New(slog.DiscardHandler))
				if *tc.Status().Enabled != row.wantEnabled {
					t.Fatal("tray desired enabled ignored process override")
				}
				tc.Close()
			}
			after, _ := os.ReadFile(filepath.Join(state, "tunnel.json"))
			if string(before) != string(after) {
				t.Fatal("startup changed persisted settings")
			}
		})
	}
}

func startLoadedTunnel(t *testing.T, c serverRun) *tunnelRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &tunnelRun{state: c.StateDir, out: &syncBuffer{}, cancel: cancel, result: make(chan error, 1)}
	go func() { r.result <- serve(ctx, c, r.out) }()
	t.Cleanup(func() {
		cancel()
		if err := <-r.result; err != nil {
			t.Error(err)
		}
	})
	eventually(t, "local listener", func() bool { return strings.Contains(r.out.String(), "WebMux local:") })
	matches := regexp.MustCompile(`WebMux local:  (http://127\.0\.0\.1:\d+)`).FindStringSubmatch(r.out.String())
	if len(matches) != 2 {
		t.Fatal("local listener URL absent")
	}
	r.local = matches[1]
	return r
}

func TestTunnelStorageFaultStartupKeepsHTTPAndExposureOff(t *testing.T) {
	for _, fault := range []string{"corrupt", "unreadable", "locked"} {
		t.Run(fault, func(t *testing.T) {
			state, err := os.MkdirTemp("/tmp", "wmxt")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(state) })
			path := filepath.Join(state, "tunnel.json")
			switch fault {
			case "corrupt":
				if err := os.WriteFile(path, []byte("broken-secret"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "locked":
				locked := tunnelsettings.Open(state, config.TunnelConfig{})
				t.Cleanup(locked.Close)
			}
			c, err := loadConfig([]string{"--config", writeTunnelStartupConfig(t, state, true, tunnelTestToken), "--tunnel"})
			if err != nil {
				t.Fatal(err)
			}
			if c.Tunnel.Enabled {
				t.Fatal("storage fault restored exposure from TOML/flag")
			}
			r := startLoadedTunnel(t, c)
			if got := r.tunnel(t); got.State != contract.TunnelOff || *got.Enabled {
				t.Fatalf("fault status %+v", got)
			}
			status, raw := r.tunnelRequest(t, "GET", "/api/tunnel", "", c.Token)
			var view contract.TunnelSettings
			if err := json.Unmarshal(raw, &view); err != nil {
				t.Fatal(err)
			}
			if status != 200 || view.StorageAvailable || view.State != contract.TunnelOff {
				t.Fatalf("readable fault status %d %+v", status, view)
			}
			status, raw = r.tunnelRequest(t, "PUT", "/api/tunnel/enabled", `{"enabled":false}`, c.Token)
			if status != 503 || !strings.Contains(string(raw), `"notPersisted":true`) {
				t.Fatalf("fault disable %d %s", status, raw)
			}
			if r.status(t, strings.TrimPrefix(r.local, "http://")) != 200 {
				t.Fatal("fault killed local HTTP")
			}
		})
	}
}

func (r *tunnelRun) tunnelRequest(t *testing.T, method, path, body, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, r.local+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 5 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := res.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw
}

func TestWebManagedStartupFailureKeepsHTTP(t *testing.T) {
	for _, token := range []string{"short", tunnelTestToken} {
		t.Run(fmt.Sprintf("token-length-%d", len(token)), func(t *testing.T) {
			state, err := os.MkdirTemp("/tmp", "wmxt")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(state) })
			store := tunnelsettings.Open(state, config.TunnelConfig{})
			if err := store.Save(config.TunnelConfig{Enabled: true}); err != nil {
				t.Fatal(err)
			}
			store.Close()
			c, err := loadConfig([]string{"--config", writeTunnelStartupConfig(t, state, false, token)})
			if err != nil {
				t.Fatal(err)
			}
			r := startLoadedTunnel(t, c)
			failed := r.waitTunnel(t, contract.TunnelFailed)
			if failed.Enabled == nil || !*failed.Enabled {
				t.Fatal("soft failure lost desired enable")
			}
			status, _ := r.tunnelRequest(t, "GET", "/api/health", "", token)
			if status != 200 {
				t.Fatal("soft failure stopped HTTP")
			}
		})
	}
}
