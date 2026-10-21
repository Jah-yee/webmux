//go:build windows

package winui

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	nimModify = 0x00000001
	nifInfo   = 0x00000010
	niifInfo  = 0x00000001
)

// notifyIconData is NOTIFYICONDATAW; uTimeout and uVersion share a union.
type notifyIconData struct {
	Size             uint32
	Wnd              windows.HWND
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             windows.Handle
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
	GUIDItem         windows.GUID
	BalloonIcon      windows.Handle
}

// Notifier shows balloon notifications on fyne.io/systray's own icon with
// Shell_NotifyIcon(NIM_MODIFY, NIF_INFO); Windows 10 and later render them
// as toasts without an AppUserModelID.
type Notifier struct {
	Window func() windows.HWND
	Logf   func(string, ...any)
}

// Show displays title and message. It never blocks on the user.
func (n *Notifier) Show(title, message string) {
	hwnd := n.Window()
	if hwnd == 0 {
		n.Logf("notification dropped (no tray window): %s", title)
		return
	}
	nid := notifyIconData{Wnd: hwnd, ID: systrayIconID, Flags: nifInfo, InfoFlags: niifInfo}
	nid.Size = uint32(unsafe.Sizeof(nid))
	copyUTF16(nid.InfoTitle[:], title)
	copyUTF16(nid.Info[:], message)
	if r, _, err := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid))); r == 0 {
		n.Logf("notification failed: %v", err)
	}
}
