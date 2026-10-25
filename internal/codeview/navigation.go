package codeview

import (
	"context"
	"errors"
	"fmt"
	"github.com/rainchan/webmux/internal/workspace"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (b *binding) listing(ctx context.Context, p string, include bool) (Listing, error) {
	r, e := b.directory(ctx, p, include, nil)
	if e != nil {
		return r, e
	}
	if b.Ignore {
		if e = b.statuses(ctx, p, r.Children); e != nil {
			return r, e
		}
	}
	r.Version = version(r.Children)
	return r, nil
}

// Search supplies a shared ignore index; tree pages check only their entries.
// Both paths retain descriptor-based boundary checks and prune ignored entries
// before opening child directories.
func (b *binding) directory(ctx context.Context, p string, include bool, ignored ignoredPaths) (Listing, error) {
	r := Listing{Children: []Node{}, Complete: true, Checked: time.Now().UTC()}
	f, e := b.open(p)
	if e != nil {
		return r, e
	}
	defer f.Close()
	entries, e := f.ReadDir(10001)
	if e != nil && e != io.EOF {
		return r, e
	}
	if len(entries) > 10000 {
		entries = entries[:10000]
		r.Limited = true
		r.Complete = false
	}
	if b.Ignore && ignored == nil {
		ignored = ignoredPaths{}
		var paths strings.Builder
		for _, entry := range entries {
			paths.WriteString(filepath.ToSlash(filepath.Join(p, entry.Name())))
			paths.WriteByte(0)
		}
		if paths.Len() > 0 {
			e = workspace.ReadGitRecordsInput(ctx, b.Root, strings.NewReader(paths.String()), func(path string) error {
				ignored[strings.TrimSuffix(path, "/")] = true
				return nil
			}, "check-ignore", "-z", "--stdin")
			if e != nil {
				return r, e
			}
		}
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		name := entry.Name()
		if strings.EqualFold(name, ".git") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(p, name))
		isIgnored := ignored.contains(rel)
		if isIgnored && !include {
			continue
		}
		full := filepath.Join(b.Root, rel)
		internal := false
		for _, g := range b.gitDirs {
			if full == g || strings.HasPrefix(full, g+"/") {
				internal = true
			}
		}
		if internal {
			continue
		}
		kind := "file"
		if entry.IsDir() {
			kind = "directory"
			fd, e := unix.Openat(int(f.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if e != nil {
				return r, e
			}
			var st unix.Stat_t
			if unix.Fstatat(fd, ".git", &st, unix.AT_SYMLINK_NOFOLLOW) == nil {
				kind = "boundary"
			} else if unix.Fstatat(fd, "HEAD", &st, unix.AT_SYMLINK_NOFOLLOW) == nil && unix.Fstatat(fd, "objects", &st, unix.AT_SYMLINK_NOFOLLOW) == nil {
				kind = "boundary"
			}
			unix.Close(fd)
		} else if entry.Type()&os.ModeSymlink != 0 {
			kind = "symlink"
		} else if !entry.Type().IsRegular() {
			kind = "special"
		}
		n := Node{ID: nodeID(rel), Path: displayPath(rel), Name: displayPath(name), Kind: kind, Ignored: isIgnored, Referenceable: utf8.ValidString(rel)}
		if kind == "file" {
			n.MediaType = nodeMediaType(name)
		}
		if kind == "symlink" {
			data := make([]byte, 4096)
			count, e := unix.Readlinkat(int(f.Fd()), name, data)
			if e == nil {
				n.Link = string(data[:count])
			}
		}
		r.Children = append(r.Children, n)
	}
	sort.Slice(r.Children, func(i, j int) bool {
		a, b := r.Children[i], r.Children[j]
		if (a.Kind == "directory") != (b.Kind == "directory") {
			return a.Kind == "directory"
		}
		return a.Name < b.Name
	})
	return r, nil
}

// Retain status only for the displayed nodes, even for a repository with a
// much larger status stream. Search calls this once for its final result set.
func (b *binding) statuses(ctx context.Context, p string, nodes []Node) error {
	if len(nodes) == 0 {
		return nil
	}
	wanted := make(map[string]*Node, len(nodes))
	for i := range nodes {
		path, _ := nodePath(nodes[i].ID)
		wanted[path] = &nodes[i]
	}
	skipSource := false
	err := workspace.ReadGitRecords(ctx, b.Root, func(record string) error {
		if skipSource {
			skipSource = false
			return nil
		}
		if len(record) < 4 {
			return problem(ErrInternal, "无效 Git 状态输出")
		}
		if n := wanted[strings.TrimSuffix(record[3:], "/")]; n != nil {
			n.Status = record[:2]
		}
		skipSource = strings.ContainsAny(record[:2], "RC")
		return nil
	}, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", pOrDot(p))
	if err == nil && skipSource {
		err = problem(ErrInternal, "Git 状态输出不完整")
	}
	return err
}
func pOrDot(p string) string {
	if p == "" {
		return "."
	}
	return p
}
func (s *Service) Tree(ctx context.Context, id, cid, nid string, include bool, cursor string) (Listing, error) {
	return withRoot(ctx, s, id, cid, func(b *binding) (Listing, error) {
		p, e := nodePath(nid)
		if e != nil {
			return Listing{}, e
		}
		r, e := b.listing(ctx, p, include)
		if e != nil {
			return r, e
		}
		start := 0
		if cursor != "" {
			v, n, ok := strings.Cut(cursor, ":")
			start, e = strconv.Atoi(n)
			if !ok || e != nil || start < 0 {
				return r, problem(ErrInvalid, "无效目录游标")
			}
			if v != r.Version {
				return r, problem(ErrConflict, "目录已改变，请重新读取")
			}
		}
		if start > len(r.Children) {
			return r, problem(ErrInvalid, "无效目录页")
		}
		end := min(start+200, len(r.Children))
		if end < len(r.Children) {
			r.Next = fmt.Sprintf("%s:%d", r.Version, end)
		}
		r.Children = r.Children[start:end]
		return r, nil
	})
}
func (s *Service) Search(ctx context.Context, id, cid, q string, include bool) (Listing, error) {
	return withRoot(ctx, s, id, cid, func(b *binding) (Listing, error) {
		r := Listing{Children: []Node{}, Complete: true, Checked: time.Now().UTC()}
		ignored, limited, e := b.searchIgnored(ctx)
		if e != nil {
			return r, e
		}
		if limited {
			r.Limited, r.Complete = true, false
			r.Version = version(r.Children)
			return r, nil
		}
		queue := []string{""}
		visited := 0
		query := strings.ToLower(q)
		for len(queue) > 0 {
			if e := ctx.Err(); e != nil {
				return r, e
			}
			p := queue[0]
			queue = queue[1:]
			page, e := b.directory(ctx, p, include, ignored)
			if e != nil {
				return r, e
			}
			if page.Limited {
				r.Limited = true
				r.Complete = false
			}
			for _, n := range page.Children {
				visited++
				if visited >= 100000 || len(r.Children) >= 200 {
					r.Limited = true
					r.Complete = false
					queue = nil
					break
				}
				path, _ := nodePath(n.ID)
				if n.Kind == "directory" {
					queue = append(queue, path)
				} else if strings.Contains(strings.ToLower(n.Path), query) {
					r.Children = append(r.Children, n)
				}
			}
		}
		if b.Ignore {
			if e := b.statuses(ctx, "", r.Children); e != nil {
				return r, e
			}
		}
		r.Version = version(r.Children)
		return r, nil
	})
}

// Locate resolves a typed path from the session's local OSC 7 report, or its
// launch directory when the shell has not reported one. Both bases may be
// logical paths through symlinks while the root is canonical, so the target is
// resolved through the filesystem before comparing it with the root. The result
// only names a node; reads still walk the root with O_NOFOLLOW.
func (s *Service) Locate(ctx context.Context, id, cid, input string) (Location, error) {
	return withRoot(ctx, s, id, cid, func(b *binding) (Location, error) {
		item, e := s.manager.Get(id)
		if e != nil {
			return Location{}, problem(ErrNotFound, "会话不存在")
		}
		input = strings.TrimSpace(input)
		if input == "" || len(input) > 4096 || strings.ContainsRune(input, 0) {
			return Location{}, problem(ErrInvalid, "路径无效")
		}
		info := item.Info()
		base := info.Cwd
		if reported := info.ReportedCwd; reported != nil {
			if reported.Remote {
				return Location{}, problem(ErrConflict, "Shell 报告的是远端目录，不能用于本机定位")
			}
			base = reported.Path
		}
		target := input
		if !filepath.IsAbs(target) {
			// Concatenate rather than Join: `..` after a symlink follows the real
			// parent, as the shell's own file commands would.
			target = base + string(os.PathSeparator) + target
		}
		resolved, e := filepath.EvalSymlinks(target)
		if errors.Is(e, os.ErrNotExist) || errors.Is(e, unix.ENOTDIR) {
			return Location{}, problem(ErrNotFound, "路径不存在")
		}
		if e != nil {
			return Location{}, problem(ErrInvalid, "无法解析路径")
		}
		rel, e := filepath.Rel(b.Root, resolved)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return Location{}, problem(ErrForbidden, "路径不在代码工作区根内，只允许打开根目录下的文件")
		}
		if rel == "." {
			rel = ""
		}
		if _, e := nodePath(nodeID(rel)); e != nil {
			return Location{}, e
		}
		stat, e := os.Stat(resolved)
		if e != nil {
			return Location{}, e
		}
		kind := "file"
		if stat.IsDir() {
			kind = "directory"
		} else if !stat.Mode().IsRegular() {
			return Location{}, problem(ErrInvalid, "不支持打开特殊文件")
		}
		return Location{NodeID: nodeID(rel), Path: rel, Kind: kind}, nil
	})
}
