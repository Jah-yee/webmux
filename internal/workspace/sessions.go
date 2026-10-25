package workspace

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/rainchan/webmux/internal/session"
)

func (s *Service) notifyLocked() { close(s.changed); s.changed = make(chan struct{}) }
func (s *Service) beginStart(path string) error {
	s.coord.Lock()
	defer s.coord.Unlock()
	for reserved := range s.reservations {
		if within(reserved, path) {
			return problem(ErrConflict, "目标工作树正在删除，请稍后刷新")
		}
	}
	s.starts[path]++
	return nil
}
func (s *Service) endStart(path string) {
	s.coord.Lock()
	defer s.coord.Unlock()
	s.starts[path]--
	if s.starts[path] == 0 {
		delete(s.starts, path)
	}
	s.notifyLocked()
}
func (s *Service) reserve(ctx context.Context, path string) error {
	s.coord.Lock()
	for reserved := range s.reservations {
		if overlap(reserved, path) {
			s.coord.Unlock()
			return problem(ErrConflict, "重叠工作树已有删除操作")
		}
	}
	s.reservations[path] = true
	for {
		starting := false
		for cwd := range s.starts {
			if within(path, cwd) {
				starting = true
				break
			}
		}
		if !starting {
			s.coord.Unlock()
			return nil
		}
		changed := s.changed
		s.coord.Unlock()
		select {
		case <-ctx.Done():
			s.unreserve(path)
			return ctx.Err()
		case <-changed:
		}
		s.coord.Lock()
	}
}
func (s *Service) unreserve(path string) {
	s.coord.Lock()
	defer s.coord.Unlock()
	delete(s.reservations, path)
	s.notifyLocked()
}
func (s *Service) association(ctx context.Context, cwd string) *session.Workspace {
	if s.gitPath == "" {
		return nil
	}
	common, err := s.common(ctx, cwd)
	if err != nil {
		return nil
	}
	root, err := s.git(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	path, err := canonical(outputLine(root))
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.capability.RegistryAvailable {
		return nil
	}
	for _, r := range s.entries {
		if r.entry.CommonDir == common {
			return &session.Workspace{RepositoryID: r.entry.ID, WorktreeID: worktreeID(r.entry.ID, path), WorktreePath: path}
		}
	}
	return nil
}

// Association commits are ordered with registry removal. Git resolution never
// holds the registry/session locks, and a removed registration cannot reappear.
func (s *Service) commitAssociation(info session.Info, association *session.Workspace, preserve bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	registered := false
	id := ""
	if association != nil {
		id = association.RepositoryID
	} else if preserve && info.Workspace != nil {
		id = info.Workspace.RepositoryID
	}
	for _, r := range s.entries {
		if r.entry.ID == id {
			registered = true
			break
		}
	}
	if !registered {
		association = nil
	} else if association == nil && preserve {
		association = info.Workspace
	}
	s.manager.SetWorkspace(info.ID, info.Cwd, association)
}
func (s *Service) reassociate(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	for _, info := range s.manager.List() {
		if ctx.Err() != nil {
			return
		}
		association := s.association(ctx, info.Cwd)
		// If the original checkout disappeared, retain its identity instead of
		// silently rebinding the record to a newly created checkout at that path.
		if info.Workspace != nil && association != nil && *association != *info.Workspace {
			association = nil
		}
		s.commitAssociation(info, association, true)
	}
}

// SessionInput describes a session to start. ConfigID selects a saved launch
// config instead of Preset, Cmd, Args and Env.
type SessionInput struct {
	Cwd       string
	Preset    string
	Cmd       []string
	Args      []string
	Name      string
	Workspace *Reference
	Env       map[string]string
	ConfigID  *string
}

// resolveCommand applies a saved launch config and validates the explicit
// environment. It runs before any filesystem or Git work.
func (s *Service) resolveCommand(in SessionInput) (preset string, cmd, args []string, env map[string]string, err error) {
	preset, cmd, args, env = in.Preset, in.Cmd, in.Args, in.Env
	if in.ConfigID != nil {
		if *in.ConfigID == "" {
			return "", nil, nil, nil, problem(ErrInvalid, "configId must not be empty")
		}
		if in.Preset != "" || len(in.Cmd) > 0 || len(in.Args) > 0 || len(in.Env) > 0 {
			return "", nil, nil, nil, problem(ErrInvalid, "configId cannot be combined with preset, cmd, args or env")
		}
		// A nil store answers launch.ErrUnavailable.
		saved, err := s.launches.Get(*in.ConfigID)
		if err != nil {
			return "", nil, nil, nil, err
		}
		preset, cmd, args, env = "custom", saved.Cmd, nil, saved.Env
	}
	if err := session.ValidateEnv(env); err != nil {
		return "", nil, nil, nil, problem(ErrInvalid, "%v", err)
	}
	return preset, cmd, args, env, nil
}

// CreateSession starts a session in the selected directory or worktree.
// Launch config errors keep their launch error kinds.
func (s *Service) CreateSession(ctx context.Context, in SessionInput) (*session.Session, error) {
	preset, cmd, args, env, err := s.resolveCommand(in)
	if err != nil {
		return nil, err
	}
	cwd, name, reference := in.Cwd, in.Name, in.Workspace
	requestCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	var selected *Worktree
	if reference != nil {
		r, err := s.repository(reference.RepositoryID)
		if err != nil {
			return nil, err
		}
		// Do not hold a repository operation gate while registering an in-flight
		// launch: removal holds that gate and waits for earlier launches to finish.
		snapshot, err := s.snapshot(ctx, r)
		if err != nil {
			return nil, err
		}
		if snapshot.Availability != "available" {
			return nil, problem(ErrUnavailable, "%s", snapshot.Error)
		}
		tree, err := findTree(snapshot, reference.WorktreeID)
		if err != nil {
			return nil, err
		}
		if !tree.Available || tree.Kind == "bare" {
			return nil, problem(ErrConflict, "所选工作树不可作为启动目录: %s", tree.Error)
		}
		selected = &tree
		if cwd == "" {
			cwd = tree.Path
		}
	}
	if cwd == "" {
		cwd = s.defaultCwd
	}
	path, err := canonical(cwd)
	if err != nil {
		return nil, problem(ErrInvalid, "directory: %v", err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, problem(ErrInvalid, "directory: %v", err)
	}
	if !stat.IsDir() {
		return nil, problem(ErrInvalid, "path is not a directory")
	}
	if selected != nil && !within(selected.Path, path) {
		return nil, problem(ErrInvalid, "cwd 与所选工作树不一致")
	}
	if err := s.beginStart(path); err != nil {
		return nil, err
	}
	defer s.endStart(path)
	association := s.association(ctx, path)
	if selected != nil && (association == nil || association.RepositoryID != reference.RepositoryID || association.WorktreeID != reference.WorktreeID) {
		return nil, problem(ErrConflict, "所选工作树身份已变化或 cwd 属于嵌套仓库，请刷新")
	}
	if err := requestCtx.Err(); err != nil {
		return nil, err
	}
	item, err := s.manager.Create(path, preset, cmd, args, name, env)
	if err != nil {
		return nil, problem(ErrInvalid, "启动会话失败: %v", err)
	}
	s.commitAssociation(item.Info(), association, false)
	return item, nil
}
func (s *Service) targetSessions(path string) []session.Info {
	result := make([]session.Info, 0)
	for _, info := range s.manager.List() {
		cwd, err := canonicalTarget(info.Cwd)
		if err != nil {
			cwd = filepath.Clean(info.Cwd)
		}
		if within(path, cwd) || within(path, info.Cwd) {
			result = append(result, info)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
func sameSessions(a, b []session.Info, allowExit bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Cwd != b[i].Cwd {
			return false
		}
		if !allowExit && (a[i].Status == session.StatusExited) != (b[i].Status == session.StatusExited) {
			return false
		}
	}
	return true
}
