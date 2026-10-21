//go:build windows

package winui

import (
	"fmt"

	"golang.org/x/sys/windows"
)

const dialogTitle = "WebMux"

const (
	idOK  = 1
	idYes = 6
	idNo  = 7
)

func messageBox(text string, flags uint32) int32 {
	ret, _ := windows.MessageBox(0, utf16Ptr(text), utf16Ptr(dialogTitle), flags|windows.MB_SETFOREGROUND|windows.MB_TOPMOST)
	return ret
}

// confirm asks an OK/Cancel question.
func confirm(text string) bool {
	return messageBox(text, windows.MB_OKCANCEL|windows.MB_ICONWARNING|windows.MB_DEFBUTTON2) == idOK
}

// confirmSessions asks before an action that ends sessions, for example
// prefix "停止 WebMux "; it does not ask when there are none.
func confirmSessions(prefix string, sessions int) bool {
	if sessions <= 0 {
		return true
	}
	return confirm(fmt.Sprintf("%s将结束 %d 个终端会话，且不可恢复。\n\n是否继续？", prefix, sessions))
}

type quitChoice int

const (
	quitCancel quitChoice = iota
	quitStop
	quitTrayOnly
)

// askQuit is the three-way exit question for a managed running WebMux.
// MessageBoxW cannot relabel its buttons, so the text explains them.
func askQuit(sessions int, forwarding bool) quitChoice {
	text := fmt.Sprintf("WebMux 正在运行（%d 个终端会话）。\n\n是：停止 WebMux 并退出（结束所有终端会话）\n否：仅退出托盘（WebMux 继续运行）\n取消：不退出", sessions)
	if forwarding {
		text += "\n\n托盘转发会随托盘退出而停止，其他设备将无法再经转发地址访问。"
	}
	switch messageBox(text, windows.MB_YESNOCANCEL|windows.MB_ICONQUESTION|windows.MB_DEFBUTTON2) {
	case idYes:
		return quitStop
	case idNo:
		return quitTrayOnly
	}
	return quitCancel
}
