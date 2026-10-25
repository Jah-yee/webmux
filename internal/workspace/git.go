package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rainchan/webmux/internal/pathutil"
)

const (
	maxWorkspaceGitOutput = 32 << 20
	maxWorkspaceGitError  = 64 << 10
)

func (s *Service) git(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	if s.gitPath == "" {
		return nil, problem(ErrUnavailable, "%s", s.Capability().GitError)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stdout := &boundedOutput{limit: maxWorkspaceGitOutput, cancel: cancel}
	run := runGitProcess(ctx, gitProcess{
		path:        s.gitPath,
		args:        append([]string{"-C", cwd}, args...),
		env:         []string{"GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "LC_ALL=C"},
		stdout:      stdout,
		stderrLimit: maxWorkspaceGitError,
	})
	if err := run.err; err != nil {
		if stdout.exceeded || run.stderrExceeded {
			return nil, problem(ErrTooLarge, "Git 输出超过限制，实际状态可能已变化；请刷新")
		}
		if run.ctxErr != nil {
			return nil, problem(ErrTimeout, "Git 操作取消或超时，实际状态可能已变化: %v", run.ctxErr)
		}
		var launch *exec.Error
		if errors.As(err, &launch) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
			return nil, problem(ErrUnavailable, "Git 无法执行: %v", err)
		}
		return nil, problem(ErrInternal, "Git %s 失败: %s (%v)", args[0], strings.TrimSpace(run.stderr), err)
	}
	return stdout.Bytes(), nil
}
func canonical(path string) (string, error) {
	path, err := pathutil.Expand(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

// canonicalTarget resolves existing ancestors too, including symlinked parents.
func canonicalTarget(path string) (string, error) {
	path, err := pathutil.Expand(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	resolved, err = canonicalTarget(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(path)), nil
}
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func overlap(a, b string) bool      { return within(a, b) || within(b, a) }
func outputLine(data []byte) string { return strings.TrimSuffix(string(data), "\n") }
func (s *Service) common(ctx context.Context, path string) (string, error) {
	out, err := s.git(ctx, path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return canonical(outputLine(out))
}
func (s *Service) locate(ctx context.Context, path string) (string, string, error) {
	path, err := canonical(path)
	if err != nil {
		return "", "", problem(ErrInvalid, "仓库目录不可访问: %v", err)
	}
	common, err := s.common(ctx, path)
	if err != nil {
		if errorKind(err) == ErrInternal {
			err = problem(ErrInvalid, "无法识别 Git 仓库（请检查目录权限和信任设置）: %v", err)
		}
		return "", "", err
	}
	out, err := s.git(ctx, path, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return "", "", err
	}
	trees, err := parseWorktrees(out, "")
	if err != nil || len(trees) == 0 {
		return "", "", problem(ErrUnavailable, "Git 不支持所需 worktree 机器输出: %v", err)
	}
	return common, trees[0].Path, nil
}
func parseWorktrees(data []byte, repository string) ([]Worktree, error) {
	if len(data) == 0 || data[len(data)-1] != 0 {
		return nil, errors.New("worktree 输出不是 NUL 格式")
	}
	out := make([]Worktree, 0)
	var current *Worktree
	for _, raw := range bytes.Split(data, []byte{0}) {
		field := string(raw)
		if field == "" {
			current = nil
			continue
		}
		key, value, _ := strings.Cut(field, " ")
		if key == "worktree" {
			if !filepath.IsAbs(value) {
				return nil, errors.New("工作树路径不是绝对路径（来自 Git）")
			}
			path, err := canonicalTarget(value)
			if err != nil {
				path = filepath.Clean(value)
			}
			kind := "linked"
			if len(out) == 0 {
				kind = "main"
			}
			out = append(out, Worktree{ID: worktreeID(repository, path), RepositoryID: repository, Path: path, Kind: kind})
			current = &out[len(out)-1]
			continue
		}
		if current == nil {
			return nil, errors.New("工作树记录缺少路径（来自 Git）")
		}
		switch key {
		case "HEAD":
			if strings.Trim(value, "0") != "" {
				current.Head = strptr(value)
			}
		case "branch":
			current.Branch = strptr(strings.TrimPrefix(value, "refs/heads/"))
		case "detached":
			current.Detached = true
		case "bare":
			current.Kind = "bare"
		case "locked":
			current.Locked = true
			current.LockReason = value
		case "prunable":
			current.Prunable = true
		}
	}
	return out, nil
}
func parseStatus(data []byte) (dirty, untracked, ignored bool, err error) {
	if len(data) == 0 {
		return
	}
	if data[len(data)-1] != 0 {
		err = errors.New("状态输出不是 NUL 格式（来自 Git）")
		return
	}
	records := bytes.Split(data[:len(data)-1], []byte{0})
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) < 4 || record[2] != ' ' {
			err = errors.New("状态记录无效（来自 Git）")
			return
		}
		switch string(record[:2]) {
		case "??":
			untracked = true
		case "!!":
			ignored = true
		default:
			dirty = true
			if record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C' {
				i++
				if i >= len(records) {
					err = errors.New("重命名状态记录不完整（来自 Git）")
					return
				}
			}
		}
	}
	return
}
func (s *Service) snapshot(ctx context.Context, r *repositoryState) (Repository, error) {
	if err := ctx.Err(); err != nil {
		return Repository{}, err
	}
	r.flightMu.Lock()
	flight := r.flight
	if flight == nil {
		readCtx, cancel := context.WithTimeout(context.Background(), readTimeout)
		flight = &snapshotFlight{done: make(chan struct{}), cancel: cancel}
		r.flight = flight
		go func() {
			defer cancel()
			if err := acquire(readCtx, r); err != nil {
				flight.err = err
			} else {
				flight.repository = s.readRepository(readCtx, r)
				release(r)
			}
			r.flightMu.Lock()
			if r.flight == flight {
				r.flight = nil
			}
			close(flight.done)
			r.flightMu.Unlock()
		}()
	}
	flight.waiters++
	r.flightMu.Unlock()
	defer func() {
		r.flightMu.Lock()
		flight.waiters--
		if flight.waiters == 0 {
			flight.cancel()
			if r.flight == flight {
				r.flight = nil
			}
		}
		r.flightMu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return Repository{}, ctx.Err()
	case <-flight.done:
		return flight.repository, flight.err
	}
}
func (s *Service) readRepository(ctx context.Context, r *repositoryState) Repository {
	e := r.entry
	result := Repository{ID: e.ID, Name: e.DisplayName, CommonDir: e.CommonDir, AnchorPath: e.AnchorPath, Availability: "unavailable", CheckedAt: time.Now().UTC(), Worktrees: []Worktree{}}
	common, err := s.common(ctx, e.AnchorPath)
	if err == nil && common != e.CommonDir {
		err = errors.New("仓库 common directory 身份已改变，请移除并重新注册")
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	data, err := s.git(ctx, e.AnchorPath, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		result.Error = err.Error()
		return result
	}
	trees, err := parseWorktrees(data, e.ID)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	for i := range trees {
		tree := &trees[i]
		stat, statErr := os.Stat(tree.Path)
		if statErr != nil || !stat.IsDir() {
			tree.Error = fmt.Sprintf("工作树目录不可访问: %v", statErr)
			continue
		}
		if tree.Kind == "bare" {
			tree.Available = true
			continue
		}
		actual, checkErr := s.common(ctx, tree.Path)
		if checkErr != nil || actual != e.CommonDir {
			tree.Error = fmt.Sprintf("无法验证工作树身份: %v", checkErr)
			continue
		}
		tree.Available = true
		status, statusErr := s.git(ctx, tree.Path, "status", "--porcelain=v1", "-z", "--untracked-files=normal", "--ignored=matching")
		if statusErr != nil {
			tree.Error = statusErr.Error()
			continue
		}
		dirty, untracked, ignored, parseErr := parseStatus(status)
		if parseErr != nil {
			tree.Error = parseErr.Error()
			continue
		}
		tree.Dirty, tree.HasUntracked, tree.HasIgnored = &dirty, untracked, ignored
	}
	sort.SliceStable(trees, func(i, j int) bool {
		if trees[i].Kind != "linked" {
			return true
		}
		if trees[j].Kind != "linked" {
			return false
		}
		return trees[i].Path < trees[j].Path
	})
	result.Availability, result.Worktrees = "available", trees
	return result
}
func (s *Service) current(ctx context.Context, r *repositoryState) (Repository, error) {
	if !s.registered(r) {
		return Repository{}, problem(ErrNotFound, "仓库已移除")
	}
	repository := s.readRepository(ctx, r)
	if repository.Availability != "available" {
		if ctx.Err() != nil {
			return repository, ctx.Err()
		}
		return repository, problem(ErrUnavailable, "%s", repository.Error)
	}
	return repository, nil
}
func findTree(repository Repository, id string) (Worktree, error) {
	for _, tree := range repository.Worktrees {
		if tree.ID == id {
			return tree, nil
		}
	}
	return Worktree{}, problem(ErrNotFound, "工作树不存在，请刷新")
}
func (s *Service) branches(ctx context.Context, r *repositoryState, repository Repository) (Branches, error) {
	result := Branches{Branches: []Branch{}, Bases: []Base{}, DefaultParent: filepath.Join(filepath.Dir(r.entry.AnchorPath), filepath.Base(r.entry.AnchorPath)+".worktrees")}
	data, err := s.git(ctx, r.entry.AnchorPath, "for-each-ref", "--format=%(refname)%00%(objectname)%00", "refs/heads/", "refs/remotes/")
	if err != nil {
		return result, err
	}
	fields := bytes.Split(data, []byte{0})
	for i := 0; i+1 < len(fields); i += 2 {
		ref := strings.TrimPrefix(string(fields[i]), "\n")
		head := string(fields[i+1])
		if ref == "" {
			continue
		}
		name := strings.TrimPrefix(ref, "refs/heads/")
		if strings.HasPrefix(ref, "refs/heads/") {
			branch := Branch{Name: name, Head: head, WorktreePaths: []string{}}
			for _, tree := range repository.Worktrees {
				if tree.Branch != nil && *tree.Branch == name {
					branch.WorktreePaths = append(branch.WorktreePaths, tree.Path)
				}
			}
			result.Branches = append(result.Branches, branch)
		}
		result.Bases = append(result.Bases, Base{Name: ref, Head: head})
	}
	for _, tree := range repository.Worktrees {
		if tree.Kind == "main" && tree.Head != nil {
			result.DefaultBase = tree.Head
		}
	}
	return result, nil
}
func (s *Service) Branches(ctx context.Context, id string) (Branches, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	r, err := s.repository(id)
	if err != nil {
		return Branches{}, err
	}
	if err := acquire(ctx, r); err != nil {
		return Branches{}, err
	}
	defer release(r)
	repository, err := s.current(ctx, r)
	if err != nil {
		return Branches{}, err
	}
	return s.branches(ctx, r, repository)
}

// unsupportedContents is deliberately conservative: removal never traverses or
// cleans another repository, including ignored nested repositories/submodules.
func (s *Service) unsupportedContents(ctx context.Context, tree Worktree) error {
	data, err := s.git(ctx, tree.Path, "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}
	for _, entry := range bytes.Split(data, []byte{0}) {
		if bytes.HasPrefix(entry, []byte("160000 ")) {
			return errors.New("工作树含子模块，请在终端处理")
		}
	}
	return filepath.WalkDir(tree.Path, func(path string, entry fs.DirEntry, walkErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if walkErr != nil {
			return walkErr
		}
		if path == tree.Path {
			return nil
		}
		if entry.Name() == ".git" {
			if filepath.Dir(path) != tree.Path {
				return errors.New("工作树含嵌套 Git 仓库，请在终端处理")
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
		}
		if entry.IsDir() {
			// Bare repositories have no .git entry.
			if stat, err := os.Stat(filepath.Join(path, "HEAD")); err == nil && !stat.IsDir() {
				if objects, err := os.Stat(filepath.Join(path, "objects")); err == nil && objects.IsDir() {
					return errors.New("工作树含嵌套 bare 仓库，请在终端处理")
				}
			}
		}
		return nil
	})
}
