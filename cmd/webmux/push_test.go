package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rainchan/webmux/internal/contract"
)

// subscribe registers a fake browser subscription over TCP from the
// loopback origin, the way a desktop browser would.
func subscribe(t *testing.T, listen, endpoint string) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"endpoint": endpoint, "keys": map[string]string{
		"p256dh": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		"auth":   base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}})
	req, _ := http.NewRequest(http.MethodPost, "http://"+listen+"/api/push/subscriptions", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+fixtureToken)
	req.Header.Set("Origin", "http://"+listen)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe: %d", resp.StatusCode)
	}
}

func TestStatusReportsPush(t *testing.T) {
	t.Parallel()
	const endpoint = "https://push.example/SECRET-ENDPOINT"
	for _, row := range []struct {
		name, extra string
		want        contract.PushStatus
	}{
		{"named tunnel", "[tunnel]\ntoken = 'tunnel-cred'\nhostname = 'term.example.com'\n",
			contract.PushStatus{Enabled: true, Available: true, Subscriptions: 1, PublicOrigin: "https://term.example.com"}},
		{"no named tunnel", "", contract.PushStatus{Enabled: true, Available: true, Subscriptions: 1}},
		{"disabled", "[push]\nenabled = false\n", contract.PushStatus{}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t, row.extra)
			stdout, _, _ := f.run(t, "status", "--json")
			if strings.Contains(stdout, `"push"`) {
				t.Fatalf("stopped instance reports push: %s", stdout)
			}
			srv := f.start(t, nil, true, "serve", "--config", f.config)
			if row.want.Enabled {
				subscribe(t, f.record(t).Listen, endpoint)
			}
			stdout, _, code := f.run(t, "status", "--json")
			status := decodeStatus(t, stdout)
			if code != 0 || status.Push == nil || *status.Push != row.want {
				t.Fatalf("push status %d %+v", code, status.Push)
			}
			secrets := []string{"SECRET-ENDPOINT", fixtureToken, "tunnel-cred", "p256dh"}
			stored, err := os.ReadFile(filepath.Join(f.state, "push.json"))
			if row.want.Enabled != (err == nil) || !row.want.Enabled && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("push.json presence: %v", err)
			}
			if err == nil {
				var file struct {
					VAPID struct{ Private string } `json:"vapid"`
				}
				if json.Unmarshal(stored, &file) != nil || file.VAPID.Private == "" {
					t.Fatalf("push.json %s", stored)
				}
				secrets = append(secrets, file.VAPID.Private)
			}
			for _, secret := range secrets {
				if strings.Contains(stdout, secret) {
					t.Fatalf("status leaks %q: %s", secret, stdout)
				}
			}
			if _, _, code := f.run(t, "stop"); code != 0 {
				t.Fatalf("stop exit %d", code)
			}
			srv.waitExit(t)
		})
	}
}
