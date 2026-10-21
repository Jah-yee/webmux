//go:build windows

package winui

import (
	"errors"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	// secretLifetime is how long a copied token stays on the clipboard.
	secretLifetime = 60 * time.Second
)

// errClipboardBusy is shown when another program holds the clipboard.
var errClipboardBusy = errors.New("剪贴板被其他程序占用，请稍后重试")

// Clipboard writes text for the tray. Secrets are kept out of clipboard
// history and cloud clipboard and cleared after a minute unless the user
// copied something else meanwhile.
type Clipboard struct {
	// Owner is the window that owns clipboard data; 0 works on current
	// Windows versions but an owner is more reliable.
	Owner func() windows.HWND

	mu       sync.Mutex
	secretAt uint32
	timer    *time.Timer
}

func sequence() uint32 {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(r)
}

func registerFormat(name string) uintptr {
	r, _, _ := procRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(utf16Ptr(name))))
	return r
}

// open must run on a locked OS thread: the clipboard is opened per thread.
func (c *Clipboard) open() error {
	var owner windows.HWND
	if c.Owner != nil {
		owner = c.Owner()
	}
	for range 10 {
		if r, _, _ := procOpenClipboard.Call(uintptr(owner)); r != 0 {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errClipboardBusy
}

func setData(format uintptr, data []byte) error {
	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if h == 0 {
		return lastError(h, err)
	}
	p, _, err := procGlobalLock.Call(h)
	if p == 0 {
		_, _, _ = procGlobalFree.Call(h)
		return lastError(p, err)
	}
	// The locked block is not Go memory, so copy with RtlMoveMemory rather
	// than converting the address to an unsafe.Pointer.
	_, _, _ = procRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	_, _, _ = procGlobalUnlock.Call(h)
	if r, _, err := procSetClipboardData.Call(format, h); r == 0 {
		_, _, _ = procGlobalFree.Call(h)
		return lastError(r, err)
	}
	return nil
}

// Write replaces the clipboard with text. A secret is excluded from
// clipboard history, cloud clipboard and monitoring tools, and cleared
// after secretLifetime if the clipboard has not changed since.
func (c *Clipboard) Write(text string, secret bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := c.open(); err != nil {
		return err
	}
	err := c.fill(text, secret)
	_, _, _ = procCloseClipboard.Call()
	if err != nil {
		return err
	}
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.secretAt = 0
	if secret {
		c.secretAt = sequence()
		at := c.secretAt
		c.timer = time.AfterFunc(secretLifetime, func() { c.clearIf(at) })
	}
	return nil
}

func (c *Clipboard) fill(text string, secret bool) error {
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return lastError(r, err)
	}
	utf16 := windows.StringToUTF16(text)
	if err := setData(cfUnicodeText, unsafe.Slice((*byte)(unsafe.Pointer(&utf16[0])), len(utf16)*2)); err != nil {
		return err
	}
	clear(utf16)
	if !secret {
		return nil
	}
	zero := []byte{0, 0, 0, 0}
	for _, name := range []string{"ExcludeClipboardContentFromMonitorProcessing", "CanIncludeInClipboardHistory", "CanUploadToCloudClipboard"} {
		format := registerFormat(name)
		if format == 0 {
			continue
		}
		if err := setData(format, zero); err != nil {
			return err
		}
	}
	return nil
}

// clearIf empties the clipboard if it still holds the secret written at seq.
func (c *Clipboard) clearIf(seq uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.secretAt != seq || sequence() != seq {
		return
	}
	c.secretAt = 0
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if c.open() != nil {
		return
	}
	_, _, _ = procEmptyClipboard.Call()
	_, _, _ = procCloseClipboard.Call()
}

// Flush clears a pending secret now, for tray exit.
func (c *Clipboard) Flush() {
	c.mu.Lock()
	at := c.secretAt
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	if at != 0 {
		c.clearIf(at)
	}
}
