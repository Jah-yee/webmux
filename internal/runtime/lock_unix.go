//go:build linux || darwin

package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// ErrLocked means another live instance owns the runtime directory.
var ErrLocked = errors.New("状态目录已被另一实例使用")

// Lock is the exclusive server.lock held for a server's whole lifetime.
type Lock struct{ f *os.File }

// AcquireLock never blocks; a held lock yields ErrLocked.
func AcquireLock(dir string) (*Lock, error) {
	f, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return &Lock{f: f}, nil
}

func (l *Lock) Release() {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}

// ErrNoSocketPath means neither the default nor the fallback path fits.
var ErrNoSocketPath = errors.New("no usable local API socket path")

// SocketPath is <state_dir>/run/webmux.sock, or a fixed name under
// $XDG_RUNTIME_DIR/webmux derived from the state directory when the default
// exceeds the platform's sun_path limit.
func SocketPath(stateDir string, getenv func(string) string) (string, error) {
	path := filepath.Join(Dir(stateDir), socketName)
	if len(path) <= maxSocketPath {
		return path, nil
	}
	base := getenv("XDG_RUNTIME_DIR")
	if base == "" {
		return "", fmt.Errorf("%w: %s is longer than %d bytes and XDG_RUNTIME_DIR is unset", ErrNoSocketPath, path, maxSocketPath)
	}
	sum := sha256.Sum256([]byte(stateDir))
	path = filepath.Join(base, "webmux", hex.EncodeToString(sum[:])[:12]+".sock")
	if len(path) > maxSocketPath {
		return "", fmt.Errorf("%w: %s is longer than %d bytes", ErrNoSocketPath, path, maxSocketPath)
	}
	return path, nil
}

// ListenSocket replaces a leftover socket file and listens with 0600
// permissions inside a 0700 directory. The caller must hold the lock.
func ListenSocket(path string) (net.Listener, error) {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// net.UnixListener unlinks path when the server closes it.
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return listener, nil
}

// PeerConn is an accepted connection from a process of the server's user.
type PeerConn struct {
	net.Conn
	PID int
}

type peerListener struct {
	net.Listener
	uid int
}

// PeerListener closes connections from other users before any byte is read
// or written, and records the peer PID of the rest.
func PeerListener(l net.Listener) net.Listener {
	return &peerListener{Listener: l, uid: os.Geteuid()}
}

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		unixConn, ok := conn.(*net.UnixConn)
		if !ok {
			_ = conn.Close()
			continue
		}
		uid, pid, err := PeerCred(unixConn)
		if err != nil || uid != l.uid {
			_ = conn.Close()
			continue
		}
		return &PeerConn{Conn: conn, PID: pid}, nil
	}
}
