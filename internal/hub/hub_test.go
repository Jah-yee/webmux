package hub

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/rainchan/webmux/internal/contract"
	"github.com/rainchan/webmux/internal/session"
)

func TestEncodeSharesRawFrames(t *testing.T) {
	for _, frame := range [][]byte{{0}, {1}, {0, 'h', 'i'}, {1, 'x'}} {
		encoded, exit, err := encode(session.Message{Frame: frame})
		if err != nil || exit || !bytes.Equal(encoded, frame) || &encoded[0] != &frame[0] {
			t.Fatalf("raw frame was rejected, changed or copied: exit=%v err=%v", exit, err)
		}
	}
}

func invalidMessages() map[string]session.Message {
	return map[string]session.Message{
		"empty":       {},
		"empty-raw":   {Frame: []byte{}},
		"raw-control": {Frame: []byte{contract.FrameState, '{', '}'}},
		"unknown-raw": {Frame: []byte{255}},
		"multiple":    {Frame: []byte{contract.FrameOutput, 'x'}, Control: &session.ControlState{}},
		// time.Time implements JSON marshaling and can reject a typed payload.
		"unencodable-state":   {Info: &session.Info{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}},
		"unencodable-control": {Control: &session.ControlState{Controller: &session.Controller{Since: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}},
	}
}

func TestEncodeRejectsInvalidMessages(t *testing.T) {
	for name, message := range invalidMessages() {
		t.Run(name, func(t *testing.T) {
			frame, _, err := encode(message)
			if err == nil || frame != nil {
				t.Fatalf("invalid message encoded: frame=%x err=%v", frame, err)
			}
		})
	}
}

// writerClient drives the same writer as Attach over a real WebSocket, with a
// controlled queue so invalid producers and precise ordering are reproducible.
func writerClient(t *testing.T, messages []session.Message) (context.Context, *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	sub := &session.Subscriber{Out: make(chan session.Message, len(messages)), Slow: make(chan struct{})}
	for _, message := range messages {
		sub.Out <- message
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = connection.CloseNow() }()
		writeMessages(ctx, connection, sub)
	}))
	t.Cleanup(server.Close)
	client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	client.SetReadLimit(1024 * 1024)
	t.Cleanup(func() { _ = client.CloseNow() })
	return ctx, client
}

func TestWriterClosesOnInvalidMessage(t *testing.T) {
	for name, message := range invalidMessages() {
		for _, pending := range []bool{false, true} {
			path := "direct"
			if pending {
				path = "after-output"
			}
			t.Run(name+"/"+path, func(t *testing.T) {
				messages := []session.Message{message}
				if pending {
					messages = append([]session.Message{{Frame: []byte{0, 'o', 'k'}}}, messages...)
				}
				ctx, client := writerClient(t, messages)
				if pending {
					kind, frame, err := client.Read(ctx)
					if err != nil || kind != websocket.MessageBinary || !bytes.Equal(frame, []byte{0, 'o', 'k'}) {
						t.Fatalf("valid output preceding error was lost: frame=%x err=%v", frame, err)
					}
				}
				_, _, err := client.Read(ctx)
				var closeError websocket.CloseError
				if !errors.As(err, &closeError) || closeError.Code != websocket.StatusInternalError || closeError.Reason != "invalid session message" {
					t.Fatalf("invalid message did not close safely: %v", err)
				}
			})
		}
	}
}

func TestWriterPreservesBoundariesExitAndSharedChunks(t *testing.T) {
	// Spare capacity is deliberately populated: appending into a subscriber's
	// shared frame would corrupt bytes even if its visible prefix stayed intact.
	storage := bytes.Repeat([]byte{'!'}, 128)
	copy(storage, []byte{0, 'f', 'i', 'r', 's', 't'})
	first := storage[:6]
	before := bytes.Clone(storage)
	second := []byte{0, 's', 'e', 'c', 'o', 'n', 'd'}
	medium := append([]byte{0}, bytes.Repeat([]byte{'M'}, 40000)...)
	large := append([]byte{0}, bytes.Repeat([]byte{'L'}, 70000)...)
	snapshot := []byte{1, 's'}
	last := []byte{0, 't', 'a', 'i', 'l'}
	code := 7
	messages := []session.Message{
		{Frame: first}, {Frame: second}, {Control: &session.ControlState{Cols: 80, Rows: 24}},
		{Frame: medium}, {Frame: large}, {Frame: snapshot}, {Frame: last}, {Exit: &code},
		// Exit is terminal: nothing queued after it may reach the connection.
		{Frame: []byte{0, 'n', 'o'}},
	}
	for range 2 {
		ctx, client := writerClient(t, messages)
		for _, want := range [][]byte{[]byte("\x00firstsecond"), {contract.FrameControl}, medium, large, snapshot, last, {contract.FrameExit}} {
			kind, frame, err := client.Read(ctx)
			if err != nil || kind != websocket.MessageBinary || len(frame) == 0 || frame[0] != want[0] {
				t.Fatalf("frame order changed: frame=%x err=%v, want type %d", frame, err, want[0])
			}
			if len(want) > 1 && !bytes.Equal(frame, want) {
				t.Fatalf("output bytes changed for type %d: got %d bytes, want %d", want[0], len(frame), len(want))
			}
		}
		if _, _, err := client.Read(ctx); websocket.CloseStatus(err) != 4410 {
			t.Fatalf("exit was not the final frame: %v", err)
		}
		if !bytes.Equal(storage, before) || !bytes.Equal(second, []byte("\x00second")) || !bytes.Equal(medium[1:], bytes.Repeat([]byte{'M'}, 40000)) || !bytes.Equal(large[1:], bytes.Repeat([]byte{'L'}, 70000)) {
			t.Fatal("writer modified a shared raw chunk")
		}
	}
}
