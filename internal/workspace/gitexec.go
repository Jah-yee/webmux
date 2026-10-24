package workspace

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/rainchan/webmux/internal/session"
)

// gitProcess describes one noninteractive Git invocation. Callers choose the
// executable, full argv, extra environment, stdin, stdout sink and stderr cap;
// runGitProcess owns the process-group lifecycle shared by every Git runner.
type gitProcess struct {
	path        string
	args        []string
	env         []string // appended to os.Environ()
	stdin       io.Reader
	stdout      io.Writer
	stderrLimit int
}

// gitExit reports what happened so each caller can keep its own error mapping.
type gitExit struct {
	err            error // raw error from starting or waiting for the process
	ctxErr         error // set when cancelled, timed out, or an output sink cancelled
	stderr         string
	stderrExceeded bool
}

// runGitProcess runs Git in its own process group. Cancellation kills the
// whole group, and so does a run that ends with the context done or with
// exec.ErrWaitDelay: Cmd's context watcher can finish when the leader exits,
// before pipe copying ends. A hook's background children may still hold those
// pipes; WaitDelay closes descriptors, but does not terminate that group.
func runGitProcess(ctx context.Context, p gitProcess) gitExit {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.path, p.args...)
	cmd.Env = append(os.Environ(), p.env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.Stdin = p.stdin
	cmd.WaitDelay = time.Second
	stderr := &boundedOutput{limit: p.stderrLimit, cancel: cancel}
	cmd.Stdout, cmd.Stderr = p.stdout, stderr
	err := session.RunCommand(cmd)
	if cmd.Process != nil && (ctx.Err() != nil || errors.Is(err, exec.ErrWaitDelay)) {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return gitExit{err: err, ctxErr: ctx.Err(), stderr: stderr.String(), stderrExceeded: stderr.exceeded}
}

// boundedOutput buffers at most limit bytes; the first write past the limit
// marks it exceeded and cancels the owning command.
type boundedOutput struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *boundedOutput) Bytes() []byte  { return b.buffer.Bytes() }
func (b *boundedOutput) String() string { return b.buffer.String() }
func (b *boundedOutput) Len() int       { return b.buffer.Len() }
func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		b.exceeded = true
		b.cancel()
		return 0, errors.New("output limit")
	}
	return b.buffer.Write(p)
}
