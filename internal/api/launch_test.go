package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/session"
)

type launchHarness struct {
	t       *testing.T
	handler http.Handler
	manager *session.Manager
	store   *launch.Service
	cwd     string
}

func newLaunchHarness(t *testing.T, store *launch.Service) *launchHarness {
	t.Helper()

	cwd := t.TempDir()
	m := session.NewManager(session.Options{DefaultCwd: cwd})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	c := config.Config{Token: "secret", DefaultCwd: cwd}
	return &launchHarness{t: t, handler: New(testDeps(t, Deps{Config: c, Manager: m, Launch: store})), manager: m, store: store, cwd: cwd}
}

func openLaunchStore(t *testing.T) *launch.Service {
	t.Helper()
	s := launch.Open(t.TempDir())
	t.Cleanup(s.Close)
	return s
}

func (h *launchHarness) do(method, path, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	r := httptest.NewRequest(method, "http://webmux.example"+path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, r)
	return w
}

func (h *launchHarness) expect(method, path, body string, status int) []byte {
	h.t.Helper()
	w := h.do(method, path, body)
	if w.Code != status {
		h.t.Fatalf("%s %s %s: status %d want %d: %s", method, path, body, w.Code, status, w.Body.String())
	}
	if status >= 400 {
		var response map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response["error"] == "" {
			h.t.Fatalf("error body %q: %v", w.Body.String(), err)
		}
	}
	return w.Body.Bytes()
}

func (h *launchHarness) create(body string) contract.LaunchConfig {
	h.t.Helper()
	var c contract.LaunchConfig
	if err := json.Unmarshal(h.expect("POST", "/api/launch-configs", body, 201), &c); err != nil {
		h.t.Fatal(err)
	}
	return c
}

func TestLaunchConfigCRUD(t *testing.T) {
	h := newLaunchHarness(t, openLaunchStore(t))
	if got := string(h.expect("GET", "/api/launch-configs", "", 200)); strings.TrimSpace(got) != "[]" {
		t.Fatalf("empty list %q", got)
	}
	a := h.create(`{"name":" Claude 恢复 ","icon":"claudecode","cmd":["claude","--resume",""],"env":{"API_KEY":"secret-value","B":"other-secret"}}`)
	if len(a.ID) != 8 || a.Name != "Claude 恢复" || !reflect.DeepEqual(a.Cmd, []string{"claude", "--resume", ""}) || a.Env["API_KEY"] != "secret-value" {
		t.Fatalf("created %+v", a)
	}
	b := h.create(`{"name":"shell","cmd":["bash"]}`)
	if a.Icon != "claudecode" || b.Icon != "" {
		t.Fatalf("created icons %q %q", a.Icon, b.Icon)
	}
	raw := h.expect("GET", "/api/launch-configs", "", 200)
	if strings.Contains(string(raw), "secret-value") || strings.Contains(string(raw), "other-secret") || strings.Contains(string(raw), `"env"`) {
		t.Fatalf("list leaks env values: %s", raw)
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0]["id"] != a.ID || list[1]["id"] != b.ID {
		t.Fatalf("list order %s", raw)
	}
	if list[0]["icon"] != "claudecode" {
		t.Fatalf("list lacks icon: %s", raw)
	}
	if !reflect.DeepEqual(list[0]["envNames"], []any{"API_KEY", "B"}) || !reflect.DeepEqual(list[1]["envNames"], []any{}) {
		t.Fatalf("envNames %s", raw)
	}
	for _, field := range []string{"createdAt", "updatedAt"} {
		value, _ := list[0][field].(string)
		if parsed, err := time.Parse(time.RFC3339Nano, value); err != nil || !strings.HasSuffix(value, "Z") || parsed.IsZero() {
			t.Fatalf("%s %q: %v", field, value, err)
		}
	}
	raw = h.expect("GET", "/api/launch-configs/"+a.ID, "", 200)
	if !strings.Contains(string(raw), "secret-value") {
		t.Fatalf("single config lacks env: %s", raw)
	}
	raw = h.expect("GET", "/api/launch-configs/"+b.ID, "", 200)
	if !strings.Contains(string(raw), `"env":{}`) {
		t.Fatalf("empty env must be {}: %s", raw)
	}
	var updated contract.LaunchConfig
	if err := json.Unmarshal(h.expect("PUT", "/api/launch-configs/"+a.ID, `{"name":"Claude","cmd":["claude"]}`, 200), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != a.ID || !updated.CreatedAt.Equal(a.CreatedAt) || updated.UpdatedAt.Before(a.UpdatedAt) || len(updated.Env) != 0 || updated.Name != "Claude" {
		t.Fatalf("whole update %+v", updated)
	}
	if updated.Icon != "" {
		t.Fatalf("whole update did not clear icon: %q", updated.Icon)
	}
	h.expect("POST", "/api/launch-configs", `{"name":"Claude ","cmd":["x"]}`, 409)
	h.expect("PUT", "/api/launch-configs/"+b.ID, `{"name":"Claude","cmd":["x"]}`, 409)
	for _, body := range []string{
		`{"name":"","cmd":["x"]}`,
		`{"name":"x"}`,
		`{"name":"x","cmd":[]}`,
		`{"name":"x","cmd":[""]}`,
		`{"name":"x","cmd":["x"],"env":{"WEBMUX_SESSION_ID":"forged"}}`,
		`{"name":"x","cmd":["x"],"env":{"1BAD":"v"}}`,
		`{"name":"x","cmd":["x"],"id":"abcdefgh"}`,
		`{"name":"x","cmd":["x"],"icon":"https://example.com/icon.svg"}`,
		`{"name":"x","cmd":["x"]}{}`,
		`not json`,
	} {
		h.expect("POST", "/api/launch-configs", body, 400)
		h.expect("PUT", "/api/launch-configs/"+b.ID, body, 400)
	}
	h.expect("GET", "/api/launch-configs/zzzzzzzz", "", 404)
	h.expect("PUT", "/api/launch-configs/zzzzzzzz", `{"name":"y","cmd":["y"]}`, 404)
	h.expect("DELETE", "/api/launch-configs/zzzzzzzz", "", 404)
	h.expect("DELETE", "/api/launch-configs/"+a.ID, "", 204)
	h.expect("GET", "/api/launch-configs/"+a.ID, "", 404)
	if err := json.Unmarshal(h.expect("GET", "/api/launch-configs", "", 200), &list); err != nil || len(list) != 1 || list[0]["id"] != b.ID {
		t.Fatalf("after delete %v %v", list, err)
	}
}

func TestLaunchConfigAuthAndOrigin(t *testing.T) {
	h := newLaunchHarness(t, openLaunchStore(t))
	for _, row := range []struct {
		method, path, body, authorization, origin string
		want                                      int
	}{
		{"GET", "/api/launch-configs", "", "", "", 401},
		{"GET", "/api/launch-configs", "", "Bearer wrong", "", 401},
		{"POST", "/api/launch-configs", `{"name":"x","cmd":["x"]}`, "", "", 401},
		{"POST", "/api/launch-configs", `{"name":"x","cmd":["x"]}`, "Bearer secret", "https://evil.example", 403},
		{"GET", "/api/launch-configs", "", "Bearer secret", "https://evil.example", 403},
		{"GET", "/api/launch-configs", "", "Bearer secret", "http://webmux.example", 200},
	} {
		r := httptest.NewRequest(row.method, "http://webmux.example"+row.path, strings.NewReader(row.body))
		r.Header.Set("Authorization", row.authorization)
		r.Header.Set("Origin", row.origin)
		w := httptest.NewRecorder()
		h.handler.ServeHTTP(w, r)
		if w.Code != row.want {
			t.Fatalf("%+v: status %d", row, w.Code)
		}
	}
	if list, _ := h.store.List(); len(list) != 0 {
		t.Fatalf("rejected request created a config: %+v", list)
	}
}

func TestLaunchConfigUnavailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "launch-configs.json")
	corrupt := []byte(`{"v":1,"configs":[`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	corruptStore := launch.Open(dir)
	t.Cleanup(corruptStore.Close)
	lockDir := t.TempDir()
	owner := launch.Open(lockDir)
	t.Cleanup(owner.Close)
	locked := launch.Open(lockDir)
	t.Cleanup(locked.Close)
	for name, row := range map[string]struct {
		store *launch.Service
		want  string
	}{
		"corrupt": {corruptStore, path},
		"locked":  {locked, filepath.Join(lockDir, "launch-configs.json")},
		"nil":     {nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			h := newLaunchHarness(t, row.store)
			for _, req := range [][2]string{
				{"GET", "/api/launch-configs"},
				{"GET", "/api/launch-configs/abcdefgh"},
				{"POST", "/api/launch-configs"},
				{"PUT", "/api/launch-configs/abcdefgh"},
				{"DELETE", "/api/launch-configs/abcdefgh"},
			} {
				body := ""
				if req[0] == "POST" || req[0] == "PUT" {
					body = `{"name":"x","cmd":["x"]}`
				}
				raw := h.expect(req[0], req[1], body, 503)
				if !strings.Contains(string(raw), "启动配置不可用") || (row.want != "" && !strings.Contains(string(raw), row.want)) {
					t.Fatalf("%v: %s", req, raw)
				}
			}
			h.expect("POST", "/api/sessions", `{"configId":"abcdefgh","cwd":"/definitely/missing"}`, 503)
			h.expect("POST", "/api/sessions", `{"cmd":["/bin/echo","fixed"]}`, 201)
		})
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(corrupt) {
		t.Fatalf("corrupt file changed: %q %v", got, err)
	}
}

func TestLaunchConfigSaveFailureReturns500(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	store := launch.Open(dir)
	t.Cleanup(store.Close)
	h := newLaunchHarness(t, store)
	a := h.create(`{"name":"a","cmd":["a"]}`)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	h.expect("POST", "/api/launch-configs", `{"name":"b","cmd":["b"],"env":{"K":"secret-value"}}`, 500)
	h.expect("PUT", "/api/launch-configs/"+a.ID, `{"name":"renamed","cmd":["a"]}`, 500)
	h.expect("DELETE", "/api/launch-configs/"+a.ID, "", 500)
	var list []launch.Summary
	if err := json.Unmarshal(h.expect("GET", "/api/launch-configs", "", 200), &list); err != nil || len(list) != 1 || list[0].Name != "a" {
		t.Fatalf("rollback %+v %v", list, err)
	}
}

// waitExit waits for the session to exit and returns its PTY output.
func waitExit(t *testing.T, item *session.Session) string {
	t.Helper()
	select {
	case <-item.Done():
	case <-time.After(5 * time.Second):
		t.Fatalf("session did not exit: %q", item.Buffer())
	}
	return string(item.Buffer())
}

func TestSessionFromLaunchConfig(t *testing.T) {
	h := newLaunchHarness(t, openLaunchStore(t))
	script := `printf '<%s><%s><%s><%s>' "$FOO" "$#" "$1" "$2"`
	body, _ := json.Marshal(map[string]any{"name": "env test", "cmd": []string{"/bin/sh", "-c", script, "sh", "", "two words"}, "env": map[string]string{"FOO": "from config"}})
	cfg := h.create(string(body))
	var info contract.SessionInfo
	raw := h.expect("POST", "/api/sessions", `{"configId":"`+cfg.ID+`","cwd":"`+h.cwd+`","env":{},"cmd":[],"name":"From config"}`, 201)
	if err := json.Unmarshal(raw, &info); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(info.Cmd, cfg.Cmd) || info.Title != "From config" || info.Cwd != h.cwd {
		t.Fatalf("session info %+v", info)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["env"]; ok {
		t.Fatalf("session info carries env: %s", raw)
	}
	if _, ok := fields["configId"]; ok {
		t.Fatalf("session info carries configId: %s", raw)
	}
	item, err := h.manager.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitExit(t, item); !strings.Contains(got, "<from config><2><><two words>") {
		t.Fatalf("PTY output %q", got)
	}
}

func TestSessionFromLaunchConfigOutlivesConfig(t *testing.T) {
	h := newLaunchHarness(t, openLaunchStore(t))
	body, _ := json.Marshal(map[string]any{"name": "reader", "cmd": []string{"/bin/sh", "-c", `read line; printf 'got=%s:%s' "$line" "$FOO"`}, "env": map[string]string{"FOO": "kept"}})
	cfg := h.create(string(body))
	var info contract.SessionInfo
	if err := json.Unmarshal(h.expect("POST", "/api/sessions", `{"configId":"`+cfg.ID+`"}`, 201), &info); err != nil {
		t.Fatal(err)
	}
	h.expect("DELETE", "/api/launch-configs/"+cfg.ID, "", 204)
	item, err := h.manager.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Info().ExitCode != nil {
		t.Fatal("session exited after the config was deleted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := item.WriteContext(ctx, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if got := waitExit(t, item); !strings.Contains(got, "got=hello:kept") {
		t.Fatalf("PTY output %q", got)
	}
}

func TestSessionConfigIDValidation(t *testing.T) {
	h := newLaunchHarness(t, openLaunchStore(t))
	cfg := h.create(`{"name":"echo","cmd":["/bin/echo","hi"]}`)
	missing := filepath.Join(h.cwd, "missing")
	ref := `"configId":"` + cfg.ID + `","cwd":"` + missing + `"`
	for _, body := range []string{
		`{"configId":"","cwd":"` + missing + `"}`,
		`{` + ref + `,"cmd":["/bin/echo"]}`,
		`{` + ref + `,"env":{"FOO":"x"}}`,
		`{` + ref + `,"launchConfig":"x"}`,
	} {
		raw := h.expect("POST", "/api/sessions", body, 400)
		if strings.Contains(string(raw), "directory") {
			t.Fatalf("%s reached directory checks: %s", body, raw)
		}
	}
	h.expect("POST", "/api/sessions", `{"configId":"zzzzzzzz","cwd":"`+missing+`"}`, 404)
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("directory touched: %v", err)
	}
	if got := h.manager.List(); len(got) != 0 {
		t.Fatalf("rejected requests created sessions: %v", got)
	}
	// env: null and env: {} are not conflicts; the cwd error comes after.
	h.expect("POST", "/api/sessions", `{`+ref+`,"env":null}`, 400)
	h.expect("POST", "/api/sessions", `{"configId":"`+cfg.ID+`","env":null,"cmd":null}`, 201)
	// Direct commands retain their explicit environment.
	h.expect("POST", "/api/sessions", `{"cmd":["/bin/echo","fixed"]}`, 201)
	var info contract.SessionInfo
	if err := json.Unmarshal(h.expect("POST", "/api/sessions", `{"cmd":["/bin/sh","-c","printf '<%s>' \"$BAR\""],"env":{"BAR":"direct"}}`, 201), &info); err != nil {
		t.Fatal(err)
	}
	item, err := h.manager.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitExit(t, item); !strings.Contains(got, "<direct>") {
		t.Fatalf("direct env output %q", got)
	}
	h.expect("POST", "/api/sessions", `{"cmd":["/bin/echo"],"env":{"WEBMUX_SESSION_ID":"forged"}}`, 400)
}
