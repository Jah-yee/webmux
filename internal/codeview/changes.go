package codeview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/rainchan/webmux/internal/workspace"
	"io"
	"strconv"
	"strings"
	"time"
)

const ChangePageSize = 200

// scanChanges hashes the complete ordered listing while retaining no list.
// Page reads, direct lookups and snapshot checks choose what to retain.
func (b *binding) scanChanges(ctx context.Context, visit func(Change) error) (string, error) {
	if !b.Diff {
		return "", problem(ErrConflict, "此目录的 Git 差异不可用")
	}
	hash := sha256.New()
	emit := func(c Change) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		c.NodeID = nodeID(c.NewPath)
		c.ID = version(c)
		io.WriteString(hash, c.ID+"\x00")
		if visit != nil {
			return visit(c)
		}
		return nil
	}
	for _, group := range []string{"unstaged", "staged"} {
		args := []string{"diff", "--raw", "-z", "--no-abbrev", "--no-ext-diff", "--no-textconv", "--find-renames"}
		if group == "staged" {
			args = append(args, "--cached")
		}
		args = append(args, "--")
		phase := 0
		var c Change
		err := workspace.ReadGitRecords(ctx, b.Root, func(record string) error {
			switch phase {
			case 0:
				fields := strings.Fields(record)
				if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") {
					return problem(ErrInternal, "无效 Git 修改输出")
				}
				c = Change{Group: group, OldOID: fields[2], NewOID: fields[3], OldMode: strings.TrimPrefix(fields[0], ":"), NewMode: fields[1], Status: fields[4]}
				phase = 1
			case 1:
				if record == "" {
					return problem(ErrInternal, "无效 Git 修改路径")
				}
				c.OldPath, c.NewPath = record, record
				if strings.HasPrefix(c.Status, "R") || strings.HasPrefix(c.Status, "C") {
					phase = 2
				} else {
					phase = 0
					return emit(c)
				}
			case 2:
				if record == "" {
					return problem(ErrInternal, "无效 Git 重命名路径")
				}
				c.NewPath = record
				phase = 0
				return emit(c)
			}
			return nil
		}, args...)
		if err != nil {
			return "", err
		}
		if phase != 0 {
			return "", problem(ErrInternal, "Git 修改输出不完整")
		}
	}
	err := workspace.ReadGitRecords(ctx, b.Root, func(path string) error {
		if path == "" {
			return problem(ErrInternal, "无效 Git 未跟踪路径")
		}
		return emit(Change{Group: "untracked", OldPath: path, NewPath: path, Status: "A", OldMode: "000000", NewMode: "100644"})
	}, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *Service) Changes(ctx context.Context, id, cid string, queries ...ChangeQuery) (Changes, error) {
	r := Changes{Entries: []Change{}, Checked: time.Now().UTC()}
	return withRoot(ctx, s, id, cid, func(b *binding) (Changes, error) {
		var err error
		var q ChangeQuery
		if len(queries) > 0 {
			q = queries[0]
		}
		if q.Group != "" && q.Group != "unstaged" && q.Group != "staged" && q.Group != "untracked" {
			return r, problem(ErrInvalid, "无效修改分组")
		}
		var path string
		if q.NodeID != "" {
			path, err = nodePath(q.NodeID)
			if err != nil {
				return r, err
			}
		}
		start, tag := 0, ""
		if q.Cursor != "" {
			var offset string
			var ok bool
			tag, offset, ok = strings.Cut(q.Cursor, ":")
			start, err = strconv.Atoi(offset)
			if !ok || tag == "" || err != nil || start < 0 {
				return r, problem(ErrInvalid, "无效修改游标")
			}
		}
		search := strings.ToLower(q.Q)
		r.Version, err = b.scanChanges(ctx, func(c Change) error {
			if q.Group != "" && q.Group != c.Group {
				return nil
			}
			if q.NodeID != "" && c.NewPath != path && c.OldPath != path {
				return nil
			}
			if search != "" && !strings.Contains(strings.ToLower(displayPath(c.NewPath)), search) && !strings.Contains(strings.ToLower(displayPath(c.OldPath)), search) {
				return nil
			}
			if r.Total >= start && len(r.Entries) < ChangePageSize {
				r.Entries = append(r.Entries, c)
			}
			r.Total++
			return nil
		})
		if err != nil {
			return r, err
		}
		expected := version([]string{cid, r.Version, q.Q, q.NodeID, q.Group})
		if q.Cursor != "" && tag != expected {
			return r, problem(ErrConflict, "修改列表已改变，请刷新列表")
		}
		if start > r.Total {
			return r, problem(ErrInvalid, "无效修改页")
		}
		end := start + len(r.Entries)
		if end < r.Total {
			r.Next = fmt.Sprintf("%s:%d", expected, end)
		}
		return r, nil
	})
}
