package tray

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/localclient"
)

var (
	// ErrWebmuxMissing means the configured webmux path does not exist in WSL.
	ErrWebmuxMissing = errors.New("找不到 webmux")
	// ErrWebmuxTooOld means webmux lacks the local API, bridge and status v1.
	ErrWebmuxTooOld = errors.New("WSL 中的 webmux 版本过旧，请升级")
	// ErrNoWSL means wsl.exe or any distribution is unavailable.
	ErrNoWSL = errors.New("未检测到 WSL 发行版")
)

// Service is everything the supervisor does inside WSL.
type Service interface {
	// Check verifies that webmux exists and supports the local API.
	Check(ctx context.Context) error
	Detect(ctx context.Context) Detection
	// Status uses the local API, falling back to `webmux status --json`
	// when the bridge reports that WebMux is not running.
	Status(ctx context.Context) (contract.StatusV1, error)
	// Watch follows /ws/sessions, reporting the session count, until the
	// subscription ends.
	Watch(ctx context.Context, sessions func(int)) error
	StartTray(ctx context.Context) (Process, error)
	Shutdown(ctx context.Context, force bool) error
	ForceStop(ctx context.Context) (contract.StopV1, error)
	Systemctl(ctx context.Context, action string) error
	UnitFailed(ctx context.Context) bool
	Keepalive(on bool) error
	// Token reads the token for one clipboard copy; callers discard it.
	Token(ctx context.Context) (string, error)
	ConfigInit(ctx context.Context) error
}

// WSLService implements Service with wsl.exe through a Runner and the
// local API through a bridge dialer.
type WSLService struct {
	Runner  Runner
	Target  Target
	LogFile string
	// Environ is the tray's environment, used to extend WSLENV.
	Environ []string
	// Dial builds the bridge dialer for a target.
	Dial      func(Target) localclient.Dialer
	KeepAlive *Keepalive

	once   sync.Once
	client *localclient.Client
}

func (s *WSLService) local() *localclient.Client {
	s.once.Do(func() {
		s.client = localclient.New(s.Dial(s.Target))
	})
	return s.client
}

// Close drops idle bridge connections.
func (s *WSLService) Close() {
	if s.client != nil {
		s.client.Close()
	}
}

func (s *WSLService) run(ctx context.Context, inv Invocation, timeout time.Duration) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return s.Runner.Run(ctx, inv.Argv, inv.Env)
}

// execFailure recognizes wsl.exe failing to execute the program.
func execFailure(res Result) bool {
	text := strings.ToLower(DecodeWSLOutput(res.Stderr) + DecodeWSLOutput(res.Stdout))
	return res.Code != 0 && (strings.Contains(text, "no such file") || strings.Contains(text, "execvpe") || strings.Contains(text, "permission denied"))
}

func (s *WSLService) Check(ctx context.Context) error {
	res, err := s.run(ctx, s.Target.Probe(), 30*time.Second)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNoWSL, err)
	}
	if execFailure(res) {
		return fmt.Errorf("%w %s", ErrWebmuxMissing, s.Target.Webmux)
	}
	if !strings.Contains(DecodeWSLOutput(res.Stderr)+DecodeWSLOutput(res.Stdout), "-log-file") {
		return ErrWebmuxTooOld
	}
	return nil
}

func (s *WSLService) Detect(ctx context.Context) Detection {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return DetectSystemd(ctx, s.Runner, s.Target)
}

func (s *WSLService) Status(ctx context.Context) (contract.StatusV1, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	status, err := s.local().Status(requestCtx)
	cancel()
	if err == nil {
		return status, nil
	}
	if !errors.Is(err, localclient.ErrNotRunning) {
		return contract.StatusV1{}, err
	}
	res, err := s.run(ctx, s.Target.Status(), 10*time.Second)
	if err != nil {
		return contract.StatusV1{}, err
	}
	if execFailure(res) {
		return contract.StatusV1{}, fmt.Errorf("%w %s", ErrWebmuxMissing, s.Target.Webmux)
	}
	if err := json.Unmarshal(res.Stdout, &status); err != nil || status.V == 0 {
		return contract.StatusV1{}, fmt.Errorf("%w: status --json: %s", ErrWebmuxTooOld, strings.TrimSpace(DecodeWSLOutput(res.Stderr)))
	}
	if status.V > contract.V {
		return contract.StatusV1{}, fmt.Errorf("WSL 中的 webmux 使用状态契约 v%d，请升级托盘", status.V)
	}
	return status, nil
}

func (s *WSLService) Watch(ctx context.Context, sessions func(int)) error {
	conn, err := s.local().DialWebSocket(ctx, "/ws/sessions")
	if err != nil {
		return err
	}
	defer func() { _ = conn.CloseNow() }()
	conn.SetReadLimit(16 << 20)
	ids := map[string]bool{}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		var event struct {
			Type     string `json:"type"`
			Sessions []struct {
				ID string `json:"id"`
			} `json:"sessions"`
			Session struct {
				ID string `json:"id"`
			} `json:"session"`
			ID string `json:"id"`
		}
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		switch event.Type {
		case "snapshot":
			ids = map[string]bool{}
			for _, item := range event.Sessions {
				ids[item.ID] = true
			}
		case "created", "updated":
			ids[event.Session.ID] = true
		case "removed":
			delete(ids, event.ID)
		default:
			continue
		}
		sessions(len(ids))
	}
}

func (s *WSLService) StartTray(context.Context) (Process, error) {
	inv := s.Target.Serve(s.LogFile, s.Environ)
	return s.Runner.Start(inv.Argv, inv.Env)
}

func (s *WSLService) Shutdown(ctx context.Context, force bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.local().Shutdown(ctx, force)
}

func (s *WSLService) ForceStop(ctx context.Context) (contract.StopV1, error) {
	res, err := s.run(ctx, s.Target.ForceStop(), 30*time.Second)
	if err != nil {
		return contract.StopV1{}, err
	}
	var stop contract.StopV1
	if err := json.Unmarshal(res.Stdout, &stop); err != nil {
		return contract.StopV1{}, fmt.Errorf("webmux stop: exit %d: %s", res.Code, strings.TrimSpace(DecodeWSLOutput(res.Stderr)))
	}
	if res.Code != 0 {
		return stop, fmt.Errorf("webmux stop: %s", stop.Message)
	}
	return stop, nil
}

func (s *WSLService) Systemctl(ctx context.Context, action string) error {
	res, err := s.run(ctx, s.Target.Systemctl(action), 60*time.Second)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("systemctl --user %s webmux.service: exit %d: %s", action, res.Code, strings.TrimSpace(DecodeWSLOutput(res.Stderr)))
	}
	return nil
}

func (s *WSLService) UnitFailed(ctx context.Context) bool {
	res, err := s.run(ctx, s.Target.UnitFailed(), 10*time.Second)
	return err == nil && res.Code == 0
}

func (s *WSLService) Keepalive(on bool) error {
	if s.KeepAlive == nil {
		return nil
	}
	if on {
		return s.KeepAlive.Ensure(s.Target)
	}
	return s.KeepAlive.Stop()
}

func (s *WSLService) Token(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.local().Token(ctx)
}

func (s *WSLService) ConfigInit(ctx context.Context) error {
	res, err := s.run(ctx, s.Target.ConfigInit(), 30*time.Second)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("webmux config init: %s", strings.TrimSpace(DecodeWSLOutput(res.Stderr)))
	}
	return nil
}

// Keepalive keeps a distribution with a systemd-managed WebMux alive through
// a detached `sleep infinity` that survives the tray. Its PID and creation
// identity are stored so a restarted tray can find and end it.
type Keepalive struct {
	Runner Runner
	// Path stores {"pid", "identity"}.
	Path string
	// Identify returns a value that changes when a PID is reused.
	Identify func(pid int) (string, bool)
	Kill     func(pid int) error

	mu sync.Mutex
}

type keepaliveRecord struct {
	PID      int    `json:"pid"`
	Identity string `json:"identity"`
}

func (k *Keepalive) load() (keepaliveRecord, bool) {
	data, err := os.ReadFile(k.Path)
	if err != nil {
		return keepaliveRecord{}, false
	}
	var r keepaliveRecord
	if json.Unmarshal(data, &r) != nil || r.PID <= 0 {
		return keepaliveRecord{}, false
	}
	identity, ok := k.Identify(r.PID)
	return r, ok && identity == r.Identity
}

// Ensure starts the keepalive unless the recorded one still runs.
func (k *Keepalive) Ensure(t Target) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, alive := k.load(); alive {
		return nil
	}
	inv := t.Keepalive()
	p, err := k.Runner.Start(inv.Argv, inv.Env)
	if err != nil {
		return err
	}
	identity, _ := k.Identify(p.PID())
	data, _ := json.Marshal(keepaliveRecord{PID: p.PID(), Identity: identity})
	if err := os.MkdirAll(filepath.Dir(k.Path), 0700); err != nil {
		return err
	}
	return os.WriteFile(k.Path, data, 0600)
}

// Stop ends the recorded keepalive if it is still the same process.
func (k *Keepalive) Stop() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	r, alive := k.load()
	_ = os.Remove(k.Path)
	if !alive {
		return nil
	}
	return k.Kill(r.PID)
}
