package tunnel

import "syscall"

// childAttr makes the kernel stop cloudflared if webmux dies without running
// its shutdown path (SIGKILL, OOM), so the tunnel never outlives the server.
func childAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
