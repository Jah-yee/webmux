//go:build darwin

package runtime

import (
	"fmt"
	"net"
	"strconv"

	"golang.org/x/sys/unix"
)

const (
	// sun_path is 104 bytes including the terminating NUL.
	maxSocketPath = 103
	darwinZombie  = 5 // SZOMB
)

// ProcessIdentity uses the kernel's microsecond process start time.
func ProcessIdentity(pid int) (Identity, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || info.Proc.P_pid != int32(pid) {
		return Identity{}, fmt.Errorf("%w: PID %d", ErrNoProcess, pid)
	}
	if info.Proc.P_stat == darwinZombie {
		return Identity{}, fmt.Errorf("%w: PID %d has exited", ErrNoProcess, pid)
	}
	start := int64(info.Proc.P_starttime.Sec)*1_000_000 + int64(info.Proc.P_starttime.Usec)
	return Identity{Start: strconv.FormatInt(start, 10)}, nil
}

// PeerCred returns the effective UID (LOCAL_PEERCRED) and PID
// (LOCAL_PEERPID) of the connected process.
func PeerCred(conn *net.UnixConn) (uid, pid int, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *unix.Xucred
	var credErr, pidErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		pid, pidErr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); err != nil {
		return 0, 0, err
	}
	if credErr != nil {
		return 0, 0, credErr
	}
	if pidErr != nil {
		return 0, 0, pidErr
	}
	return int(cred.Uid), pid, nil
}
