package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// ReadGit is a bounded, noninteractive, read-only runner shared by code views.
// It never refreshes the index or invokes user diff/textconv helpers.
func ReadGit(ctx context.Context, cwd string, limit int, args ...string) ([]byte, error) {
	return ReadGitInput(ctx, cwd, limit, nil, args...)
}

func ReadGitInput(ctx context.Context, cwd string, limit int, input io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	overrides, err := readGitOverrides(ctx, cwd, limit, args...)
	if err != nil {
		return nil, err
	}
	return readGitCommand(ctx, cwd, limit, input, overrides, args...)
}

// StreamGit runs a read-only Git command like ReadGitInput but copies stdout
// to w without buffering or an internal timeout; the caller's ctx bounds it.
// A write error from w terminates the process group and is returned wrapped.
func StreamGit(ctx context.Context, cwd string, w io.Writer, args ...string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	overrides, err := readGitOverrides(ctx, cwd, 64<<10, args...)
	if err != nil {
		return err
	}
	out := &streamOutput{w: w, cancel: cancel}
	err = runReadGit(ctx, cwd, nil, overrides, out, args...)
	if out.err != nil {
		return fmt.Errorf("Git 输出写入失败: %w", out.err)
	}
	return err
}

type streamOutput struct {
	w      io.Writer
	err    error
	cancel context.CancelFunc
}

func (s *streamOutput) Write(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n, err := s.w.Write(p)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		s.err = err
		s.cancel()
	}
	return n, err
}
func readGitOverrides(ctx context.Context, cwd string, limit int, args ...string) ([]string, error) {
	overrides := []string{}
	if len(args) > 0 && (args[0] == "diff" || args[0] == "status") {
		// Even raw/status comparisons can run clean/process filters. Disable
		// every configured driver for this command without changing config.
		names, e := readGitCommand(ctx, cwd, limit, nil, nil, "config", "--null", "--name-only", "--get-regexp", `^filter\..*\.(clean|smudge|process|required)$`)
		if e != nil {
			return nil, e
		}
		for _, key := range strings.Split(string(names), "\x00") {
			if key == "" {
				continue
			}
			value := ""
			if strings.HasSuffix(key, ".required") {
				value = "false"
			}
			overrides = append(overrides, "-c", key+"="+value)
		}
	}
	return overrides, nil
}
func readGitCommand(ctx context.Context, cwd string, limit int, input io.Reader, overrides []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := &boundedOutput{limit: limit, cancel: cancel}
	err := runReadGit(ctx, cwd, input, overrides, out, args...)
	if out.exceeded {
		return nil, problem(ErrTooLarge, "Git 输出超过读取限制")
	}
	return out.Bytes(), err
}

func runReadGit(ctx context.Context, cwd string, input io.Reader, overrides []string, out io.Writer, args ...string) error {
	prefix := []string{"--no-pager", "--literal-pathspecs", "-c", "core.fsmonitor=false", "-C", cwd}
	if len(args) > 0 && (args[0] == "check-ignore" || args[0] == "check-attr") {
		prefix = []string{"--no-pager", "-c", "core.fsmonitor=false", "-C", cwd}
	}
	prefix = append(prefix, overrides...)
	run := runGitProcess(ctx, gitProcess{
		path:        "git",
		args:        append(prefix, args...),
		env:         []string{"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "LC_ALL=C"},
		stdin:       input,
		stdout:      out,
		stderrLimit: 8192,
	})
	if run.stderrExceeded {
		return problem(ErrTooLarge, "Git 输出超过读取限制")
	}
	if run.ctxErr != nil {
		return problem(ErrTimeout, "Git 读取取消或超时")
	}
	if err := run.err; err != nil {
		var exit *exec.ExitError
		if len(args) > 0 && (args[0] == "diff" || args[0] == "check-ignore" || args[0] == "config") && errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("Git 读取失败: %s (%w)", strings.TrimSpace(run.stderr), err)
	}
	return nil
}
