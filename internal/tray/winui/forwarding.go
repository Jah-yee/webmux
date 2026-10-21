//go:build windows

package winui

import (
	"errors"
	"net"
	"sort"
	"sync"

	"golang.org/x/sys/windows"

	"github.com/rainchan/webmux/internal/tray"
	"github.com/rainchan/webmux/internal/tray/forward"
)

// relays owns the forwarding listeners, one per enabled Windows address.
type relays struct {
	logf func(string, ...any)

	mu     sync.Mutex
	active map[string]*relay // by Windows address
	failed map[string]string // by Windows address: reason shown in the menu
}

type relay struct {
	listen, target string
	fw             *forward.Forwarder
}

func newRelays(logf func(string, ...any)) *relays {
	return &relays{logf: logf, active: map[string]*relay{}, failed: map[string]string{}}
}

// windowsInterfaces lists the host's interfaces for tray.PlanForward.
func windowsInterfaces() []tray.WinInterface {
	list, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]tray.WinInterface, 0, len(list))
	for _, iface := range list {
		w := tray.WinInterface{Name: iface.Name, Up: iface.Flags&net.FlagUp != 0, Loopback: iface.Flags&net.FlagLoopback != 0}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				w.Addrs = append(w.Addrs, ipnet.IP)
			}
		}
		out = append(out, w)
	}
	return out
}

// reconcile listens on exactly want (address → "ip:port") toward target,
// restarting relays whose port or target changed and retrying failures.
func (r *relays) reconcile(want map[string]string, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for addr, rl := range r.active {
		if listen, ok := want[addr]; !ok || listen != rl.listen || target != rl.target {
			_ = rl.fw.Close()
			delete(r.active, addr)
			r.logf("forwarding stopped on %s", rl.listen)
		}
	}
	for addr := range r.failed {
		if _, ok := want[addr]; !ok {
			delete(r.failed, addr)
		}
	}
	for addr, listen := range want {
		if _, ok := r.active[addr]; ok {
			continue
		}
		fw, err := forward.Start(forward.Options{Listen: listen, Target: target, Logf: r.logf})
		if err != nil {
			reason := failureReason(err)
			if r.failed[addr] != reason {
				r.logf("forwarding on %s failed: %v", listen, err)
			}
			r.failed[addr] = reason
			continue
		}
		delete(r.failed, addr)
		r.active[addr] = &relay{listen: listen, target: target, fw: fw}
		r.logf("forwarding %s to %s", listen, target)
	}
}

func failureReason(err error) string {
	switch {
	case errors.Is(err, windows.WSAEADDRINUSE):
		return "启用失败：端口已被占用"
	case errors.Is(err, windows.WSAEADDRNOTAVAIL):
		return "启用失败：地址不可用"
	case errors.Is(err, windows.WSAEACCES):
		return "启用失败：没有权限监听该地址"
	case errors.Is(err, forward.ErrAddress):
		return "启用失败：只能监听具体的非回环地址"
	}
	return "启用失败：" + err.Error()
}

// state reports whether addr forwards now, and its failure reason.
func (r *relays) state(addr string) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.active[addr]
	return ok, r.failed[addr]
}

// listening lists the active listen addresses, sorted.
func (r *relays) listening() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.active))
	for _, rl := range r.active {
		out = append(out, rl.listen)
	}
	sort.Strings(out)
	return out
}

func (r *relays) any() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active) > 0
}

func (r *relays) closeAll() {
	r.reconcile(nil, "")
}
