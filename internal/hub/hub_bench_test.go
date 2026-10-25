package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
	"golang.org/x/term"
)

const hubOutputHelper = "webmux-hub-output-helper"

// This subprocess writes real PTY output. Keeping the workload in the test
// binary avoids shell buffering, external utilities and terminal echo.
func TestHubOutputProcess(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != hubOutputHelper {
		return
	}
	if _, err := term.MakeRaw(int(os.Stdin.Fd())); err != nil {
		os.Exit(2)
	}
	if _, err := os.Stdout.Write([]byte("READY")); err != nil {
		os.Exit(2)
	}
	var command [1]byte
	enabled := false
	for {
		if _, err := io.ReadFull(os.Stdin, command[:]); err != nil {
			os.Exit(0)
		}
		size, chunk := hubOutputSize(command[0])
		body := hubOutputBytes(size)
		for start := 0; start < len(body); start += chunk {
			if _, err := os.Stdout.Write(body[start:min(start+chunk, len(body))]); err != nil {
				os.Exit(2)
			}
		}
		enabled = !enabled
		if _, err := os.Stdout.Write(hubOutputMode(enabled)); err != nil {
			os.Exit(2)
		}
	}
}

func hubOutputSize(command byte) (size, chunk int) {
	switch command {
	case 's':
		return 64 * 1024, 1024
	case 'l':
		return 256 * 1024, 256 * 1024
	default:
		return 1024, 1024
	}
}

func hubOutputBytes(size int) []byte {
	p := make([]byte, size)
	for i := range p {
		p[i] = byte('a' + (i/80+i%80)%26)
		if i%80 == 79 {
			p[i] = '\r'
		}
	}
	return p
}

func hubOutputMode(enabled bool) []byte {
	if enabled {
		return []byte("\x1b[?2031h")
	}
	return []byte("\x1b[?2031l")
}

// BenchmarkAttachOutput uses only APIs available before the aggregation change,
// so this file can be copied unchanged to the baseline worktree. It measures
// the complete real session -> Attach -> WebSocket path, not a batching model.
func BenchmarkAttachOutput(b *testing.B) {
	for _, workload := range []struct {
		name    string
		command byte
		phases  int
	}{
		{"continuous", 's', 1},
		{"control-boundaries", 'b', 4},
		{"large-write", 'l', 1},
	} {
		b.Run(workload.name, func(b *testing.B) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			manager := session.NewManager(session.Options{DefaultCwd: b.TempDir()})
			defer func() {
				shutdown, stop := context.WithTimeout(context.Background(), 7*time.Second)
				defer stop()
				manager.Shutdown(shutdown)
			}()
			executable, err := os.Executable()
			if err != nil {
				b.Fatal(err)
			}
			s, err := manager.Create("", "custom", []string{executable, "-test.run=^TestHubOutputProcess$", "--", hubOutputHelper}, nil, "", nil)
			if err != nil {
				b.Fatal(err)
			}
			observer := s.Attach()
			ready := make([]byte, 0, 5)
			startup, stopStartup := context.WithTimeout(ctx, 10*time.Second)
			defer stopStartup()
			for !bytes.Equal(ready, []byte("READY")) {
				select {
				case m := <-observer.Out:
					if len(m.Frame) > 0 {
						ready = append(ready, m.Frame[1:]...)
					}
					if m.Exit != nil || len(ready) > 5 {
						b.Fatal("output helper failed to start")
					}
				case <-startup.Done():
					b.Fatal("output helper startup timed out")
				}
			}
			s.Detach(observer)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connection, err := websocket.Accept(w, r, nil)
				if err != nil {
					return
				}
				Attach(ctx, s, connection, session.AttachOptions{})
			}))
			defer server.Close()
			client, _, err := websocket.Dial(startup, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = client.CloseNow() }()
			client.SetReadLimit(2 * 1024 * 1024)
			for _, want := range []byte{contract.FrameControl, contract.FramePosition, contract.FrameSnapshot, contract.FrameState} {
				kind, frame, err := client.Read(startup)
				if err != nil || kind != websocket.MessageBinary || len(frame) == 0 || frame[0] != want {
					b.Fatalf("initial replay: type=%v frame=%x err=%v, want %d", kind, frame, err, want)
				}
			}
			size, _ := hubOutputSize(workload.command)
			body := hubOutputBytes(size)
			want := [2][]byte{
				append(append([]byte(nil), body...), hubOutputMode(false)...),
				append(append([]byte(nil), body...), hubOutputMode(true)...),
			}
			command := []byte{workload.command}
			enabled := false
			b.ReportAllocs()
			b.SetBytes(int64(len(want[0]) * workload.phases))
			b.ResetTimer()
			for range b.N {
				for range workload.phases {
					enabled = !enabled
					index := 0
					if enabled {
						index = 1
					}
					expected := want[index]
					iteration, stop := context.WithTimeout(ctx, 10*time.Second)
					if err := s.WriteContext(iteration, command); err != nil {
						stop()
						b.Fatal(err)
					}
					position := 0
					for {
						kind, frame, err := client.Read(iteration)
						if err != nil || kind != websocket.MessageBinary || len(frame) == 0 {
							stop()
							b.Fatalf("output read: type=%v err=%v", kind, err)
						}
						switch frame[0] {
						case contract.FrameOutput:
							payload := frame[1:]
							if len(payload) > len(expected)-position || !bytes.Equal(payload, expected[position:position+len(payload)]) {
								stop()
								b.Fatalf("output changed or reordered at byte %d", position)
							}
							position += len(payload)
						case contract.FrameControl:
							var state struct {
								ColorSchemeReports bool `json:"colorSchemeReports"`
							}
							if err := json.Unmarshal(frame[1:], &state); err != nil || state.ColorSchemeReports != enabled || position != len(expected) {
								stop()
								b.Fatalf("control crossed output boundary: bytes=%d want=%d mode=%v err=%v", position, len(expected), state.ColorSchemeReports, err)
							}
						case contract.FrameState:
							continue
						default:
							stop()
							b.Fatalf("unexpected frame %d", frame[0])
						}
						if frame[0] == contract.FrameControl {
							break
						}
					}
					stop()
				}
			}
			b.StopTimer()
		})
	}
}
