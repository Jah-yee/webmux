package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

func testWorkspace(t *testing.T, m *session.Manager, cwd string) *workspace.Service {
	t.Helper()
	service := workspace.New(t.TempDir(), m, nil, cwd)
	t.Cleanup(service.Close)
	return service
}
func workspaceAPI(t *testing.T) (http.Handler, *workspace.Service, *session.Manager, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("requires real Git")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "API test")
	t.Setenv("GIT_AUTHOR_EMAIL", "api@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "API test")
	t.Setenv("GIT_COMMITTER_EMAIL", "api@example.invalid")
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"commit", "--allow-empty", "-m", "initial"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Git fixture: %v %s", err, out)
		}
	}
	c := config.Config{DefaultCwd: root, Token: "secret"}
	m := session.NewManager(testSessionOptions(c))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	service := testWorkspace(t, m, root)
	return New(testDeps(t, Deps{Config: c, Manager: m, Workspace: service})), service, m, root
}
func workspaceRequest(t *testing.T, handler http.Handler, method, path string, body any, want int) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(data)))
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != want {
		t.Fatalf("%s %s: status %d want %d: %s", method, path, recorder.Code, want, recorder.Body.String())
	}
	return recorder
}
func TestWorkspaceAPIRoutesAndErrorBoundaries(t *testing.T) {
	handler, _, m, root := workspaceAPI(t)
	for _, status := range []int{401, 403} {
		request := httptest.NewRequest("POST", "/api/repositories", strings.NewReader(fmt.Sprintf(`{"path":%q}`, root)))
		if status == 403 {
			request.Header.Set("Authorization", "Bearer secret")
			request.Header.Set("Origin", "https://evil.example")
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != status {
			t.Fatalf("auth boundary: %d %s", recorder.Code, recorder.Body.String())
		}
	}
	empty := workspaceRequest(t, handler, "GET", "/api/repositories", nil, 200)
	if strings.TrimSpace(empty.Body.String()) != "[]" {
		t.Fatal("unauthorized registration had side effects")
	}
	register := workspaceRequest(t, handler, "POST", "/api/repositories", map[string]string{"path": root}, 201)
	var repository contract.Repository
	if err := json.Unmarshal(register.Body.Bytes(), &repository); err != nil {
		t.Fatal(err)
	}
	duplicate := workspaceRequest(t, handler, "POST", "/api/repositories", map[string]string{"path": root}, 200)
	var again contract.Repository
	if err := json.Unmarshal(duplicate.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if again.ID != repository.ID {
		t.Fatal("duplicate registration changed identity")
	}
	base := "/api/repositories/" + repository.ID
	workspaceRequest(t, handler, "GET", base+"/worktrees", nil, 200)
	workspaceRequest(t, handler, "GET", base+"/branches", nil, 200)
	workspaceRequest(t, handler, "GET", "/api/repositories/missing/worktrees", nil, 404)
	workspaceRequest(t, handler, "POST", base+"/worktrees", contract.CreateWorktreeRequest{Path: filepath.Join(t.TempDir(), "bad"), Mode: "new", Branch: "--force", Base: "HEAD"}, 400)
	created := workspaceRequest(t, handler, "POST", base+"/worktrees", contract.CreateWorktreeRequest{Path: filepath.Join(t.TempDir(), "linked"), Mode: "new", Branch: "linked", Base: "HEAD"}, 201)
	var tree contract.Worktree
	if err := json.Unmarshal(created.Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	ref := &contract.WorkspaceReference{RepositoryID: repository.ID, WorktreeID: tree.ID}
	workspaceRequest(t, handler, "POST", "/api/sessions", map[string]any{"cwd": root, "preset": "custom", "cmd": []string{"/bin/sh"}, "workspace": ref}, 400)
	started := workspaceRequest(t, handler, "POST", "/api/sessions", map[string]any{"preset": "custom", "cmd": []string{"/bin/sh", "-c", "printf READY; read value"}, "workspace": ref}, 201)
	var info contract.SessionInfo
	if err := json.Unmarshal(started.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Cwd != tree.Path || info.Workspace == nil || info.Workspace.WorktreeID != tree.ID {
		t.Fatalf("workspace JSON: %+v", info)
	}
	treeURL := base + "/worktrees/" + tree.ID
	pre := workspaceRequest(t, handler, "POST", treeURL+"/removal-preview", struct{}{}, 200)
	var preview contract.RemovalPreview
	if err := json.Unmarshal(pre.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Sessions) != 1 || !preview.ForceAllowed {
		t.Fatalf("preview JSON: %+v", preview)
	}
	workspaceRequest(t, handler, "POST", treeURL+"/remove", contract.RemoveWorktreeRequest{ConfirmationID: preview.ConfirmationID, Mode: "safe"}, 409)
	for _, status := range []int{401, 403} {
		request := httptest.NewRequest("POST", treeURL+"/remove", strings.NewReader(`{"mode":"force"}`))
		if status == 403 {
			request.Header.Set("Authorization", "Bearer secret")
			request.Header.Set("Origin", "https://evil.example")
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != status {
			t.Fatalf("remove auth boundary: %d", recorder.Code)
		}
	}
	item, err := m.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if item.Info().Status == session.StatusExited {
		t.Fatal("rejected operations stopped PTY")
	}
	workspaceRequest(t, handler, "DELETE", base, nil, 204)
	if item.Info().Workspace != nil || item.Info().Status == session.StatusExited {
		t.Fatal("unregister changed PTY lifecycle")
	}
	if _, err := os.Stat(tree.Path); err != nil {
		t.Fatal("unregister deleted tree", err)
	}
	legacy := workspaceRequest(t, handler, "POST", "/api/sessions", map[string]any{"cwd": root, "preset": "custom", "cmd": []string{"/bin/sh", "-c", "printf LEGACY"}}, 201)
	if !strings.Contains(legacy.Body.String(), `"workspace":null`) {
		t.Fatal("legacy JSON missing explicit null workspace")
	}
}
func TestWorkspaceAPIConfigDegradationAndTimeout(t *testing.T) {
	cwd := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	m := session.NewManager(session.Options{DefaultCwd: cwd})
	defer m.Shutdown(context.Background())
	state := filepath.Join(t.TempDir(), "state-file")
	if err := os.WriteFile(state, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	service := workspace.New(state, m, nil, cwd)
	defer service.Close()
	handler := New(testDeps(t, Deps{Config: config.Config{DefaultCwd: cwd, Token: "secret"}, Manager: m, Workspace: service}))
	response := workspaceRequest(t, handler, "GET", "/api/config", nil, 200)
	var settings struct {
		DefaultCwd string               `json:"defaultCwd"`
		Hostname   string               `json:"hostname"`
		Workspace  workspace.Capability `json:"workspace"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	hostname, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Hostname != hostname {
		t.Fatalf("hostname = %q, want %q", settings.Hostname, hostname)
	}
	if settings.Workspace.GitAvailable || settings.Workspace.RegistryAvailable || strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "token") {
		t.Fatalf("capability/credential response: %s", response.Body.String())
	}
	workspaceRequest(t, handler, "GET", "/api/repositories", nil, 503)
	workspaceRequest(t, handler, "POST", "/api/sessions", map[string]any{"preset": "custom", "cmd": []string{"/bin/sh", "-c", "printf DIRECT"}}, 201)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest("POST", "/api/sessions", strings.NewReader(`{"preset":"custom","cmd":["/bin/sh"]}`)).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != 504 {
		t.Fatalf("timeout boundary: %d %s", recorder.Code, recorder.Body.String())
	}
}
func TestUnavailableRepositoryIsLocalToList(t *testing.T) {
	handler, _, _, root := workspaceAPI(t)
	added := workspaceRequest(t, handler, "POST", "/api/repositories", map[string]string{"path": root}, 201)
	var repository contract.Repository
	if err := json.Unmarshal(added.Body.Bytes(), &repository); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(root+"-moved", root) })
	list := workspaceRequest(t, handler, "GET", "/api/repositories", nil, 200)
	var repositories []contract.Repository
	if err := json.Unmarshal(list.Body.Bytes(), &repositories); err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 || repositories[0].Availability != "unavailable" || repositories[0].Error == "" {
		t.Fatalf("local read failure lost registry: %s", list.Body.String())
	}
	workspaceRequest(t, handler, "GET", "/api/repositories/"+repository.ID+"/branches", nil, 503)
	workspaceRequest(t, handler, "DELETE", "/api/repositories/"+repository.ID, nil, 204)
}
func TestWorkspaceMetadataUsesExistingWebSocketProtocol(t *testing.T) {
	handler, service, m, root := workspaceAPI(t)
	repository, _, err := service.Register(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := service.CreateSession(context.Background(), workspace.SessionInput{Cwd: root, Preset: "custom", Cmd: []string{"/bin/sh", "-c", "printf READY; read value"}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/sessions", &websocket.DialOptions{Subprotocols: tokenProtocols("secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var event contract.SessionEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "snapshot" || len(event.Sessions) != 1 || event.Sessions[0].Workspace == nil || event.Sessions[0].Workspace.RepositoryID != repository.ID {
		t.Fatalf("list WS workspace: %s", data)
	}
	terminal, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/s/"+item.Info().ID, &websocket.DialOptions{Subprotocols: tokenProtocols("secret")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = terminal.CloseNow() }()
	readPosition(ctx, t, terminal, "reset")
	kind, replay, err := terminal.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || len(replay) == 0 || replay[0] != 1 {
		t.Fatalf("binary replay changed: %v %q %v", kind, replay, err)
	}
	_, state, err := terminal.Read(ctx)
	if err != nil || len(state) == 0 || state[0] != 2 {
		t.Fatalf("state frame changed: %q %v", state, err)
	}
	var info contract.SessionInfo
	if err := json.Unmarshal(state[1:], &info); err != nil {
		t.Fatal(err)
	}
	if info.Workspace == nil || info.Workspace.RepositoryID != repository.ID {
		t.Fatalf("terminal WS metadata: %s", state)
	}
	if err := m.Close(item.Info().ID); err != nil {
		t.Fatal(err)
	}
}
