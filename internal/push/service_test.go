package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rainchan/webmux/internal/attentionkind"
	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
)

type received struct {
	path   string
	header http.Header
	body   []byte
}

// pushServer is a fake push service: handler decides the status per path.
type pushServer struct {
	*httptest.Server
	mu   sync.Mutex
	got  []received
	hits chan struct{}
}

func newPushServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *pushServer {
	t.Helper()
	p := &pushServer{hits: make(chan struct{}, 64)}
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.got = append(p.got, received{r.URL.Path, r.Header.Clone(), body})
		p.mu.Unlock()
		p.hits <- struct{}{}
		if handler != nil {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *pushServer) requests() []received {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]received(nil), p.got...)
}

func pushConfig() config.PushConfig {
	return config.PushConfig{Enabled: true, Subject: "mailto:me@example.com", TTLSeconds: 86400, Kinds: config.DefaultPushKinds(), IncludeTitle: true}
}

func startService(t *testing.T, m *session.Manager, server *pushServer, log *slog.Logger) *Service {
	t.Helper()
	if log == nil {
		log = discard()
	}
	s := New(pushConfig(), t.TempDir(), m, log, server.Client().Transport)
	t.Cleanup(s.Close)
	if err := s.Unavailable(); err != nil {
		t.Fatal(err)
	}
	return s
}

func newManager(t *testing.T, presets ...session.Preset) *session.Manager {
	t.Helper()
	m := session.NewManager(session.Options{DefaultCwd: t.TempDir(), Presets: presets})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	return m
}

func TestServicePushesNewQuestionOnce(t *testing.T) {
	question := `printf '\033]777;notify;warp://cli-agent;{"v":1,"event":"question_asked"}\007'; ` +
		`stty raw -echo; while :; do printf '\r|'; sleep 0.05; printf '\r/'; sleep 0.05; done`
	m := newManager(t, session.Preset{Name: "agent", Cmd: []string{"/bin/bash", "-c", question}})
	server := newPushServer(t, nil)
	s := startService(t, m, server, nil)
	p256dh, auth, ua := browserKeys(t)
	if _, err := s.Register(server.URL+"/device", p256dh, auth, "https://term.example.com"); err != nil {
		t.Fatal(err)
	}
	created, err := m.Create("", "agent", nil, nil, "deploy", nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.hits:
	case <-time.After(5 * time.Second):
		t.Fatal("question was never pushed")
	}
	// The spinner keeps producing output and list updates; none may alert.
	time.Sleep(2 * time.Second)
	got := server.requests()
	if len(got) != 1 {
		t.Fatalf("%d push requests, want 1", len(got))
	}
	h := got[0].header
	if h.Get("Content-Encoding") != "aes128gcm" || h.Get("TTL") != "86400" || h.Get("Urgency") != "high" ||
		!strings.HasPrefix(h.Get("Authorization"), "vapid t=") || !strings.HasSuffix(h.Get("Authorization"), ", k="+s.PublicKey()) {
		t.Fatalf("headers %v", h)
	}
	var message struct {
		Notification struct{ Title, Body, Navigate string } `json:"notification"`
		WebMux       struct {
			SessionID string `json:"sessionId"`
			Kind      string `json:"kind"`
		} `json:"webmux"`
	}
	if err := json.Unmarshal(decryptForTest(t, got[0].body, ua, auth), &message); err != nil {
		t.Fatal(err)
	}
	id := created.Info().ID
	if message.WebMux.SessionID != id || message.WebMux.Kind != "question" || message.Notification.Title != "deploy" ||
		message.Notification.Body != "需要回答" || message.Notification.Navigate != "https://term.example.com/s/"+id {
		t.Fatalf("message %+v", message)
	}
}

func TestServiceTestDeliveryOutcomes(t *testing.T) {
	m := newManager(t)
	server := newPushServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch strings.Split(r.URL.Path, "/")[1] {
		case "gone":
			w.WriteHeader(http.StatusGone)
		case "moved":
			http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
		case "busy":
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusCreated)
		}
	})
	var logs bytes.Buffer
	s := startService(t, m, server, slog.New(slog.NewTextHandler(&logs, nil)))
	register := func(path string) string {
		p256dh, auth, _ := browserKeys(t)
		id, err := s.Register(server.URL+path+"/SECRET-TOKEN", p256dh, auth, "http://127.0.0.1:7681")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	ok, gone, moved, busy := register(""), register("/gone"), register("/moved"), register("/busy")
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = closed.Close()
	keys := testSubscription(t, "")
	unreachable, err := s.Register("https://"+closed.Addr().String()+"/SECRET-TOKEN", keys.P256dh, keys.Auth, "http://127.0.0.1:7681")
	if err != nil {
		t.Fatal(err)
	}
	report, err := s.SendTestAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// One accepted, one expired (deleted), and redirect, 429 and unreachable
	// failures that keep their subscriptions.
	if report.Devices != 5 || report.Accepted != 1 || !slices.Equal(report.Removed, []string{gone}) || len(report.Errors) != 3 {
		t.Fatalf("report %+v", report)
	}
	for _, message := range report.Errors {
		if strings.Contains(message, "SECRET-TOKEN") {
			t.Fatalf("error leaks the endpoint: %q", message)
		}
	}
	if _, kept := s.store.Get(gone); kept || s.Subscriptions() != 4 {
		t.Fatalf("410 subscription kept; %d remain", s.Subscriptions())
	}
	for name, id := range map[string]string{"accepted": ok, "redirect": moved, "429": busy, "unreachable": unreachable} {
		if _, kept := s.store.Get(id); !kept {
			t.Fatalf("%s removed the subscription", name)
		}
	}
	for _, r := range server.requests() {
		if r.path == "/elsewhere" {
			t.Fatal("redirect was followed")
		}
	}
	if strings.Contains(logs.String(), "SECRET-TOKEN") || !strings.Contains(logs.String(), "push delivery failed") {
		t.Fatalf("logs:\n%s", logs.String())
	}
}

func TestServiceQueueNeverBlocksAndCloseJoins(t *testing.T) {
	release := make(chan struct{})
	server := newPushServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	var logs bytes.Buffer
	s := New(pushConfig(), t.TempDir(), newManager(t), slog.New(slog.NewTextHandler(&logs, nil)), server.Client().Transport)
	p256dh, auth, _ := browserKeys(t)
	if _, err := s.Register(server.URL+"/slow", p256dh, auth, "http://127.0.0.1:7681"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for range queueSize + workers + 10 {
		s.enqueue(Alert{SessionID: "abcdefgh", Kind: attentionkind.Question})
	}
	if time.Since(start) > time.Second {
		t.Fatal("enqueue blocked on a slow push service")
	}
	if !strings.Contains(logs.String(), "push queue full") {
		t.Fatalf("dropped alerts not logged:\n%s", logs.String())
	}
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not stop in-flight sends")
	}
}

func TestServiceSendTimesOutAndKeepsSubscription(t *testing.T) {
	release := make(chan struct{})
	server := newPushServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	s := startService(t, newManager(t), server, nil)
	if s.client.Timeout != sendTimeout || sendTimeout != 10*time.Second {
		t.Fatalf("client timeout %v", s.client.Timeout)
	}
	s.client.Timeout = 200 * time.Millisecond // stands in for the 10 s limit
	p256dh, auth, _ := browserKeys(t)
	id, err := s.Register(server.URL+"/hang", p256dh, auth, "http://127.0.0.1:7681")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	report, err := s.SendTestAll(context.Background())
	if err != nil || report.Accepted != 0 || len(report.Errors) != 1 || len(report.Removed) != 0 || time.Since(start) > 5*time.Second {
		t.Fatalf("hung push service: %+v, %v after %v", report, err, time.Since(start))
	}
	if _, kept := s.store.Get(id); !kept {
		t.Fatal("timeout removed the subscription")
	}
}

func TestDisabledServiceTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	cfg := pushConfig()
	cfg.Enabled = false
	s := New(cfg, dir, newManager(t), discard(), nil)
	if s != nil {
		t.Fatal("disabled push returned a service")
	}
	if !errors.Is(s.Unavailable(), ErrDisabled) || s.Status().Enabled || s.Subscriptions() != 0 {
		t.Fatalf("nil service state: %v %+v", s.Unavailable(), s.Status())
	}
	if _, err := s.Register("https://push.example/x", "", "", ""); !errors.Is(err, ErrDisabled) {
		t.Fatalf("register while disabled: %v", err)
	}
	s.Close()
	if _, err := os.Stat(filepath.Join(dir, storeFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("push store created while disabled: %v", err)
	}
}

func TestStorageFailureLeavesServiceUnavailable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	s := New(pushConfig(), file, newManager(t), discard(), http.DefaultClient.Transport)
	defer s.Close()
	status := s.Status()
	if !errors.Is(s.Unavailable(), ErrStorage) || !status.Enabled || status.Available || status.Error == "" {
		t.Fatalf("status %+v", status)
	}
}
