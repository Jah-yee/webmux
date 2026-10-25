package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/files"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/wire"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProtocolAndSecurity(t *testing.T) {
	c, err := config.Load(t.TempDir()+"/absent", false)
	if err != nil {
		t.Fatal(err)
	}
	c.Token = "secret"
	c.DefaultCwd = t.TempDir()
	if err = c.Validate(); err != nil {
		t.Fatal(err)
	}
	m := session.NewManager(testSessionOptions(c))
	server := httptest.NewServer(New(testDeps(t, Deps{Config: c, Manager: m, Workspace: testWorkspace(t, m, c.DefaultCwd)})))
	defer server.Close()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	request := func(method, path, body, token, origin string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	for _, row := range []struct {
		token, origin string
		want          int
	}{{"", "", 401}, {"wrong", "", 401}, {"secret", "http://evil.example", 403}, {"secret", "", 200}} {
		res := request("GET", "/api/health", "", row.token, row.origin)
		_ = res.Body.Close()
		if res.StatusCode != row.want {
			t.Fatalf("status %d want %d", res.StatusCode, row.want)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(path string) *websocket.Conn {
		t.Helper()
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+path, &websocket.DialOptions{Subprotocols: tokenProtocols("secret")})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = conn.CloseNow() })
		return conn
	}
	list := dial("/ws/sessions")
	_, raw, err := list.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Type     string
		Sessions []contract.SessionInfo
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil || snapshot.Type != "snapshot" || snapshot.Sessions == nil {
		t.Fatalf("empty snapshot: %s", raw)
	}
	res := request("POST", "/api/sessions", `{"preset":"shell"}`, "secret", "")
	if res.StatusCode != 201 {
		t.Fatal(res.Status)
	}
	var info contract.SessionInfo
	_ = json.NewDecoder(res.Body).Decode(&info)
	_ = res.Body.Close()
	res = request("DELETE", "/api/sessions/"+info.ID+"?remove=1", "", "secret", "")
	_ = res.Body.Close()
	if res.StatusCode != 409 {
		t.Fatal("running removal must conflict")
	}
	conn := dial("/ws/s/" + info.ID)
	readPosition(ctx, t, conn, "reset")
	kind, p, err := conn.Read(ctx)
	if err != nil || kind != websocket.MessageBinary || p[0] != 1 {
		t.Fatalf("initial frame %v %v %v", kind, p, err)
	}
	if err = conn.Write(ctx, websocket.MessageBinary, contract.Frame(0, []byte("printf 'FINAL_OUTPUT_MARKER\\n'; exit 6\n"))); err != nil {
		t.Fatal(err)
	}
	for {
		_, p, err = conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if p[0] == 3 {
			break
		}
	}
	again := dial("/ws/s/" + info.ID)
	readPosition(ctx, t, again, "reset")
	_, p, err = again.Read(ctx)
	if err != nil || p[0] != 1 || !strings.Contains(string(p[1:]), "FINAL_OUTPUT_MARKER") {
		t.Fatalf("final replay %q %v", p, err)
	}
	for {
		_, p, err = again.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if p[0] == 3 {
			if string(p[1:]) != `{"exitCode":6}` {
				t.Fatal(string(p))
			}
			break
		}
	}
	_, _, err = again.Read(ctx)
	if websocket.CloseStatus(err) != 4410 {
		t.Fatalf("exit close: %v", err)
	}
	res = request("GET", "/api/sessions/"+info.ID+"/buffer", "", "secret", "")
	buffer, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !strings.Contains(string(buffer), "FINAL_OUTPUT_MARKER") {
		t.Fatal("buffer lost output")
	}
	res = request("DELETE", "/api/sessions/"+info.ID+"?remove=1", "", "secret", "")
	_ = res.Body.Close()
	if res.StatusCode != 204 {
		t.Fatal(res.Status)
	}
}

func TestDirectoryPickerListsAllDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	names := []string{"webmux", "zz directory"}
	for i := range 25 {
		names = append(names, fmt.Sprintf(".cache-%02d", i))
	}
	for _, name := range names {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "a-linked")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "ordinary-file")
	if err := os.WriteFile(file, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"file-link": file, "broken-link": filepath.Join(root, "missing")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	allNames := append([]string{"a-linked"}, names...)
	wantAll := make([]string, 0, len(allNames))
	for _, name := range allNames {
		wantAll = append(wantAll, filepath.Join(root, name))
	}
	server := &Server{Deps: Deps{Config: config.Config{DefaultCwd: root}, Files: files.New(root)}}
	for _, row := range []struct {
		name, prefix string
		want         []string
	}{
		{"default", "", wantAll},
		{"trailing slash", root + "/", wantAll},
		{"home", "~", wantAll},
		{"home slash", "~/", wantAll},
		{"partial name", root + "/web", []string{filepath.Join(root, "webmux")}},
		{"home partial name", "~/web", []string{filepath.Join(root, "webmux")}},
		{"hidden prefix", root + "/.cache-2", wantAll[len(wantAll)-5:]},
		{"no match", root + "/absent", []string{}},
		{"symlink navigation", root + "/a-linked/", []string{}},
	} {
		t.Run(row.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			server.dirs(recorder, httptest.NewRequest("GET", "/api/fs/dirs?prefix="+url.QueryEscape(row.prefix), nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
			}
			var got []string
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, row.want) {
				t.Fatalf("directories = %v, want %v", got, row.want)
			}
		})
	}
	for _, prefix := range []string{root + "/missing/", file + "/"} {
		recorder := httptest.NewRecorder()
		server.dirs(recorder, httptest.NewRequest("GET", "/api/fs/dirs?prefix="+url.QueryEscape(prefix), nil))
		var body map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusBadRequest || body["error"] == "" {
			t.Fatalf("directory error: status %d, body %v", recorder.Code, body)
		}
	}
}

func TestCustomCommandExecutesLiteralArgvInSelectedDirectory(t *testing.T) {
	cwd := t.TempDir()
	script := "#!/bin/sh\nprintf 'cwd=%s\\n' \"$PWD\"\nprintf '<%s>\\n' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(cwd, "report"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m := session.NewManager(session.Options{DefaultCwd: t.TempDir()})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	cmd := []string{"./report", "two words", `"quoted value"`, "$HOME", "*", "$(touch sentinel)", "; exit 9"}
	body, err := json.Marshal(map[string]any{"cwd": cwd, "preset": "custom", "cmd": cmd, "args": []string{}, "name": "My command"})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	New(testDeps(t, Deps{Config: config.Config{}, Manager: m, Workspace: testWorkspace(t, m, cwd)})).ServeHTTP(recorder, httptest.NewRequest("POST", "/api/sessions", strings.NewReader(string(body))))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	var info contract.SessionInfo
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Preset != "custom" || info.Cwd != cwd || info.Title != "My command" || !reflect.DeepEqual(info.Cmd, cmd) {
		t.Fatalf("custom session info: %+v", info)
	}
	item, err := m.Get(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	sub := item.Attach()
	defer item.Detach(sub)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-sub.Out:
			if m.Exit == nil {
				continue
			}
			info = wire.Session(item.Info())
			if info.ExitCode == nil || *info.ExitCode != 0 {
				t.Fatalf("custom command exit: %+v", info)
			}
			want := "cwd=" + cwd + "\r\n"
			for _, arg := range cmd[1:] {
				want += "<" + arg + ">\r\n"
			}
			if got := string(item.Buffer()); got != want {
				t.Fatalf("PTY output = %q, want %q", got, want)
			}
			if _, err := os.Stat(filepath.Join(cwd, "sentinel")); !os.IsNotExist(err) {
				t.Fatalf("shell expansion created sentinel: %v", err)
			}
			return
		case <-deadline:
			t.Fatal("custom PTY failed to exit")
		}
	}
}

func TestSessionCreationRejectsCommandConflicts(t *testing.T) {
	presets := []config.Preset{
		{Name: "fixed", Cmd: []string{"/bin/echo", "fixed"}},
		{Name: "with-args", Cmd: []string{"/bin/echo", "preset"}, AllowArgs: true},
	}
	m := session.NewManager(session.Options{DefaultCwd: t.TempDir(), Presets: testPresets(presets)})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	}()
	handler := New(testDeps(t, Deps{Config: config.Config{Presets: presets}, Manager: m, Workspace: testWorkspace(t, m, t.TempDir())}))
	for _, body := range []string{
		`{"preset":"custom"}`,
		`{"preset":"custom","cmd":[]}`,
		`{"preset":"custom","cmd":[""]}`,
		`{"preset":"custom","cmd":["/bin/echo"],"args":["extra"]}`,
		`{"preset":"fixed","cmd":["/bin/echo"]}`,
		`{"preset":"with-args","cmd":["/bin/echo"],"args":["extra"]}`,
		`{"preset":"fixed","cmd":[]}`,
		`{"cmd":["/bin/echo"]}`,
		`{"preset":"fixed","args":["extra"]}`,
		`{"preset":"missing"}`,
		`{"preset":"custom","cmd":["/definitely/missing/webmux-executable"]}`,
	} {
		t.Run(body, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/sessions", strings.NewReader(body)))
			var response map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusBadRequest || response["error"] == "" {
				t.Fatalf("status %d, body %v", recorder.Code, response)
			}
		})
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("invalid requests created sessions: %v", got)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/sessions", strings.NewReader(`{"preset":"with-args","args":["two words","$HOME"]}`)))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("preset with allowed arguments: status %d: %s", recorder.Code, recorder.Body.String())
	}
	var info contract.SessionInfo
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.Preset != "with-args" || !reflect.DeepEqual(info.Cmd, []string{"/bin/echo", "preset", "two words", "$HOME"}) {
		t.Fatalf("preset arguments: %+v", info)
	}
}
