//go:build windows

package winui

import (
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW          = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW            = user32.NewProc("CallWindowProcW")
	procOpenClipboard              = user32.NewProc("OpenClipboard")
	procCloseClipboard             = user32.NewProc("CloseClipboard")
	procEmptyClipboard             = user32.NewProc("EmptyClipboard")
	procSetClipboardData           = user32.NewProc("SetClipboardData")
	procRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procGetClipboardSequenceNumber = user32.NewProc("GetClipboardSequenceNumber")
	procGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	procGlobalLock                 = kernel32.NewProc("GlobalLock")
	procGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	procGlobalFree                 = kernel32.NewProc("GlobalFree")
	procRtlMoveMemory              = kernel32.NewProc("RtlMoveMemory")
	procShellNotifyIconW           = shell32.NewProc("Shell_NotifyIconW")
)

const (
	wmUser       = 0x0400
	wmRButtonUp  = 0x0205
	gwlpWndProc  = -4
	systrayClass = "SystrayClass"
	// systrayIconID is the uID fyne.io/systray gives its notification icon.
	systrayIconID = 100
	// systrayMessage is the callback message fyne.io/systray registers.
	systrayMessage = wmUser + 1
)

var (
	findTarget uint32
	findResult windows.HWND
	findClass  [64]uint16
	enumProc   = windows.NewCallback(func(hwnd windows.HWND, _ uintptr) uintptr {
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(hwnd, &pid); err != nil || pid != findTarget {
			return 1
		}
		n, err := windows.GetClassName(hwnd, &findClass[0], int32(len(findClass)))
		if err != nil || windows.UTF16ToString(findClass[:n]) != systrayClass {
			return 1
		}
		findResult = hwnd
		return 0
	})
)

// findTrayWindow returns the hidden window fyne.io/systray created in this
// process; its notification icon is (hwnd, systrayIconID). Callers serialize.
func findTrayWindow() windows.HWND {
	findTarget = windows.GetCurrentProcessId()
	findResult = 0
	_ = windows.EnumWindows(enumProc, nil)
	return findResult
}

// subclass routes the messages of hwnd through proc. The previous window
// procedure is stored in prev before proc can run.
func subclass(hwnd windows.HWND, proc uintptr, prev *atomic.Uintptr) {
	index := gwlpWndProc
	old, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index))
	prev.Store(old)
	_, _, _ = procSetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index), proc)
}

func callWindowProc(prev uintptr, hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := procCallWindowProcW.Call(prev, uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func utf16Ptr(s string) *uint16 {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		p, _ = windows.UTF16PtrFromString("")
	}
	return p
}

// copyUTF16 copies s into dst, truncated so dst stays NUL-terminated.
func copyUTF16(dst []uint16, s string) {
	src := windows.StringToUTF16(s)
	if len(src) > len(dst) {
		src = src[:len(dst)]
		src[len(src)-1] = 0
	}
	copy(dst, src)
}

func lastError(r uintptr, err error) error {
	if r != 0 {
		return nil
	}
	if errno, ok := err.(syscall.Errno); ok && errno != 0 {
		return errno
	}
	return syscall.EINVAL
}
