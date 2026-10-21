//go:build linux || darwin

package attach

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
)

// fakeTerminal feeds scripted input and records output.
type fakeTerminal struct {
	input      *io.PipeReader
	feed       *io.PipeWriter
	mu         sync.Mutex
	out        bytes.Buffer
	cols, rows int
}

func newTerminal(cols, rows int) *fakeTerminal {
	r, w := io.Pipe()
	return &fakeTerminal{input: r, feed: w, cols: cols, rows: rows}
}

func (f *fakeTerminal) Read(p []byte) (int, error) { return f.input.Read(p) }
func (f *fakeTerminal) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.Write(p)
}
func (f *fakeTerminal) Size() (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows, nil
}
func (f *fakeTerminal) setSize(cols, rows int) {
	f.mu.Lock()
	f.cols, f.rows = cols, rows
	f.mu.Unlock()
}
func (f *fakeTerminal) output() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.out.String()
}

func server(t *testing.T) (*session.Manager, *localclient.Client) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "att")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	m := session.NewManager(session.Options{})
	ws := workspace.New(dir, m, dir)
	handler := api.NewLocal(config.Config{DefaultCwd: dir}, m, ws, nil, api.Local{Status: func() contract.StatusV1 { return contract.StatusV1{} }})
	socket := filepath.Join(dir, "s.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(l) }()
	client := localclient.New(localclient.UnixDialer{Path: socket})
	t.Cleanup(func() {
		client.Close()
		_ = srv.Close()
		m.Shutdown(context.Background())
		ws.Close()
	})
	return m, client
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAttachInputOutputResizeAndDetach(t *testing.T) {
	m, client := server(t)
	s, err := m.Create("", "custom", []string{"sh", "-c", "stty -echo; while read line; do echo \"got:$line\"; stty size; done"}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	term := newTerminal(120, 40)
	done := make(chan Result, 1)
	errs := make(chan error, 1)
	go func() {
		r, err := Run(context.Background(), client, s.Info().ID, term, Options{})
		errs <- err
		done <- r
	}()
	_, _ = term.feed.Write([]byte("one\r"))
	eventually(t, "input echo and initial size", func() bool { return strings.Contains(term.output(), "got:one\r\n40 120") })
	term.setSize(160, 50)
	time.Sleep(2 * ResizeInterval)
	_, _ = term.feed.Write([]byte("two\r"))
	eventually(t, "resized terminal", func() bool { return strings.Contains(term.output(), "got:two\r\n50 160") })
	// Ctrl+C is input; the detach key never reaches the session.
	_, _ = term.feed.Write([]byte("three\x1d"))
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if r := <-done; !r.Detached {
		t.Fatalf("result %+v", r)
	}
	if info := s.Info(); info.Status == "exited" {
		t.Fatal("detaching ended the session")
	}
}

func TestAttachReportsExitCodeAndMissingSession(t *testing.T) {
	m, client := server(t)
	s, err := m.Create("", "custom", []string{"sh", "-c", "read x; exit 7"}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	term := newTerminal(80, 24)
	go func() { _, _ = term.feed.Write([]byte("\r")) }()
	r, err := Run(context.Background(), client, s.Info().ID, term, Options{})
	if err != nil || !r.Exited || r.ExitCode != 7 {
		t.Fatalf("exit %+v %v", r, err)
	}
	if _, err := Run(context.Background(), client, "missing0", newTerminal(80, 24), Options{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing session: %v", err)
	}
}

func TestObserveSendsNothing(t *testing.T) {
	m, client := server(t)
	s, err := m.Create("", "custom", []string{"cat"}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	term := newTerminal(200, 60)
	errs := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), client, s.Info().ID, term, Options{Observe: true})
		errs <- err
	}()
	_, _ = term.feed.Write([]byte("typed-while-observing\r"))
	time.Sleep(3 * ResizeInterval)
	_, _ = term.feed.Write([]byte{DetachKey})
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	info := s.Info()
	if info.Cols == 200 || strings.Contains(term.output(), "typed-while-observing") {
		t.Fatalf("observer changed the session: %dx%d output %q", info.Cols, info.Rows, term.output())
	}
}
