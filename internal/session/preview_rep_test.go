package session

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/0magnet/xterm-go/vt"
)

func TestPreviewBoundsRepeatWork(t *testing.T) {
	var preview Preview
	preview.Feed([]byte("x"))
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	preview.Feed([]byte("\x1b[1000000b"))
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
		t.Fatalf("short REP allocated %d bytes", allocated)
	}
	preview.Feed([]byte("\r\ncontinued"))
	if !strings.Contains(preview.text, "continued") {
		t.Fatal("preview stopped processing after REP")
	}
}

func TestPreviewBoundsCursorAndRowRepeats(t *testing.T) {
	for _, final := range []string{"I", "Z", "L", "M", "S", "T"} {
		for _, setup := range []string{"", "\x1b[2;4r", "\x1b[?1049h\x1b[2;4r"} {
			for _, count := range []int{2, 100000} {
				t.Run(fmt.Sprintf("%s/%q/%d", final, setup, count), func(t *testing.T) {
					var got Preview
					got.Resize(80, 24)
					got.Resize(40, 6) // Bounds must follow resizes, not creation dimensions.
					options := vt.NewOptions()
					options.Cols, options.Rows, options.Scrollback = 40, 6, 0
					want := Preview{screen: vt.NewTerminal(options)} // Unmodified VT semantics.
					prefix := setup + "\x1b[1;1Hone\x1b[2;1Htwo\x1b[3;1Hthree\x1b[4;1Hfour\x1b[5;1Hfive\x1b[3;17H"
					got.Feed([]byte(prefix))
					want.Feed([]byte(prefix))
					limit := want.screen.Buffer().ScrollBottom - want.screen.Buffer().ScrollTop + 1
					if final == "I" || final == "Z" {
						limit = want.screen.Cols()
					}
					var before, after runtime.MemStats
					runtime.ReadMemStats(&before)
					// Split CSI parameters must still hit the bounded handlers.
					got.Feed([]byte(fmt.Sprintf("\x1b[%d", count)))
					got.Feed([]byte(final))
					runtime.ReadMemStats(&after)
					if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
						t.Fatalf("CSI allocated %d bytes", allocated)
					}
					want.Feed([]byte(fmt.Sprintf("\x1b[%d%s", min(count, limit), final)))
					if got.text != want.text || got.screen.Buffer().X != want.screen.Buffer().X || got.screen.Buffer().Y != want.screen.Buffer().Y {
						t.Fatalf("bounded CSI differs from VT: got %q at %d,%d; want %q at %d,%d",
							got.text, got.screen.Buffer().X, got.screen.Buffer().Y,
							want.text, want.screen.Buffer().X, want.screen.Buffer().Y)
					}
					got.Feed([]byte("\r\ncontinued"))
					if !strings.Contains(got.text, "continued") {
						t.Fatal("preview stopped after CSI")
					}
				})
			}
		}
	}
}

func BenchmarkPreviewScrollRepeat(b *testing.B) {
	for _, count := range []int{24, 100000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			var p Preview
			p.Resize(80, 24)
			sequence := []byte(fmt.Sprintf("\x1b[%dS", count))
			b.ReportAllocs()
			for b.Loop() {
				p.Feed(sequence)
			}
		})
	}
}

func TestShutdownDuringRepeatedPreviewControls(t *testing.T) {
	t.Parallel()
	m := NewManager(Options{DefaultCwd: t.TempDir()})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Shutdown(ctx)
	})
	s, err := m.Create("", []string{"/bin/sh", "-c",
		`while :; do printf '\033[100000Smarker\n'; sleep 0.01; done`,
	}, "", nil)

	if err != nil {
		t.Fatal(err)
	}
	// The preview's count clamp must never rewrite the original PTY stream.
	waitForOutput(t, s, "\x1b[100000Smarker")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stopped := make(chan struct{})
	go func() {
		m.Shutdown(ctx)
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("preview output prevented bounded shutdown")
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("session cleanup did not finish")
	}
}

func TestPreviewPreservesOrdinarySplitRepeat(t *testing.T) {
	var preview Preview
	for _, part := range []string{"x\x1b[", "3", "b"} {
		preview.Feed([]byte(part))
	}
	if preview.text != "xxxx" {
		t.Fatalf("ordinary REP = %q", preview.text)
	}
}
