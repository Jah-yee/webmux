package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rainchan/webmux/internal/session"
)

type identity struct {
	root        os.FileInfo
	admin       os.FileInfo
	common      os.FileInfo
	gitFile     os.FileInfo
	adminPath   string
	gitContents string
	// Holding the objects prevents inode reuse from making a removed/recreated
	// checkout compare equal to an earlier confirmation.
	handles []*os.File
}

func (id *identity) hold(path string) (os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	id.handles = append(id.handles, file)
	return file.Stat()
}
func (id *identity) close() {
	for _, file := range id.handles {
		_ = file.Close()
	}
	id.handles = nil
}

type removalState struct {
	tree      Worktree
	identity  identity
	sessions  []session.Info
	blockers  []string
	supported bool
}
type confirmation struct {
	repositoryID string
	worktreeID   string
	expires      time.Time
	state        removalState
	timer        *time.Timer
}

func (s *Service) identify(ctx context.Context, r *repositoryState, tree Worktree) (id identity, err error) {
	defer func() {
		if err != nil {
			id.close()
		}
	}()
	id.root, err = id.hold(tree.Path)
	if err != nil {
		return id, err
	}
	path, err := canonical(tree.Path)
	if err != nil || path != tree.Path {
		return id, fmt.Errorf("工作树规范路径身份已改变: %v", err)
	}
	data, err := s.git(ctx, tree.Path, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return id, err
	}
	id.adminPath, err = canonical(outputLine(data))
	if err != nil {
		return id, err
	}
	if !within(r.entry.CommonDir, id.adminPath) || id.adminPath == r.entry.CommonDir {
		return id, fmt.Errorf("linked worktree 管理目录身份无效")
	}
	id.admin, err = id.hold(id.adminPath)
	if err != nil {
		return id, err
	}
	id.common, err = id.hold(r.entry.CommonDir)
	if err != nil {
		return id, err
	}
	file := filepath.Join(tree.Path, ".git")
	id.gitFile, err = os.Lstat(file)
	if err != nil {
		return id, err
	}
	if !id.gitFile.Mode().IsRegular() {
		return id, fmt.Errorf("linked worktree 的 .git 不是普通文件")
	}
	held, err := id.hold(file)
	if err != nil {
		return id, err
	}
	if !os.SameFile(id.gitFile, held) {
		return id, fmt.Errorf(".git 文件身份在读取期间改变")
	}
	contents, err := os.ReadFile(file)
	if err != nil {
		return id, err
	}
	id.gitContents = string(contents)
	return id, nil
}
func sameIdentity(a, b identity) bool {
	return a.root != nil && b.root != nil && a.admin != nil && b.admin != nil && a.common != nil && b.common != nil && a.gitFile != nil && b.gitFile != nil &&
		os.SameFile(a.root, b.root) && os.SameFile(a.admin, b.admin) && os.SameFile(a.common, b.common) && os.SameFile(a.gitFile, b.gitFile) && a.adminPath == b.adminPath && a.gitContents == b.gitContents
}
func sameText(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
func sameTarget(a, b removalState) bool {
	return sameIdentity(a.identity, b.identity) && a.supported == b.supported &&
		a.tree.ID == b.tree.ID && a.tree.Path == b.tree.Path && a.tree.Kind == b.tree.Kind &&
		sameText(a.tree.Branch, b.tree.Branch) && sameText(a.tree.Head, b.tree.Head) && a.tree.Detached == b.tree.Detached &&
		a.tree.Locked == b.tree.Locked && a.tree.LockReason == b.tree.LockReason && a.tree.Prunable == b.tree.Prunable && a.tree.Available == b.tree.Available
}
func (s *Service) removalState(ctx context.Context, r *repositoryState, worktreeID string) (removalState, error) {
	repository, err := s.current(ctx, r)
	if err != nil {
		return removalState{}, err
	}
	tree, err := findTree(repository, worktreeID)
	if err != nil {
		return removalState{}, err
	}
	state := removalState{tree: tree, sessions: s.targetSessions(tree.Path), blockers: []string{}, supported: true}
	block := func(message string) { state.supported = false; state.blockers = append(state.blockers, message) }
	if tree.Kind != "linked" {
		block("主工作区和 bare 仓库不能在页面删除")
	}
	if tree.Locked {
		block("工作树已锁定，请先在终端解锁: " + tree.LockReason)
	}
	if !tree.Available || tree.Prunable {
		block("工作树目录缺失或不可验证，请在终端处理")
	}
	if tree.Dirty == nil || tree.Error != "" {
		block("工作树状态未知，不能确认安全删除: " + tree.Error)
	}
	if state.supported {
		state.identity, err = s.identify(ctx, r, tree)
		if err != nil {
			block("无法验证工作树身份: " + err.Error())
		}
		if err := s.unsupportedContents(ctx, tree); err != nil {
			block(err.Error())
		}
	}
	if !state.supported {
		state.identity.close()
	}
	if tree.Dirty != nil && *tree.Dirty {
		state.blockers = append(state.blockers, "存在未提交修改")
	}
	if tree.HasUntracked {
		state.blockers = append(state.blockers, "存在未跟踪文件")
	}
	if tree.HasIgnored {
		state.blockers = append(state.blockers, "存在忽略文件（可能包含 .env 或构建产物）")
	}
	for _, info := range state.sessions {
		if info.Status != session.StatusExited {
			state.blockers = append(state.blockers, "存在运行中的 WebMux 会话")
			break
		}
	}
	if ctx.Err() != nil {
		state.identity.close()
		return state, ctx.Err()
	}
	return state, nil
}
func (s *Service) PreviewRemoval(ctx context.Context, repositoryID, worktreeID string) (Preview, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	r, err := s.repository(repositoryID)
	if err != nil {
		return Preview{}, err
	}
	if err := acquire(ctx, r); err != nil {
		return Preview{}, err
	}
	defer release(r)
	state, err := s.removalState(ctx, r, worktreeID)
	if err != nil {
		return Preview{}, err
	}
	id, err := randomID()
	if err != nil {
		state.identity.close()
		return Preview{}, err
	}
	now := time.Now()
	expires := now.Add(confirmationLifetime)
	s.mu.Lock()
	if err := s.requireRegistryLocked(); err != nil {
		s.mu.Unlock()
		state.identity.close()
		return Preview{}, err
	}
	s.coord.Lock()
	for key, old := range s.confirmations {
		if !now.Before(old.expires) {
			old.timer.Stop()
			old.state.identity.close()
			delete(s.confirmations, key)
		}
	}
	timer := time.AfterFunc(confirmationLifetime, func() {
		s.coord.Lock()
		if expired, exists := s.confirmations[id]; exists {
			delete(s.confirmations, id)
			expired.state.identity.close()
		}
		s.coord.Unlock()
	})
	s.confirmations[id] = confirmation{repositoryID: repositoryID, worktreeID: worktreeID, expires: expires, state: state, timer: timer}
	s.coord.Unlock()
	s.mu.Unlock()
	return Preview{ConfirmationID: id, ExpiresAt: expires, Worktree: state.tree, Sessions: state.sessions, Blockers: state.blockers, ForceAllowed: state.supported}, nil
}
func (s *Service) RemoveWorktree(ctx context.Context, repositoryID, worktreeID string, req RemoveRequest) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if req.Mode != "safe" && req.Mode != "force" {
		return problem(ErrInvalid, "删除模式必须为 safe 或 force")
	}
	s.coord.Lock()
	confirmation, ok := s.confirmations[req.ConfirmationID]
	delete(s.confirmations, req.ConfirmationID)
	s.coord.Unlock()
	if ok {
		confirmation.timer.Stop()
		defer confirmation.state.identity.close()
	}
	if !ok || !time.Now().Before(confirmation.expires) || confirmation.repositoryID != repositoryID || confirmation.worktreeID != worktreeID {
		return problem(ErrConflict, "删除确认已失效，请重新预览")
	}
	if req.Mode == "force" && (req.ConfirmPath != confirmation.state.tree.Path || !req.CloseSessions) {
		return problem(ErrInvalid, "强制删除必须输入完整目标路径并明确授权关闭会话")
	}
	r, err := s.repository(repositoryID)
	if err != nil {
		return err
	}
	if err := acquire(ctx, r); err != nil {
		return err
	}
	defer release(r)
	path := confirmation.state.tree.Path
	if err := s.reserve(ctx, path); err != nil {
		return err
	}
	defer s.unreserve(path)
	// Every execution path releases the reservation and re-reads real Git state,
	// including cancellation after Git or session cleanup partially took effect.
	defer func() {
		refreshCtx, refreshCancel := context.WithTimeout(context.Background(), readTimeout)
		defer refreshCancel()
		s.readRepository(refreshCtx, r)
	}()
	current, err := s.removalState(ctx, r, worktreeID)
	if err != nil {
		return err
	}
	defer current.identity.close()
	if !current.supported {
		return problem(ErrConflict, "不支持删除此工作树: %v", current.blockers)
	}
	if !sameTarget(confirmation.state, current) || !sameSessions(confirmation.state.sessions, current.sessions, false) {
		return problem(ErrConflict, "工作树身份、分支、锁或会话集合已变化，请重新预览；尚未关闭任何会话")
	}
	if req.Mode == "safe" && len(current.blockers) != 0 {
		return problem(ErrConflict, "安全删除被阻止: %v", current.blockers)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if req.Mode == "force" {
		active := make([]*session.Session, 0, len(current.sessions))
		for _, info := range current.sessions {
			if info.Status == session.StatusExited {
				continue
			}
			item, err := s.manager.Get(info.ID)
			if err != nil {
				return problem(ErrConflict, "会话集合已变化，请重新预览")
			}
			active = append(active, item)
		}
		for _, item := range active {
			item.Close()
		}
		for _, item := range active {
			select {
			case <-item.Done():
			case <-ctx.Done():
				return problem(ErrTimeout, "关闭会话未在期限内完成，工作树未删除；部分会话可能已关闭")
			}
		}
	}
	final, err := s.removalState(ctx, r, worktreeID)
	if err != nil {
		return err
	}
	defer final.identity.close()
	if !final.supported || !sameTarget(current, final) || !sameSessions(current.sessions, final.sessions, req.Mode == "force") {
		return problem(ErrConflict, "关闭期间目标身份或会话集合变化，工作树未删除；部分会话可能已关闭")
	}
	for _, info := range final.sessions {
		if info.Status != session.StatusExited {
			return problem(ErrConflict, "仍有运行会话，工作树未删除")
		}
	}
	if req.Mode == "safe" && len(final.blockers) != 0 {
		return problem(ErrConflict, "安全删除被最新状态阻止: %v", final.blockers)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	argv := []string{"worktree", "remove"}
	if req.Mode == "force" {
		argv = append(argv, "--force")
	}
	argv = append(argv, "--", path)
	if _, err := s.git(ctx, r.entry.AnchorPath, argv...); err != nil {
		return problem(errorKind(err), "%v；删除未完整成功，部分会话可能已关闭，请刷新实际状态", err)
	}
	return nil
}

func (s *Service) clearConfirmations() {
	s.coord.Lock()
	defer s.coord.Unlock()
	for id, confirmation := range s.confirmations {
		confirmation.timer.Stop()
		confirmation.state.identity.close()
		delete(s.confirmations, id)
	}
}
