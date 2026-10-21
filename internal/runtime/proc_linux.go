//go:build linux

package runtime

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// sun_path is 108 bytes including the terminating NUL.
const maxSocketPath = 107

// ProcessIdentity combines the boot-relative start time with the executable
// path, ignoring the " (deleted)" suffix an upgraded binary leaves behind.
func ProcessIdentity(pid int) (Identity, error) {
	dir := "/proc/" + strconv.Itoa(pid)
	data, err := os.ReadFile(dir + "/stat")
	if err != nil {
		return Identity{}, fmt.Errorf("%w: PID %d", ErrNoProcess, pid)
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return Identity{}, fmt.Errorf("parse /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return Identity{}, fmt.Errorf("parse /proc/%d/stat", pid)
	}
	if fields[0] == "Z" || fields[0] == "X" {
		return Identity{}, fmt.Errorf("%w: PID %d has exited", ErrNoProcess, pid)
	}
	exe, err := os.Readlink(dir + "/exe")
	if err != nil {
		return Identity{}, fmt.Errorf("read executable of PID %d: %w", pid, err)
	}
	return Identity{Start: fields[19], Exe: strings.TrimSuffix(exe, " (deleted)")}, nil
}

// PeerCred returns the effective UID and PID of the connected process.
func PeerCred(conn *net.UnixConn) (uid, pid int, err error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if credErr != nil {
		return 0, 0, credErr
	}
	return int(cred.Uid), int(cred.Pid), nil
}
