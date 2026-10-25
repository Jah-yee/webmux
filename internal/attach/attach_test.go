//go:build linux || darwin

package attach

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/api"
	"github.com/rainchan/webmux/internal/codeview"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/files"
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
	ws := workspace.New(dir, m, nil, dir)
	handler := api.NewLocal(api.Deps{Config: config.Config{DefaultCwd: dir}, Manager: m, Workspace: ws, Code: codeview.New(m), Files: files.New(dir)}, api.Local{Status: func() contract.StatusV1 { return contract.StatusV1{} }})
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
	// The shell behind the native terminal must get its keys back unencoded.
	if !strings.HasSuffix(term.output(), inputModeReset) {
		t.Fatalf("input modes left enabled: %q", term.output())
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

// Once another client takes control, the native terminal's automatic replies
// must not claim it back: input is dropped under a banner until Enter.
func TestOtherControllerKeepsControlUntilEnter(t *testing.T) {
	m, client := server(t)
	s, err := m.Create("", "custom", []string{"sh", "-c", "stty -echo; while read line; do echo \"got:$line\"; done"}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := s.Info().ID
	term := newTerminal(120, 40)
	errs := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), client, id, term, Options{Client: "native-test"})
		errs <- err
	}()
	_, _ = term.feed.Write([]byte("first\r"))
	eventually(t, "native input", func() bool { return strings.Contains(term.output(), "got:first") })

	browser, err := client.DialWebSocket(context.Background(), "/ws/s/"+id+"?client=browser-test&label=Chrome")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = browser.CloseNow() }()
	var mu sync.Mutex
	current := ""
	controller := func() string {
		mu.Lock()
		defer mu.Unlock()
		return current
	}
	go func() {
		for {
			_, frame, err := browser.Read(context.Background())
			if err != nil {
				return
			}
			var state struct {
				Controller *struct {
					ClientID string `json:"clientId"`
				} `json:"controller"`
			}
			if len(frame) > 0 && frame[0] == frameControl && json.Unmarshal(frame[1:], &state) == nil {
				mu.Lock()
				current = ""
				if state.Controller != nil {
					current = state.Controller.ClientID
				}
				mu.Unlock()
			}
		}
	}()
	eventually(t, "native control", func() bool { return controller() == "native-test" })
	if err := browser.Write(context.Background(), websocket.MessageBinary, []byte("\x00browser\r")); err != nil {
		t.Fatal(err)
	}
	eventually(t, "banner", func() bool { return strings.Contains(term.output(), "Controlled by Chrome") })

	// A focus report and a device attributes reply, as terminals send them.
	_, _ = term.feed.Write([]byte("\x1b[I\x1b[?62;22c"))
	time.Sleep(3 * ResizeInterval)
	if got := controller(); got != "browser-test" {
		t.Fatalf("automatic replies moved control to %q", got)
	}

	_, _ = term.feed.Write([]byte("\r"))
	eventually(t, "reclaimed control", func() bool { return controller() == "native-test" })
	_, _ = term.feed.Write([]byte("native\r"))
	eventually(t, "native input", func() bool { return strings.Contains(term.output(), "got:native") })
	if strings.Contains(term.output(), "62;22") {
		t.Fatalf("dropped input reached the session: %q", term.output())
	}
	_, _ = term.feed.Write([]byte{DetachKey})
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

// A program that enabled colour scheme reports (DEC 2031) before the native
// terminal attached learns that terminal's scheme from its background: the
// probe's answer is consumed, and only CSI ? 997 ; 1|2 n reaches the program.
func TestAttachReportsNativeColorScheme(t *testing.T) {
	for _, tc := range []struct {
		name, background, want string
	}{
		{"dark", "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\", "E[?997;1n"},
		{"light", "\x1b]11;rgb:ffff/fafa/f0f0\a", "E[?997;2n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, client := server(t)
			s, err := m.Create("", "custom", []string{"sh", "-c", `stty -icanon -echo; printf '\033[?2031hready\n'; head -c 9 | tr '\033' E; echo; sleep 30`}, nil, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			eventually(t, "program enabled reports", func() bool { return strings.Contains(s.Info().Preview, "ready") })
			term := newTerminal(80, 24)
			errs := make(chan error, 1)
			go func() {
				_, err := Run(context.Background(), client, s.Info().ID, term, Options{})
				errs <- err
			}()
			eventually(t, "background probe", func() bool { return strings.Contains(term.output(), backgroundQuery) })
			_, _ = term.feed.Write([]byte(tc.background))
			eventually(t, "scheme report", func() bool { return strings.Contains(term.output(), tc.want) })
			_, _ = term.feed.Write([]byte{DetachKey})
			if err := <-errs; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBackgroundSchemeParsesOSC11Replies(t *testing.T) {
	for _, tc := range []struct {
		in         string
		start, end int
		dark, ok   bool
	}{
		{"\x1b]11;rgb:0000/0000/0000\x1b\\", 0, 25, true, true},
		{"a\x1b]11;rgb:ff/ff/ff\ab", 1, 19, false, true},
		{"\x1b]11;rgb:f/f/f\a", 0, 15, false, true},
		{"\x1b]11;rgb:2828/2c2c/3434\x1b\\", 0, 25, true, true},
		{"\x1b]11;rgb:0000/0000\a", 0, 0, false, false},
		{"\x1b]11;rgb:0000/0000/00", 0, 0, false, false},
		{"\x1b]10;rgb:ffff/ffff/ffff\a", 0, 0, false, false},
	} {
		start, end, dark, ok := backgroundReply([]byte(tc.in))
		if ok != tc.ok || ok && (start != tc.start || end != tc.end || dark != tc.dark) {
			t.Errorf("backgroundReply(%q) = %d %d %v %v", tc.in, start, end, dark, ok)
		}
	}
}

// Programs such as omp enable modifyOtherKeys or kitty keyboard mode, after
// which the native terminal encodes Ctrl+] as an escape sequence.
func TestDetachIndexRecognisesEncodedCtrlBracket(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"ab\x1d", 2},
		{"ab\x1b[27;5;93~cd", 2},
		{"\x1b[93;5u", 0},
		{"x\x1b[93;5:1u", 1},
		{"\x1b[93;5u\x1d", 0},
		{"\x1b[27;5;91~\x1b[93u", -1},
		{"\x1b[93;5;29u", 0},
		{"\x1b[93:125;5u", 0},
		{"\x1b[93;69u", 0},
		{"\x1b[93;5:3u", -1},
		{"\x1b[93;6u\x1b[93;3u", -1},
		{"\x1b[A\x1b[93;5:2u", 3},
		// win32-input-mode: Ctrl down, then ] with Left Ctrl held.
		{"\x1b[17;29;0;1;8;1_\x1b[221;27;29;1;8;1_", 16},
		{"\x1b[221;27;29;0;8;1_", -1},
		{"\x1b[221;27;93;1;10;1_", -1},
	} {
		if got := detachIndex([]byte(tc.in)); got != tc.want {
			t.Errorf("detachIndex(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestTakeControlIndexRecognisesPlainEnterOnly(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"x\r", 1},
		{"\x1b[I\x1b[13u", 3},
		{"\x1b[13;1:1u", 0},
		{"\x1b[13;5u\x1b[13;1:3u", -1},
		{"\x1b[?62;22c\x1b[O", -1},
		{"\x1b[13;28;13;1;0;1_", 0},
		{"\x1b[13;28;13;1;32;1_", 0},
		{"\x1b[13;28;13;0;0;1_\x1b[13;28;10;1;8;1_", -1},
	} {
		if got := takeControlIndex([]byte(tc.in)); got != tc.want {
			t.Errorf("takeControlIndex(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
