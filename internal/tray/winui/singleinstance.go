//go:build windows

package winui

import (
	"errors"
	"time"

	"golang.org/x/sys/windows"
)

// Instance is the single tray instance of this Windows user session.
//
// The design asks the second instance to PostMessage the hidden window of
// the first. fyne.io/systray's window has a generic class and no title, so
// the first instance instead waits on a named auto-reset event that the
// second instance sets; the effect is the same.
type Instance struct {
	mutex windows.Handle
	event windows.Handle
}

func userSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return user.User.Sid.String(), nil
}

// AcquireInstance takes `Local\WebMuxTray-<SID>`. When another instance
// holds it, AcquireInstance wakes that instance and returns nil, nil.
func AcquireInstance() (*Instance, error) {
	sid, err := userSID()
	if err != nil {
		return nil, err
	}
	eventName := utf16Ptr(`Local\WebMuxTrayWake-` + sid)
	mutex, err := windows.CreateMutex(nil, false, utf16Ptr(`Local\WebMuxTray-`+sid))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(mutex)
		wake(eventName)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	event, err := windows.CreateEvent(nil, 0, 0, eventName)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(mutex)
		return nil, err
	}
	return &Instance{mutex: mutex, event: event}, nil
}

// wake sets the first instance's event; it may still be creating it.
func wake(name *uint16) {
	for range 20 {
		event, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
		if err == nil {
			_ = windows.SetEvent(event)
			_ = windows.CloseHandle(event)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// Wakes calls f each time another instance starts.
func (i *Instance) Wakes(f func()) {
	go func() {
		for {
			r, err := windows.WaitForSingleObject(i.event, windows.INFINITE)
			if err != nil || r != windows.WAIT_OBJECT_0 {
				return
			}
			f()
		}
	}()
}
