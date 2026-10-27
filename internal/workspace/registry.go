package workspace

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/rainchan/webmux/internal/config"
	"github.com/rainchan/webmux/internal/launch"
	"github.com/rainchan/webmux/internal/pathutil"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/statefile"
)

type registration struct {
	ID          string `json:"id"`
	CommonDir   string `json:"commonDir"`
	AnchorPath  string `json:"anchorPath"`
	DisplayName string `json:"displayName"`
}
type registry struct {
	Version      int            `json:"version"`
	Repositories []registration `json:"repositories"`
}
type repositoryState struct {
	entry    registration
	gate     chan struct{}
	flightMu sync.Mutex
	flight   *snapshotFlight
}
type snapshotFlight struct {
	done       chan struct{}
	repository Repository
	err        error
	cancel     context.CancelFunc
	waiters    int
}
type Service struct {
	manager    *session.Manager
	launches   *launch.Service
	defaultCwd string
	gitPath    string
	stateDir   string
	mu         sync.Mutex
	// saveMu serializes registry read-modify-write cycles without holding mu
	// across the state file commit.
	saveMu        sync.Mutex
	capability    Capability
	entries       []*repositoryState
	store         *statefile.Handle[registry]
	coord         sync.Mutex
	starts        map[string]int
	reservations  map[string]bool
	changed       chan struct{}
	confirmations map[string]confirmation
}

// New never makes optional Git or storage availability a server startup failure.
// launches resolves saved launch configs; nil makes them unavailable.
func New(stateDir string, manager *session.Manager, launches *launch.Service, defaultCwd string) *Service {
	if stateDir == "" {
		stateDir = config.DefaultStateDir()
	}
	s := &Service{manager: manager, launches: launches, defaultCwd: defaultCwd, stateDir: stateDir, starts: make(map[string]int), reservations: make(map[string]bool), changed: make(chan struct{}), confirmations: make(map[string]confirmation)}
	git, err := exec.LookPath("git")
	if err != nil {
		s.capability.GitError = "Git 不可用: " + err.Error()
	} else {
		s.gitPath = git
		s.capability.GitAvailable = true
	}
	if err := s.load(); err != nil {
		s.capability.RegistryError = "仓库清单不可用: " + err.Error()
	} else {
		s.capability.RegistryAvailable = true
	}
	return s
}
func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store.Close()
	s.capability.RegistryAvailable = false
	s.capability.RegistryError = "仓库服务已关闭"
	s.clearConfirmations()
}
func (s *Service) Capability() Capability { s.mu.Lock(); defer s.mu.Unlock(); return s.capability }
func (s *Service) load() error {
	expanded, err := pathutil.Expand(s.stateDir)
	if err != nil {
		return err
	}
	s.stateDir = expanded
	var stored registry
	s.store, stored, err = statefile.Open[registry](s.stateDir, statefile.Repositories, validateRegistry)
	if statefile.IsKind(err, statefile.Missing) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range stored.Repositories {
		s.entries = append(s.entries, newRepository(entry))
	}
	return nil
}
func validateRegistry(stored registry) error {
	if stored.Version != 1 || stored.Repositories == nil {
		return errors.New("不支持或损坏的仓库清单")
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	for _, entry := range stored.Repositories {
		if entry.ID == "" || !filepath.IsAbs(entry.CommonDir) || !filepath.IsAbs(entry.AnchorPath) || ids[entry.ID] || paths[entry.CommonDir] {
			return errors.New("仓库清单身份无效或重复")
		}
		ids[entry.ID], paths[entry.CommonDir] = true, true
	}
	return nil
}
func newRepository(entry registration) *repositoryState {
	r := &repositoryState{entry: entry, gate: make(chan struct{}, 1)}
	r.gate <- struct{}{}
	return r
}
func (s *Service) save(entries []*repositoryState) error {
	stored := registry{Version: 1, Repositories: make([]registration, 0, len(entries))}
	for _, entry := range entries {
		stored.Repositories = append(stored.Repositories, entry.entry)
	}
	err := s.store.Commit(stored)
	if statefile.IsKind(err, statefile.CommitUncertain) {
		// Rename already published different bytes. Keep the last confirmed
		// memory state, but never allow it to overwrite the uncertain disk state.
		message := "清单已替换但目录同步失败，持久结果不确定；仓库管理已停用，请重启重新读取: " + err.Error()
		s.mu.Lock()
		s.capability.RegistryAvailable = false
		s.capability.RegistryError = message
		s.mu.Unlock()
		return errors.New(message)
	}
	return err
}
func (s *Service) requireRegistryLocked() error {
	if !s.capability.RegistryAvailable {
		return problemf(ErrUnavailable, "%s", s.capability.RegistryError)
	}
	return nil
}
func (s *Service) repository(id string) (*repositoryState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireRegistryLocked(); err != nil {
		return nil, err
	}
	for _, r := range s.entries {
		if r.entry.ID == id {
			return r, nil
		}
	}
	return nil, problemf(ErrNotFound, "仓库不存在，请刷新")
}
func (s *Service) registered(r *repositoryState) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, candidate := range s.entries {
		if candidate == r {
			return true
		}
	}
	return false
}
func (s *Service) Register(ctx context.Context, path string) (Repository, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	if err := s.requireRegistry(); err != nil {
		return Repository{}, false, err
	}
	if path == "" {
		return Repository{}, false, problemf(ErrInvalid, "请选择仓库目录")
	}
	common, anchor, err := s.locate(ctx, path)
	if err != nil {
		return Repository{}, false, err
	}
	s.saveMu.Lock()
	s.mu.Lock()
	if err := s.requireRegistryLocked(); err != nil {
		s.mu.Unlock()
		s.saveMu.Unlock()
		return Repository{}, false, err
	}
	for _, r := range s.entries {
		if r.entry.CommonDir == common {
			s.mu.Unlock()
			s.saveMu.Unlock()
			snapshot, err := s.snapshot(ctx, r)
			s.reassociate(ctx)
			return snapshot, false, err
		}
	}
	id, err := randomID()
	if err != nil {
		s.mu.Unlock()
		s.saveMu.Unlock()
		return Repository{}, false, err
	}
	r := newRepository(registration{ID: id, CommonDir: common, AnchorPath: anchor, DisplayName: filepath.Base(anchor)})
	next := append(append([]*repositoryState(nil), s.entries...), r)
	s.mu.Unlock()
	err = s.save(next)
	if err == nil {
		s.mu.Lock()
		s.entries = next
		s.mu.Unlock()
	}
	s.saveMu.Unlock()
	if err != nil {
		return Repository{}, false, fmt.Errorf("保存仓库清单: %w", err)
	}
	snapshot, err := s.snapshot(ctx, r)
	s.reassociate(ctx)
	return snapshot, true, err
}
func (s *Service) requireRegistry() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requireRegistryLocked()
}
func (s *Service) Unregister(id string) error {
	r, err := s.repository(id)
	if err != nil {
		return err
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	if err := s.requireRegistryLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	next := make([]*repositoryState, 0, len(s.entries))
	found := false
	for _, item := range s.entries {
		if item == r {
			found = true
		} else {
			next = append(next, item)
		}
	}
	if !found {
		s.mu.Unlock()
		return problemf(ErrNotFound, "仓库不存在")
	}
	s.mu.Unlock()
	if err := s.save(next); err != nil {
		return fmt.Errorf("保存仓库清单: %w", err)
	}
	s.mu.Lock()
	s.entries = next
	s.mu.Unlock()
	for _, info := range s.manager.List() {
		if info.Workspace != nil && info.Workspace.RepositoryID == id {
			s.manager.SetWorkspace(info.ID, info.Cwd, nil)
		}
	}
	return nil
}
func (s *Service) List(ctx context.Context) ([]Repository, error) {
	s.mu.Lock()
	if err := s.requireRegistryLocked(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	entries := append([]*repositoryState(nil), s.entries...)
	s.mu.Unlock()
	out := make([]Repository, 0, len(entries))
	for _, r := range entries {
		snapshot, err := s.snapshot(ctx, r)
		if err != nil {
			return nil, err
		}
		if s.registered(r) {
			out = append(out, snapshot)
		}
	}
	s.reassociate(ctx)
	return out, nil
}
func (s *Service) Refresh(ctx context.Context, id string) (Repository, error) {
	r, err := s.repository(id)
	if err != nil {
		return Repository{}, err
	}
	snapshot, err := s.snapshot(ctx, r)
	if err == nil {
		s.reassociate(ctx)
	}
	return snapshot, err
}
func acquire(ctx context.Context, r *repositoryState) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.gate:
		return nil
	}
}
func release(r *repositoryState) { r.gate <- struct{}{} }
