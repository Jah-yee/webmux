package session

import "github.com/0magnet/xterm-go/vt"

// colorSchemeMode is DEC private mode 2031: the program asks to receive
// CSI ? 997 ; 1|2 n whenever the terminal switches between dark and light.
// Browsers send those reports, but a program usually enables the mode once at
// startup, so the session must remember it beyond the bounded replay history.
const colorSchemeMode = 2031

func (s *Preview) trackColorScheme() {
	handler := s.screen.InputHandler()
	mode := func(final string, enabled bool) {
		handler.RegisterCsiHandler(vt.FunctionID{Prefix: "?", Final: final}, func(params *vt.Params) bool {
			for _, mode := range params.Params[:params.Length] {
				if mode == colorSchemeMode {
					s.colorSchemeReports = enabled
				}
			}
			// The observer's own handling of other modes in the sequence continues.
			return false
		})
	}
	mode("h", true)
	mode("l", false)
	// RIS restores every mode's default.
	handler.RegisterEscHandler(vt.FunctionID{Final: "c"}, func() bool {
		s.colorSchemeReports = false
		return false
	})
}
