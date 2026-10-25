package workspace

import (
	"context"
	"errors"
	"os"
	"strings"
)

func (s *Service) CreateWorktree(ctx context.Context, repositoryID string, req CreateRequest) (Worktree, error) {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	r, err := s.repository(repositoryID)
	if err != nil {
		return Worktree{}, err
	}
	if err := acquire(ctx, r); err != nil {
		return Worktree{}, err
	}
	defer release(r)
	repository, err := s.current(ctx, r)
	if err != nil {
		return Worktree{}, err
	}
	if req.Mode != "new" && req.Mode != "existing" {
		return Worktree{}, problem(ErrInvalid, "请选择新分支或已有本地分支")
	}
	if req.Branch == "" || strings.HasPrefix(req.Branch, "-") || strings.ContainsRune(req.Branch, '\x00') {
		return Worktree{}, problem(ErrInvalid, "分支名无效")
	}
	validatedBranch, err := s.git(ctx, r.entry.AnchorPath, "check-ref-format", "--branch", req.Branch)
	if err != nil {
		if errorKind(err) != ErrInternal {
			return Worktree{}, err
		}
		return Worktree{}, problem(ErrInvalid, "分支名无效: %s", req.Branch)
	}
	if outputLine(validatedBranch) != req.Branch {
		return Worktree{}, problem(ErrInvalid, "请使用明确分支名，不使用 checkout 简写")
	}
	if req.Path == "" {
		return Worktree{}, problem(ErrInvalid, "请明确选择工作树目标路径")
	}
	path, err := canonicalTarget(req.Path)
	if err != nil {
		return Worktree{}, problem(ErrInvalid, "目标路径无效: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return Worktree{}, problem(ErrConflict, "目标路径已存在或无法确认可用，不会覆盖")
	}
	// Read other registered Git inventories without taking their operation gates;
	// otherwise simultaneous creations in two repositories could deadlock.
	s.mu.Lock()
	entries := append([]*repositoryState(nil), s.entries...)
	s.mu.Unlock()
	for _, other := range entries {
		if overlap(other.entry.AnchorPath, path) || overlap(other.entry.CommonDir, path) {
			return Worktree{}, problem(ErrConflict, "目标与已注册仓库目录相互嵌套: %s", other.entry.AnchorPath)
		}
		trees := repository.Worktrees
		if other != r {
			data, err := s.git(ctx, other.entry.AnchorPath, "worktree", "list", "--porcelain", "-z")
			if err != nil {
				continue
			}
			trees, err = parseWorktrees(data, other.entry.ID)
			if err != nil {
				continue
			}
		}
		for _, tree := range trees {
			if overlap(tree.Path, path) {
				return Worktree{}, problem(ErrConflict, "目标与已有工作树相互嵌套: %s", tree.Path)
			}
		}
	}
	branches, err := s.branches(ctx, r, repository)
	if err != nil {
		return Worktree{}, err
	}
	exists := false
	for _, branch := range branches.Branches {
		if branch.Name == req.Branch {
			exists = true
			if req.Mode == "existing" && len(branch.WorktreePaths) > 0 {
				return Worktree{}, problem(ErrConflict, "分支已被工作树占用: %s", req.Branch)
			}
		}
	}
	argv := []string{"worktree", "add", "--no-guess-remote"}
	if req.Mode == "new" {
		if exists {
			return Worktree{}, problem(ErrConflict, "分支已存在，请选择已有本地分支模式")
		}
		if req.Base == "" || strings.HasPrefix(req.Base, "-") || strings.ContainsRune(req.Base, '\x00') {
			return Worktree{}, problem(ErrInvalid, "请选择明确的本地提交基线")
		}
		oid, err := s.git(ctx, r.entry.AnchorPath, "rev-parse", "--verify", "--end-of-options", req.Base+"^{commit}")
		if err != nil {
			if errorKind(err) != ErrInternal {
				return Worktree{}, err
			}
			return Worktree{}, problem(ErrInvalid, "基线无法解析为本地提交: %v", err)
		}
		argv = append(argv, "-b", req.Branch, "--", path, outputLine(oid))
	} else {
		if req.Base != "" {
			return Worktree{}, problem(ErrInvalid, "已有分支模式不能指定基线")
		}
		if !exists {
			return Worktree{}, problem(ErrNotFound, "本地分支不存在")
		}
		argv = append(argv, "--", path, req.Branch)
	}
	if err := s.beginStart(path); err != nil {
		return Worktree{}, err
	}
	defer s.endStart(path)
	_, operationErr := s.git(ctx, r.entry.AnchorPath, argv...)
	// Cancellation is not evidence that Git made no changes. A fresh bounded
	// read uses its own context; no rollback deletes branches or directories.
	refreshCtx, refreshCancel := context.WithTimeout(context.Background(), readTimeout)
	defer refreshCancel()
	actual := s.readRepository(refreshCtx, r)
	if operationErr != nil {
		category := errorKind(operationErr)
		if category == ErrInternal {
			category = ErrConflict
		}
		return Worktree{}, problem(category, "%v；请刷新实际工作树/分支，可能存在残留", operationErr)
	}
	if actual.Availability != "available" {
		return Worktree{}, problem(ErrInternal, "Git 创建已完成但刷新失败: %s", actual.Error)
	}
	for _, tree := range actual.Worktrees {
		if tree.Path == path {
			return tree, nil
		}
	}
	return Worktree{}, problem(ErrInternal, "Git 创建后未发现目标，请刷新实际状态")
}
