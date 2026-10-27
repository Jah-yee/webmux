//go:build linux || darwin

package sessioncmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rainchan/webmux/internal/attach"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
)

const sessionsJSON = `[{"id":"abcdefgh","title":"review","status":"running","cmd":["codex"],"cwd":"/w","exitCode":null}]`

type fakeServer struct {
	mu        sync.Mutex
	bodies    []map[string]any
	rejectEnv bool
}

func (f *fakeServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		reject := f.rejectEnv
		f.mu.Unlock()
		if _, ok := body["env"]; ok && reject {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"json: unknown field \"env\""}`)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"newsess1","status":"running"}`)
	})
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, sessionsJSON) })
	mux.HandleFunc("GET /api/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "abcdefgh" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"session not found"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"abcdefgh"}`)
	})
	mux.HandleFunc("GET "+contract.PathStatus, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(contract.StatusV1{V: 1, URLs: contract.URLs{Local: "http://127.0.0.1:7681"}})
	})
	return mux
}

func env(t *testing.T, f *fakeServer) (Env, *bytes.Buffer, *bytes.Buffer, *[]string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "scm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "s.sock")
	if f != nil {
		l, err := net.Listen("unix", socket)
		if err != nil {
			t.Fatal(err)
		}
		srv := &http.Server{Handler: f.handler()}
		go func() { _ = srv.Serve(l) }()
		t.Cleanup(func() { _ = srv.Close() })
	}
	var stdout, stderr bytes.Buffer
	opened := &[]string{}
	return Env{
		Connect:        func() (*localclient.Client, error) { return localclient.New(localclient.UnixDialer{Path: socket}), nil },
		Stdout:         &stdout,
		Stderr:         &stderr,
		OpenConsole:    func() (Console, error) { return &fakeConsole{}, nil },
		OpenBrowser:    func(url string) error { *opened = append(*opened, url); return nil },
		ResolveCwd:     func(_ context.Context, cwd string) (string, error) { return "/mnt/c/work/" + cwd, nil },
		NotRunningHint: "start it",
	}, &stdout, &stderr, opened
}

func TestRunSendsArgvCwdEnvAndDetaches(t *testing.T) {
	f := &fakeServer{}
	e, stdout, _, _ := env(t, f)
	code := Run(context.Background(), e, []string{"--detach", "--name", "review", "--env", "A=1", "--env", "B=x=y", "--cwd", "app", "--", "printf", "%s;$(id)", "a b"})
	if code != 0 || stdout.String() != "newsess1\thttp://127.0.0.1:7681/s/newsess1\n" {
		t.Fatalf("detach %d %q", code, stdout.String())
	}
	want := map[string]any{"cmd": []any{"printf", "%s;$(id)", "a b"}, "cwd": "/mnt/c/work/app", "name": "review", "env": map[string]any{"A": "1", "B": "x=y"}}
	if !reflect.DeepEqual(f.bodies[0], want) {
		t.Fatalf("request %#v", f.bodies[0])
	}
	if code := Run(context.Background(), e, []string{"--detach"}); code != contract.ExitUsage {
		t.Fatalf("missing command exit %d", code)
	}
	if code := Run(context.Background(), e, []string{"--env", "novalue", "--", "true"}); code != contract.ExitUsage {
		t.Fatalf("bad --env exit %d", code)
	}
}

func TestRunReportsUnsupportedEnvAndStoppedServer(t *testing.T) {
	f := &fakeServer{rejectEnv: true}
	e, _, stderr, _ := env(t, f)
	if code := Run(context.Background(), e, []string{"--detach", "--env", "A=1", "--", "true"}); code != 1 || !strings.Contains(stderr.String(), `unknown field \"env\"`) && !strings.Contains(stderr.String(), `unknown field "env"`) {
		t.Fatalf("unsupported env %d %q", code, stderr.String())
	}
	stopped, _, stderr, _ := env(t, nil)
	if code := Run(context.Background(), stopped, []string{"--", "claude"}); code != contract.ExitNotRunning || !strings.Contains(stderr.String(), "not running; start it") {
		t.Fatalf("stopped server %d %q", code, stderr.String())
	}
	if code := List(context.Background(), stopped, nil); code != contract.ExitNotRunning {
		t.Fatalf("ls on stopped server %d", code)
	}
}

func TestListAndOpen(t *testing.T) {
	e, stdout, _, opened := env(t, &fakeServer{})
	if code := List(context.Background(), e, []string{"--json"}); code != 0 || strings.TrimSpace(stdout.String()) != sessionsJSON {
		t.Fatalf("ls --json %d %q", code, stdout.String())
	}
	stdout.Reset()
	if code := List(context.Background(), e, nil); code != 0 || !strings.Contains(stdout.String(), "abcdefgh  review  running  codex    /w") {
		t.Fatalf("ls %d %q", code, stdout.String())
	}
	stdout.Reset()
	if code := Open(context.Background(), e, []string{"--browser", "abcdefgh"}); code != 0 || stdout.String() != "http://127.0.0.1:7681/s/abcdefgh\n" || len(*opened) != 1 {
		t.Fatalf("open %d %q %v", code, stdout.String(), *opened)
	}
	if code := Open(context.Background(), e, []string{"missing"}); code != 1 {
		t.Fatalf("open missing %d", code)
	}
}

// fakeConsole is a terminal whose input is already closed.
type fakeConsole struct{ restored bool }

func (c *fakeConsole) ReadContext(context.Context, []byte) (int, error) { return 0, io.EOF }
func (c *fakeConsole) Write(p []byte) (int, error)                      { return len(p), nil }
func (c *fakeConsole) Size() (int, int, error)                          { return 80, 24, nil }
func (c *fakeConsole) Restore() error                                   { c.restored = true; return nil }

func TestAttachedRunAnnouncesSessionAndNeedsTerminal(t *testing.T) {
	f := &fakeServer{}
	e, stdout, stderr, _ := env(t, f)
	console := &fakeConsole{}
	e.OpenConsole = func() (Console, error) { return console, nil }
	// The fake server has no WebSocket endpoint, so attaching then fails.
	code := Run(context.Background(), e, []string{"--", "claude"})
	if code != 1 || stdout.Len() != 0 || !console.restored {
		t.Fatalf("attached run %d stdout %q restored %v", code, stdout.String(), console.restored)
	}
	if !strings.HasPrefix(stderr.String(), "webmux: session newsess1 · http://127.0.0.1:7681/s/newsess1 · Ctrl+] to detach\r\n") {
		t.Fatalf("announcement %q", stderr.String())
	}
	e.OpenConsole = func() (Console, error) { return nil, attach.ErrNotTerminal }
	if code := Run(context.Background(), e, []string{"--", "claude"}); code != contract.ExitUsage || len(f.bodies) != 1 {
		t.Fatalf("no terminal: exit %d, %d sessions created", code, len(f.bodies))
	}
}
