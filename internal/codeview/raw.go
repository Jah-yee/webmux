package codeview

import (
	"context"
	"errors"
	"fmt"
	"github.com/rainchan/webmux/internal/workspace"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

// ErrChanged reports that a source changed after streaming started; callers
// must abort the response rather than finish it.
var ErrChanged = errors.New("内容已变化，请刷新")

// Transfer streams the exact source bytes of one snapshot side. It holds a
// transfer slot (not a service slot) until Close.
type Transfer struct {
	Name      string
	MediaType string
	Size      int64
	binding   *binding
	path      string
	file      *os.File
	identity  *identity
	oid       string
	release   func()
}

// Raw resolves a snapshot side into an opened, verified Transfer. The service
// slot is released before returning so slow clients only occupy a transfer slot.
func (s *Service) Raw(ctx context.Context, id, sid, side string) (*Transfer, error) {
	select {
	case s.transfers <- struct{}{}:
	default:
		return nil, problem(ErrBusy, "下载并发已满，请稍后重试")
	}
	done := func() { <-s.transfers }
	t, e := s.openRaw(ctx, id, sid, side)
	if e != nil {
		done()
		return nil, e
	}
	t.release = done
	return t, nil
}
func (s *Service) openRaw(ctx context.Context, id, sid, side string) (*Transfer, error) {
	return withBinding(ctx, s, id, func(b *binding) (*Transfer, error) {
		v, e := s.get(id, sid)
		if e != nil {
			return nil, e
		}
		var d Document
		if v.file != nil {
			d = v.file.Document
		} else if side == "old" {
			d = v.diff.Old
		} else if side == "new" {
			d = v.diff.New
		} else {
			return nil, problem(ErrInvalid, "请选择 old/new 来源")
		}
		if d.Source == "empty" {
			return nil, problem(ErrNotFound, "此侧内容不存在")
		}
		if d.Size > MaxDownloadBytes {
			return nil, problem(ErrTooLarge, "超过 100 MiB 下载上限")
		}
		if !d.Downloadable {
			return nil, problem(ErrNotFound, "此来源不是可下载的常规文件")
		}
		t := &Transfer{Name: path.Base(d.rawPath), MediaType: d.MediaType, Size: d.Size, binding: b, path: d.rawPath}
		if d.Source == "worktree" {
			if d.identity == nil {
				return nil, problem(ErrConflict, "内容已变化，请刷新")
			}
			f, e := b.open(d.rawPath)
			if e != nil {
				if errors.Is(e, os.ErrNotExist) {
					return nil, problem(ErrConflict, "内容已变化，请刷新")
				}
				return nil, e
			}
			info, e := f.Stat()
			if e != nil || !info.Mode().IsRegular() || *fileIdentity(info) != *d.identity {
				f.Close()
				return nil, problem(ErrConflict, "内容已变化，请刷新")
			}
			t.file, t.identity = f, d.identity
			return t, nil
		}
		if d.Source != "index" && d.Source != "head" {
			return nil, problem(ErrInvalid, "无效文件来源")
		}
		raw, e := git(ctx, b.Root, "cat-file", "-s", d.OID)
		if e != nil {
			return nil, fmt.Errorf("读取对象大小: %w", e)
		}
		size, e := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if e != nil || size != d.Size {
			return nil, problem(ErrConflict, "内容已变化，请刷新")
		}
		t.oid = d.OID
		return t, nil
	})
}

// Close releases the opened file and the transfer slot; safe to call twice.
func (t *Transfer) Close() {
	if t.file != nil {
		t.file.Close()
		t.file = nil
	}
	if t.release != nil {
		t.release()
		t.release = nil
	}
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if e := c.ctx.Err(); e != nil {
		return 0, e
	}
	return c.r.Read(p)
}

// WriteTo copies exactly Size bytes to w. A non-nil error after any byte was
// written means the response is incomplete and must be aborted; ErrChanged
// marks a source that no longer matches the snapshot.
func (t *Transfer) WriteTo(ctx context.Context, w io.Writer) error {
	if t.file == nil {
		out := &countingWriter{w: w}
		if e := workspace.StreamGit(ctx, t.binding.Root, out, "cat-file", "blob", t.oid); e != nil {
			return e
		}
		if out.n != t.Size {
			return ErrChanged
		}
		return nil
	}
	n, e := io.CopyN(w, contextReader{ctx, t.file}, t.Size)
	if e != nil && !errors.Is(e, io.EOF) {
		return e
	}
	if e != nil || n != t.Size {
		return ErrChanged
	}
	info, e := t.file.Stat()
	if e != nil || *fileIdentity(info) != *t.identity {
		return ErrChanged
	}
	current, e := t.binding.open(t.path)
	if e != nil {
		return ErrChanged
	}
	defer current.Close()
	now, e := current.Stat()
	if e != nil || !os.SameFile(info, now) || *fileIdentity(now) != *t.identity {
		return ErrChanged
	}
	return nil
}
