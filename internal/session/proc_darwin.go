//go:build darwin

package session

import (
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// XNU keeps p_stat at SRUN for every live process; run state comes from the
// task's running-thread count, as ps derives it.
const (
	darwinStopped = 4 // SSTOP
	darwinZombie  = 5 // SZOMB

	procInfoCallPIDInfo = 2  // PROC_INFO_CALL_PIDINFO
	procPIDTaskInfo     = 4  // PROC_PIDTASKINFO
	procPIDPathInfo     = 11 // PROC_PIDPATHINFO
)

// procTaskInfo mirrors struct proc_taskinfo from <sys/proc_info.h>.
type procTaskInfo struct {
	virtualSize, residentSize, totalUser, totalSystem, threadsUser, threadsSystem uint64
	policy, faults, pageins, cowFaults, messagesSent, messagesReceived            int32
	syscallsMach, syscallsUnix, csw, threadnum, numrunning, priority              int32
}

// Read once per probe, not once per session. Only the service user's processes
// are inspected further: others cannot be signalled or read anyway.
func processes() processTable {
	list, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	uid := uint32(os.Geteuid())
	table := make(processTable, len(list))
	for i := range list {
		p := &list[i]
		pid := int(p.Proc.P_pid)
		state, sid := byte('?'), 0
		switch {
		case p.Proc.P_stat == darwinZombie:
			state = 'Z'
		case p.Eproc.Ucred.Uid != uid:
		case p.Proc.P_stat == darwinStopped:
			state = 'T'
		default:
			var info procTaskInfo
			// x/sys has no proc_pidinfo wrapper; on darwin Syscall6 is routed
			// through libSystem's syscall(2) rather than a raw kernel trap.
			n, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDTaskInfo, 0, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)) //nolint:staticcheck // see above
			if errno == 0 && n == unsafe.Sizeof(info) {
				state = 'S'
				if info.numrunning > 0 {
					state = 'R'
				}
			}
		}
		if p.Eproc.Ucred.Uid == uid {
			if value, err := unix.Getsid(pid); err == nil {
				sid = value
			}
		}
		start := uint64(p.Proc.P_starttime.Sec)*1_000_000 + uint64(p.Proc.P_starttime.Usec)
		table[pid] = processState{ppid: int(p.Eproc.Ppid), state: state, sid: sid, start: start, timer: darwinTimer(p.Proc.P_comm[:], p.Eproc.Wmesg[:])}
	}
	return table
}

// macOS omits other processes' environments from kern.procargs2, even for the
// same user, so detached descendants are found only by session and process tree.
func sessionMarked(int, []byte) bool { return false }

// kern.proc p_comm is not a reliable sleep(1) name on current macOS, and
// proc_threadinfo.pth_name is the thread name rather than the wait channel.
// The executable path is checked only for sleeping leaves.
func enrichTimer(pid int, state processState) processState {
	if state.timer || state.state != 'S' {
		return state
	}
	if sleepCommand(procPath(pid)) {
		state.timer = true
	}
	return state
}

func darwinTimer(comm, wmesg []byte) bool {
	return sleepCommand(cString(comm)) || timerWait(string(wmesg))
}

func procPath(pid int) string {
	var buf [4 * 1024]byte
	n, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDPathInfo, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))) //nolint:staticcheck // see processes
	if errno != 0 || n == 0 {
		return ""
	}
	if int(n) > len(buf) {
		n = uintptr(len(buf))
	}
	return cString(buf[:n])
}

func cString(b []byte) string {
	return cutCString(string(b))
}
