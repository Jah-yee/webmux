package session

import "testing"

func fuzzHandler() *SignalHandler {
	return &SignalHandler{
		Title:         func(string) {},
		Bell:          func() {},
		Notify:        func() {},
		Event:         func(string) {},
		Cwd:           func(string, string) {},
		ProgramStatus: func(string, string) {},
		Probe:         func() {},
		Prompt:        func() {},
		Reset:         func() {},
	}
}

func FuzzOSCScanner(f *testing.F) {
	for _, seed := range []string{
		"\x1b]0;title\a",
		"\x1b]2;title\x1b\\",
		"\x1b]777;notify;warp://cli-agent;{\"v\":1,\"event\":\"question_asked\"}\x1b\\",
		"\x1b]7501;?;states=idle,working,done,blocked,error\a",
		"\x1b]133;A\a\x1b]133;D\a",
		"\x1bPignored\a\x1b\\\x1b]0;after\a",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var scanner OSCScanner
		h := fuzzHandler()
		for len(data) > 0 {
			n := int(data[0])%8 + 1
			if n > len(data) {
				n = len(data)
			}
			scanner.Feed(data[:n], h)
			data = data[n:]
		}
		scanner.Feed(nil, h)
	})
}

func FuzzPreview(f *testing.F) {
	for _, seed := range []string{
		"plain text",
		"a\r\nb\r\nc",
		"\x1b[2J\x1b[Hhome",
		"\x1b[?1049h\x1b[Halternate\x1b[?1049l",
		"\x1b[界界abcd",
		"\x1b]0;title\a\x1b[?2031h",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		var preview Preview
		preview.Feed(data)
		preview.FeedThrottled(data)
		preview.Resize(80, 24)
		_ = preview.recoveryFrame()
		_ = preview.Feed(nil)
	})
}
