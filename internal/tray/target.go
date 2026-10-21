package tray

import (
	"context"
	"sync"

	"github.com/rainchan/webmux/internal/contract"
)

// TargetService is the supervisor's Service. A WSLService is bound
// to one distribution and set of paths, while settings change at runtime:
// the supervisor stops a running instance through the old target and then
// starts through the new one, and path changes apply at the next start. So
// a retarget either applies now (nothing runs that the old target must
// still reach) or waits for the next Check, which every start begins with.
type TargetService struct {
	build func(Settings) *WSLService

	mu      sync.Mutex
	cur     *WSLService
	pending *Settings
}

func NewTargetService(build func(Settings) *WSLService, s Settings) *TargetService {
	return &TargetService{build: build, cur: build(s)}
}

// Current is the service for wsl.exe calls outside the supervisor.
func (t *TargetService) Current() *WSLService {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.cur
}

func (t *TargetService) Retarget(s Settings, now bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !now {
		t.pending = &s
		return
	}
	t.pending = nil
	t.swap(s)
}

// Settle applies a pending retarget once nothing runs; it reports whether
// the target changed so the caller can refresh the status.
func (t *TargetService) Settle() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		return false
	}
	before := t.cur
	t.swap(*t.pending)
	t.pending = nil
	return t.cur != before
}

func (t *TargetService) swap(s Settings) {
	next := t.build(s)
	if t.cur.Target == next.Target && t.cur.LogFile == next.LogFile {
		return
	}
	t.cur.Close()
	t.cur = next
}

func (t *TargetService) Check(ctx context.Context) (string, error) {
	t.mu.Lock()
	if t.pending != nil {
		t.swap(*t.pending)
		t.pending = nil
	}
	t.mu.Unlock()
	return t.Current().Check(ctx)
}

func (t *TargetService) Detect(ctx context.Context) Detection {
	return t.Current().Detect(ctx)
}

func (t *TargetService) Status(ctx context.Context) (contract.StatusV1, error) {
	return t.Current().Status(ctx)
}

func (t *TargetService) Watch(ctx context.Context, sessions func(int)) error {
	return t.Current().Watch(ctx, sessions)
}

func (t *TargetService) StartTray(ctx context.Context) (Process, error) {
	return t.Current().StartTray(ctx)
}

func (t *TargetService) Shutdown(ctx context.Context, force bool) error {
	return t.Current().Shutdown(ctx, force)
}

func (t *TargetService) ForceStop(ctx context.Context) (contract.StopV1, error) {
	return t.Current().ForceStop(ctx)
}

func (t *TargetService) Systemctl(ctx context.Context, action string) error {
	return t.Current().Systemctl(ctx, action)
}

func (t *TargetService) UnitFailed(ctx context.Context) bool {
	return t.Current().UnitFailed(ctx)
}

func (t *TargetService) Keepalive(on bool) error {
	return t.Current().Keepalive(on)
}

func (t *TargetService) Token(ctx context.Context) (string, error) {
	return t.Current().Token(ctx)
}

func (t *TargetService) ConfigInit(ctx context.Context) error {
	return t.Current().ConfigInit(ctx)
}

func (t *TargetService) Install(ctx context.Context) error {
	return t.Current().Install(ctx)
}

func (t *TargetService) SetTunnel(ctx context.Context, enabled bool) (contract.TunnelStatus, error) {
	return t.Current().SetTunnel(ctx, enabled)
}

func (t *TargetService) Close() {
	t.Current().Close()
}
