package tray

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
)

// ExecRunner runs wsl.exe with WSL_UTF8=1 and, on Windows, without console
// windows (see exec_windows.go).
type ExecRunner struct {
	// Environ is the base environment; nil means os.Environ().
	Environ []string
}

func (r ExecRunner) env(extra []string) []string {
	base := r.Environ
	if base == nil {
		base = os.Environ()
	}
	return append(append(append([]string{}, base...), "WSL_UTF8=1"), extra...)
}

func (r ExecRunner) Run(ctx context.Context, argv, env []string) (Result, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = r.env(env)
	configure(cmd, false)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	var exit *exec.ExitError
	if errors.As(err, &exit) && ctx.Err() == nil {
		res.Code = exit.ExitCode()
		return res, nil
	}
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		return res, err
	}
	return res, nil
}

// Start creates a detached process whose standard streams are the null
// device, so it outlives the tray.
func (r ExecRunner) Start(argv, env []string) (Process, error) {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = r.env(env)
	configure(cmd, true)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &execProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		p.mu.Lock()
		p.code = cmd.ProcessState.ExitCode()
		p.mu.Unlock()
		close(p.done)
	}()
	return p, nil
}

type execProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.Mutex
	code int
}

func (p *execProcess) PID() int              { return p.cmd.Process.Pid }
func (p *execProcess) Done() <-chan struct{} { return p.done }
func (p *execProcess) ExitCode() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.code
}
