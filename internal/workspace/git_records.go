package workspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"
)

// ReadGitRecords streams NUL-delimited records, without buffering or limiting
// the total output. Callers retain only the records needed for their request.
func ReadGitRecords(ctx context.Context, cwd string, visit func(string) error, args ...string) error {
	return ReadGitRecordsInput(ctx, cwd, nil, visit, args...)
}

func ReadGitRecordsInput(ctx context.Context, cwd string, input io.Reader, visit func(string) error, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	overrides, err := readGitOverrides(ctx, cwd, 2*1024*1024, args...)
	if err != nil {
		return err
	}
	records := &nulRecords{visit: visit, cancel: cancel}
	err = runReadGit(ctx, cwd, input, overrides, records, args...)
	if records.err != nil {
		return records.err
	}
	if err != nil {
		return err
	}
	if len(records.pending) != 0 {
		return fmt.Errorf("读取 Git NUL 输出不完整")
	}
	return nil
}

type nulRecords struct {
	pending []byte
	visit   func(string) error
	cancel  context.CancelFunc
	err     error
}

func (r *nulRecords) Write(p []byte) (int, error) {
	size := len(p)
	for len(p) > 0 {
		end := bytes.IndexByte(p, 0)
		if end < 0 {
			end = len(p)
		}
		// This bounds one malformed record, never the number of repository paths.
		if len(r.pending)+end > 1024*1024 {
			r.err = fmt.Errorf("读取 Git 单条路径记录超过 1 MiB")
		} else {
			r.pending = append(r.pending, p[:end]...)
			if end < len(p) {
				r.err = r.visit(string(r.pending))
				r.pending = r.pending[:0]
			}
		}
		if r.err != nil {
			r.cancel()
			return 0, r.err
		}
		if end == len(p) {
			break
		}
		p = p[end+1:]
	}
	return size, nil
}
