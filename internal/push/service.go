package push

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/session"
)

const (
	queueSize   = 64
	workers     = 2
	sendTimeout = 10 * time.Second
)

var (
	ErrDisabled = errors.New("推送已在配置中停用（[push] enabled = false）")
	ErrStorage  = errors.New("服务端推送存储不可用，请查看服务日志")
	ErrGone     = errors.New("订阅已失效，请重新开启")
)

// Status is what local clients see; it never carries endpoints or keys.
type Status struct {
	Enabled       bool
	Available     bool
	Error         string
	Subscriptions int
}

// Service watches the session list and pushes new attention signals to every
// registered subscription. A nil *Service means push is disabled.
type Service struct {
	cfg      config.PushConfig
	log      *slog.Logger
	store    *Store
	storeErr error
	client   *http.Client

	queue     chan Alert
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// New returns nil when push is disabled, without touching stateDir. Storage
// failures leave a Service that reports ErrStorage and sends nothing.
// transport reaches push services; nil uses a clone of the default one.
func New(cfg config.PushConfig, stateDir string, m *session.Manager, log *slog.Logger, transport http.RoundTripper) *Service {
	if !cfg.Enabled {
		return nil
	}
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}
	client := &http.Client{
		Transport:     transport,
		Timeout:       sendTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	s := &Service{cfg: cfg, log: log, client: client, queue: make(chan Alert, queueSize)}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	store, err := OpenStore(stateDir, log)
	if err != nil {
		log.Warn("push unavailable: storage", "error", err)
		s.storeErr = err
		return s
	}
	s.store = store
	s.wg.Add(1 + workers)
	go s.watch(m)
	for range workers {
		go s.work()
	}
	return s
}

// Close stops watching and sending and waits for every push goroutine.
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		s.cancel()
		s.wg.Wait()
	})
}

// Unavailable reports why push cannot be used, or nil.
func (s *Service) Unavailable() error {
	switch {
	case s == nil:
		return ErrDisabled
	case s.store == nil:
		return ErrStorage
	}
	return nil
}

func (s *Service) Status() Status {
	if s == nil {
		return Status{}
	}
	if s.store == nil {
		return Status{Enabled: true, Error: s.storeErr.Error()}
	}
	return Status{Enabled: true, Available: true, Subscriptions: s.store.Count()}
}

// Subscriptions is the number of registered devices; 0 when unavailable.
func (s *Service) Subscriptions() int {
	if s.Unavailable() != nil {
		return 0
	}
	return s.store.Count()
}

// PublicKey is the VAPID application server key, or "" when unavailable.
func (s *Service) PublicKey() string {
	if s.Unavailable() != nil {
		return ""
	}
	return s.store.PublicKey()
}

// Validate checks a browser subscription without service state or I/O,
// before the caller decides where it may be registered from.
func Validate(endpoint, p256dh, auth string) error {
	if err := checkEndpoint(endpoint); err != nil {
		return err
	}
	_, _, err := decodeClientKeys(p256dh, auth)
	return err
}

// Register stores a validated subscription made from origin.
func (s *Service) Register(endpoint, p256dh, auth, origin string) (string, error) {
	if err := s.Unavailable(); err != nil {
		return "", err
	}
	if err := Validate(endpoint, p256dh, auth); err != nil {
		return "", err
	}
	return s.store.Add(Subscription{Endpoint: endpoint, P256dh: p256dh, Auth: auth, Origin: origin})
}

func (s *Service) Unregister(id string) error {
	if err := s.Unavailable(); err != nil {
		return err
	}
	return s.store.Remove(id)
}

// TestReport summarises one test notification sent to every device. Removed
// lists the subscriptions the push service reported as expired, which are
// deleted; Errors name only the push service host, never the endpoint.
type TestReport struct {
	Devices  int      `json:"devices"`
	Accepted int      `json:"accepted"`
	Removed  []string `json:"removed"`
	Errors   []string `json:"errors"`
}

// SendTestAll pushes a test notification to every registered device at once
// and waits for the push services to answer, so one device can check what the
// others show in the background.
func (s *Service) SendTestAll(ctx context.Context) (TestReport, error) {
	if err := s.Unavailable(); err != nil {
		return TestReport{}, err
	}
	subs := s.store.List()
	report := TestReport{Devices: len(subs), Removed: []string{}, Errors: []string{}}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, sub := range subs {
		wg.Go(func() {
			err := s.sendTest(ctx, sub)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				report.Accepted++
			case errors.Is(err, ErrGone):
				report.Removed = append(report.Removed, sub.ID)
			default:
				report.Errors = append(report.Errors, err.Error())
			}
		})
	}
	wg.Wait()
	slices.Sort(report.Removed)
	slices.Sort(report.Errors)
	return report, nil
}

func (s *Service) sendTest(ctx context.Context, sub Subscription) error {
	body, err := testPayload(sub.Origin)
	if err != nil {
		return err
	}
	return s.deliver(ctx, sub, body, "normal")
}

func (s *Service) watch(m *session.Manager) {
	defer s.wg.Done()
	tracker := NewTracker(s.cfg.Kinds)
	for {
		sub := m.Subscribe()
		lagged := false
		for !lagged {
			select {
			case <-s.ctx.Done():
				m.Unsubscribe(sub)
				return
			case <-sub.Slow:
				// Events after the overflow were dropped; the next snapshot
				// is compared against the kept baseline instead.
				m.Unsubscribe(sub)
				lagged = true
			case e := <-sub.Out:
				for _, alert := range tracker.Observe(e) {
					s.enqueue(alert)
				}
			}
		}
		s.log.Warn("push fell behind the session list; resubscribing")
	}
}

func (s *Service) enqueue(alert Alert) {
	select {
	case s.queue <- alert:
	default:
		s.log.Warn("push queue full; alert dropped", "session", alert.SessionID, "kind", alert.Kind)
	}
}

func (s *Service) work() {
	defer s.wg.Done()
	for {
		select {
		case <-s.ctx.Done():
			return
		case alert := <-s.queue:
			// Serial within one alert: at most 32 requests, and no burst
			// against a single push service.
			for _, sub := range s.store.List() {
				if s.ctx.Err() != nil {
					return
				}
				body, err := payload(alert, sub.Origin, s.cfg.IncludeTitle)
				if err != nil {
					s.log.Error("push payload", "error", err)
					continue
				}
				_ = s.deliver(s.ctx, sub, body, urgency(alert.Kind))
			}
		}
	}
}

// deliver encrypts and posts one message. 404/410 delete the subscription;
// other failures keep it and are not retried. Errors and logs name only the
// subscription ID and the push service host, never the endpoint.
func (s *Service) deliver(ctx context.Context, sub Subscription, payload []byte, urgency string) error {
	host := endpointHost(sub.Endpoint)
	err := s.post(ctx, sub, payload, urgency)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrGone):
		if removeErr := s.store.Remove(sub.ID); removeErr != nil {
			s.log.Warn("push subscription expired but could not be removed", "subscription", sub.ID, "error", removeErr)
		} else {
			s.log.Info("push subscription expired; removed", "subscription", sub.ID, "service", host)
		}
	case ctx.Err() != nil:
		// Shutdown or a caller that gave up; nothing to report.
	default:
		s.log.Warn("push delivery failed", "subscription", sub.ID, "service", host, "error", err)
	}
	return err
}

func (s *Service) post(ctx context.Context, sub Subscription, payload []byte, urgency string) error {
	body, err := encryptFresh(payload, sub)
	if err != nil {
		return err
	}
	key, publicKey := s.store.signer()
	auth, err := vapidHeader(sub.Endpoint, s.cfg.Subject, key, publicKey, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("endpoint 无效")
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.Itoa(s.cfg.TTLSeconds))
	req.Header.Set("Urgency", urgency)
	req.Header.Set("Authorization", auth)
	resp, err := s.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // url.Error repeats the full endpoint
		}
		return fmt.Errorf("推送服务 %s 请求失败: %w", endpointHost(sub.Endpoint), err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	}
	return fmt.Errorf("推送服务 %s 返回 %d", endpointHost(sub.Endpoint), resp.StatusCode)
}

func endpointHost(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "?"
	}
	return u.Host
}
