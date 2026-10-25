package codeview

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/rainchan/webmux/internal/session"
	"github.com/rainchan/webmux/internal/workspace"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

type binding struct {
	Context
	identity       os.FileInfo
	gitDirs        []string
	gitIdentity    os.FileInfo
	commonIdentity os.FileInfo
}
type snapshot struct {
	session           string
	context           string
	file              *File
	diff              *Diff
	size              int
	created, timeUsed time.Time
}
type Service struct {
	manager   *session.Manager
	mu        sync.Mutex
	roots     map[string]*binding
	snapshots map[string]*snapshot
	bytes     int
	slots     chan struct{}
	transfers chan struct{}
	done      chan struct{}
	once      sync.Once
}

func New(m *session.Manager) *Service {
	s := &Service{manager: m, roots: map[string]*binding{}, snapshots: map[string]*snapshot{}, slots: make(chan struct{}, 2), transfers: make(chan struct{}, 4), done: make(chan struct{})}
	go s.sweep()
	return s
}
func (s *Service) Close() {
	s.once.Do(func() {
		close(s.done)
		s.mu.Lock()
		clear(s.roots)
		clear(s.snapshots)
		s.bytes = 0
		s.mu.Unlock()
	})
}
func (s *Service) sweep() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-s.manager.Done():
			s.Close()
			return
		case <-ticker.C:
			s.mu.Lock()
			for id := range s.roots {
				if _, err := s.manager.Get(id); err != nil {
					delete(s.roots, id)
				}
			}
			for id, v := range s.snapshots {
				if _, err := s.manager.Get(v.session); err != nil || time.Since(v.created) > Lifetime {
					s.bytes -= v.size
					delete(s.snapshots, id)
				}
			}
			s.mu.Unlock()
		}
	}
}
func random() string {
	var b [18]byte
	_, err := rand.Read(b[:])
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
func digest(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }
func nodeID(p string) string { return base64.RawURLEncoding.EncodeToString([]byte(p)) }
func nodePath(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	p := string(b)
	if err != nil || strings.ContainsRune(p, 0) || filepath.IsAbs(p) {
		return "", problem(ErrInvalid, "无效文件标识")
	}
	if p != "" {
		for _, part := range strings.Split(p, "/") {
			if part == "" || part == "." || part == ".." || strings.EqualFold(part, ".git") {
				return "", problem(ErrInvalid, "路径越界或 Git 内部路径")
			}
		}
	}
	return p, nil
}
func (s *Service) acquire(ctx context.Context) (func(), error) {
	select {
	case s.slots <- struct{}{}:
		return func() { <-s.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.done:
		return nil, problem(ErrUnavailable, "代码服务已关闭")
	}
}
func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	return workspace.ReadGit(ctx, root, MaxBytes, args...)
}

// bind performs filesystem and Git validation; callers must hold a service slot.
func (s *Service) bind(ctx context.Context, id string) (*binding, error) {
	item, err := s.manager.Get(id)
	if err != nil {
		return nil, problem(ErrNotFound, "会话不存在")
	}
	s.mu.Lock()
	b := s.roots[id]
	s.mu.Unlock()
	if b != nil {
		return b, s.validate(ctx, b)
	}
	cwd, err := filepath.EvalSymlinks(item.Info().Cwd)
	if err != nil {
		return nil, err
	}
	for _, segment := range strings.Split(cwd, string(os.PathSeparator)) {
		if strings.EqualFold(segment, ".git") {
			return nil, problem(ErrConflict, "会话启动目录位于 Git 内部，代码浏览不可用")
		}
	}
	if raw, e := git(ctx, cwd, "rev-parse", "--is-inside-git-dir"); e == nil && strings.TrimSpace(string(raw)) == "true" {
		return nil, problem(ErrConflict, "Git 管理目录不可作为浏览根")
	}
	root := cwd
	b = &binding{Context: Context{ID: random(), Kind: "directory", Browse: true, Reason: "普通目录或 Git 不可用；忽略规则与差异不可用"}}
	if out, e := git(ctx, cwd, "rev-parse", "--show-toplevel"); e == nil {
		root = strings.TrimSuffix(string(out), "\n")
		root, e = filepath.EvalSymlinks(root)
		if e != nil {
			return nil, e
		}
		b.Kind = "worktree"
		b.Diff = true
		b.Ignore = true
		b.Reason = ""
		for _, arg := range []string{"--absolute-git-dir", "--git-common-dir"} {
			out, e = git(ctx, root, "rev-parse", "--path-format=absolute", arg)
			if e != nil {
				return nil, e
			}
			p, e := filepath.EvalSymlinks(strings.TrimSuffix(string(out), "\n"))
			if e != nil {
				return nil, e
			}
			b.gitDirs = append(b.gitDirs, p)
		}
		b.commonIdentity, err = os.Stat(b.gitDirs[1])
		if err != nil {
			return nil, err
		}
		b.gitIdentity, err = os.Stat(b.gitDirs[0])
		if err != nil {
			return nil, err
		}
	}
	if linked := item.Info().Workspace; linked != nil && b.Diff {
		expected, e := filepath.EvalSymlinks(linked.WorktreePath)
		if e != nil || expected != root {
			return nil, problem(ErrConflict, "工作区关联与浏览根不一致")
		}
	}
	b.Root = root
	b.identity, err = os.Stat(root)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if old := s.roots[id]; old != nil {
		b = old
	} else {
		s.roots[id] = b
	}
	s.mu.Unlock()
	return b, s.validate(ctx, b)
}
func (s *Service) validate(ctx context.Context, b *binding) error {
	stat, err := os.Lstat(b.Root)
	if err != nil || !stat.IsDir() || !os.SameFile(b.identity, stat) {
		return problem(ErrConflict, "浏览根身份已失效")
	}
	if b.Diff {
		top, e := git(ctx, b.Root, "rev-parse", "--show-toplevel")
		if e != nil {
			return e
		}
		actual, e := filepath.EvalSymlinks(strings.TrimSuffix(string(top), "\n"))
		if e != nil || actual != b.Root {
			return problem(ErrConflict, "工作树根来源已改变")
		}
		common, e := git(ctx, b.Root, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if e != nil {
			return e
		}
		commonPath, e := filepath.EvalSymlinks(strings.TrimSuffix(string(common), "\n"))
		if e != nil || commonPath != b.gitDirs[1] {
			return problem(ErrConflict, "仓库 common directory 已改变")
		}
		commonInfo, e := os.Stat(commonPath)
		if e != nil || !os.SameFile(commonInfo, b.commonIdentity) {
			return problem(ErrConflict, "仓库 common directory 已替换")
		}
		out, e := git(ctx, b.Root, "rev-parse", "--absolute-git-dir")
		if e != nil {
			return e
		}
		p, e := filepath.EvalSymlinks(strings.TrimSuffix(string(out), "\n"))
		if e != nil || p != b.gitDirs[0] {
			return problem(ErrConflict, "Git 来源身份已改变")
		}
		stat, e = os.Stat(p)
		if e != nil || !os.SameFile(stat, b.gitIdentity) {
			return problem(ErrConflict, "Git 管理目录已替换")
		}
	}
	return nil
}
func (s *Service) Context(ctx context.Context, id string) (Context, error) {
	return withBinding(ctx, s, id, func(b *binding) (Context, error) { return b.Context, nil })
}

// withBinding holds a service slot while fn uses session id's validated
// binding. Failing to acquire the slot or bind returns T's zero value.
func withBinding[T any](ctx context.Context, s *Service, id string, fn func(*binding) (T, error)) (T, error) {
	release, err := s.acquire(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	defer release()
	b, err := s.bind(ctx, id)
	if err != nil {
		var zero T
		return zero, err
	}
	return fn(b)
}

// withRoot is withBinding for requests made against browsing context cid; it
// rejects them once the session's context has been replaced.
func withRoot[T any](ctx context.Context, s *Service, id, cid string, fn func(*binding) (T, error)) (T, error) {
	return withBinding(ctx, s, id, func(b *binding) (T, error) {
		if b.ID != cid {
			var zero T
			return zero, problem(ErrConflict, "浏览上下文已改变")
		}
		return fn(b)
	})
}

// Open each segment relative to an already opened directory. O_NOFOLLOW and
// O_NONBLOCK prevent symlink races and blocking special-file opens on Unix.
func boundary(fd int) bool {
	var stat unix.Stat_t
	return unix.Fstatat(fd, ".git", &stat, unix.AT_SYMLINK_NOFOLLOW) == nil || (unix.Fstatat(fd, "HEAD", &stat, unix.AT_SYMLINK_NOFOLLOW) == nil && unix.Fstatat(fd, "objects", &stat, unix.AT_SYMLINK_NOFOLLOW) == nil)
}
func displayPath(p string) string {
	if utf8.ValidString(p) {
		return p
	}
	return strconv.QuoteToASCII(p)
}
func (b *binding) open(p string) (*os.File, error) {
	if _, err := nodePath(nodeID(p)); err != nil {
		return nil, err
	}
	full := filepath.Join(b.Root, p)
	for _, g := range b.gitDirs {
		if full == g || strings.HasPrefix(full, g+"/") {
			return nil, problem(ErrInvalid, "Git 管理路径不可读取")
		}
	}
	fd, err := unix.Open(b.Root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), b.Root)
	stat, err := f.Stat()
	if err != nil || !os.SameFile(stat, b.identity) {
		f.Close()
		return nil, problem(ErrConflict, "浏览根已替换")
	}
	if p == "" {
		return f, nil
	}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		if i > 0 {
			if boundary(int(f.Fd())) {
				f.Close()
				return nil, problem(ErrInvalid, "嵌套仓库边界不可遍历")
			}
		}
		flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		next, e := unix.Openat(int(f.Fd()), part, flags, 0)
		f.Close()
		if e != nil {
			if os.IsNotExist(e) {
				return nil, os.ErrNotExist
			}
			return nil, problem(ErrInvalid, "路径不可安全读取: %v", e)
		}
		f = os.NewFile(uintptr(next), full)
	}
	stat, err = f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if stat.IsDir() && p != "" {
		if boundary(int(f.Fd())) {
			f.Close()
			return nil, problem(ErrInvalid, "嵌套仓库边界不可遍历")
		}
	}
	if !stat.IsDir() && !stat.Mode().IsRegular() {
		f.Close()
		return nil, problem(ErrInvalid, "不支持读取特殊文件或链接")
	}
	return f, nil
}
func lines(text string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}
func document(root, p, source, oid string, data []byte) Document {
	d := Document{NodeID: nodeID(p), rawPath: p, Path: displayPath(p), Absolute: displayPath(filepath.Join(root, p)), Source: source, OID: oid, Version: digest(data), Kind: "text", Referenceable: utf8.ValidString(p) && utf8.ValidString(root)}
	switch {
	case len(data) > MaxBytes:
		d.Kind = "too-large"
		d.Reason = "超过 2 MiB"
	case strings.IndexByte(string(data), 0) >= 0:
		d.Kind = "binary"
		d.Reason = "二进制内容"
	case !utf8.Valid(data):
		d.Kind = "unsupported"
		d.Reason = "不支持的文本编码"
	default:
		d.Lines = len(lines(string(data)))
		if d.Lines > MaxLines {
			d.Kind = "too-large"
			d.Lines = 0
			d.Reason = "超过 20000 行"
		} else {
			d.Text = string(data)
			if extension(p) == ".svg" && svg(d.Text) {
				d.MediaType = svgMediaType
			}
		}
	}
	return d
}
func fileIdentity(info os.FileInfo) *identity {
	id := &identity{size: info.Size(), mtime: info.ModTime().UnixNano()}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		id.dev, id.ino = uint64(st.Dev), uint64(st.Ino)
	}
	return id
}
func (b *binding) read(p string) (Document, error) {
	f, err := b.open(p)
	if err != nil {
		return Document{}, err
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return Document{}, err
	}
	if !before.Mode().IsRegular() {
		return Document{}, problem(ErrInvalid, "目标不是常规文件")
	}
	candidate := imageCandidate(p)
	limit := int64(MaxBytes + 1)
	if candidate {
		limit = headerBytes
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return Document{}, err
	}
	after, err := f.Stat()
	if err != nil {
		return Document{}, err
	}
	current, e := b.open(p)
	if e != nil {
		return Document{}, problem(ErrConflict, "读取期间文件已替换")
	}
	defer current.Close()
	now, e := current.Stat()
	if e != nil || !os.SameFile(before, now) || before.Size() != after.Size() || before.ModTime() != after.ModTime() {
		return Document{}, problem(ErrConflict, "读取期间文件发生变化，请刷新")
	}
	ident := fileIdentity(before)
	var d Document
	if candidate {
		d = imageDocument(b.Root, p, "worktree", "", before.Size(), data)
		d.Version = version([]any{ident.dev, ident.ino, ident.size, ident.mtime, digest(data)})
	} else {
		d = document(b.Root, p, "worktree", "", data)
		if before.Size() > MaxBytes {
			d.Version = version([]any{d.Version, before.Size(), before.ModTime()})
		}
		d.Size = before.Size()
		d.Downloadable = d.Size <= MaxDownloadBytes
	}
	d.identity = ident
	return d, nil
}
func (s *Service) put(v *snapshot) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, old := range s.snapshots {
		if now.Sub(old.created) > Lifetime {
			s.bytes -= old.size
			delete(s.snapshots, id)
		}
	}
	for s.bytes+v.size > CacheBytes {
		var oldest string
		for id, old := range s.snapshots {
			if oldest == "" || old.timeUsed.Before(s.snapshots[oldest].timeUsed) {
				oldest = id
			}
		}
		if oldest == "" {
			break
		}
		s.bytes -= s.snapshots[oldest].size
		delete(s.snapshots, oldest)
	}
	id := random()
	v.created = now
	v.timeUsed = now
	s.snapshots[id] = v
	s.bytes += v.size
	return id
}
func (s *Service) get(id, sid string) (*snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.snapshots[sid]
	if v == nil || time.Since(v.created) > Lifetime {
		return nil, problem(ErrGone, "内容快照已过期，请刷新")
	}
	if v.session != id {
		return nil, problem(ErrInvalid, "快照不属于此会话")
	}
	v.timeUsed = time.Now()
	return v, nil
}
func (s *Service) File(ctx context.Context, id, cid, nid string) (File, error) {
	return s.FileSource(ctx, id, cid, nid, "worktree")
}
func (s *Service) FileSource(ctx context.Context, id, cid, nid, source string) (File, error) {
	return withRoot(ctx, s, id, cid, func(b *binding) (File, error) {
		p, e := nodePath(nid)
		if e != nil {
			return File{}, e
		}
		d, e := b.sourceFile(ctx, p, source)
		if e != nil {
			return File{}, e
		}
		f := File{Document: d, Checked: time.Now().UTC()}
		f.Snapshot = s.put(&snapshot{session: id, context: cid, file: &f, size: documentSize(d) + 512})
		return f, nil
	})
}
func (s *Service) SnapshotFile(ctx context.Context, id, sid, side string) (File, error) {
	return withBinding(ctx, s, id, func(*binding) (File, error) {
		v, e := s.get(id, sid)
		if e != nil {
			return File{}, e
		}
		if v.file != nil {
			return *v.file, nil
		}
		d := v.diff.New
		if side == "old" {
			d = v.diff.Old
		} else if side != "new" {
			return File{}, problem(ErrInvalid, "请选择 old/new 来源")
		}
		return File{Snapshot: sid, Document: d, Checked: v.diff.Checked}, nil
	})
}
func documentSize(d Document) int {
	return len(d.Text) + len(d.Path) + len(d.Absolute) + len(d.NodeID) + len(d.OID) + len(d.Reason) + 256
}

func version(v any) string { b, _ := json.Marshal(v); return digest(b) }
