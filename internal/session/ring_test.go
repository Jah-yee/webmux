package session

import (
	"bytes"
	"errors"
	"testing"
)

func TestNewRingBufferRejectsEmptyCapacity(t *testing.T) {
	if _, err := NewRingBuffer(0); !errors.Is(err, ErrRingCapacity) {
		t.Fatalf("err = %v", err)
	}
}

func TestRingWrap(t *testing.T) {
	r := mustRing(t, 5)
	for _, step := range []struct{ in, want string }{{"ab", "ab"}, {"cde", "abcde"}, {"fg", "cdefg"}, {"1234567", "34567"}, {"8", "45678"}, {"", "45678"}} {
		r.Write([]byte(step.in))
		if got := string(r.Snapshot()); got != step.want {
			t.Fatalf("got %q want %q", got, step.want)
		}
	}
	snap := r.Snapshot()
	snap[0] = 'x'
	if bytes.Equal(snap, r.Snapshot()) {
		t.Fatal("snapshot aliases storage")
	}
}
func TestRingBinary(t *testing.T) {
	r := mustRing(t, 4)
	r.Write([]byte{0, 27, 255})
	r.Write([]byte{1, 2})
	if !bytes.Equal(r.Snapshot(), []byte{27, 255, 1, 2}) {
		t.Fatal(r.Snapshot())
	}
}
func TestRingOffsets(t *testing.T) {
	r := mustRing(t, 5)
	r.Write([]byte("abc"))
	if start, end := r.Range(); start != 0 || end != 3 {
		t.Fatalf("range %d..%d", start, end)
	}
	r.Write([]byte("defg"))
	if start, end := r.Range(); start != 2 || end != 7 {
		t.Fatalf("wrapped range %d..%d", start, end)
	}
	for _, step := range []struct {
		offset uint64
		want   string
		ok     bool
	}{{2, "cdefg", true}, {4, "efg", true}, {7, "", true}, {1, "", false}, {8, "", false}} {
		got, ok := r.sinceWithPrefix([]byte{9}, step.offset)
		if ok != step.ok || (ok && (got[0] != 9 || string(got[1:]) != step.want)) || (!ok && got != nil) {
			t.Fatalf("since %d: %q %v", step.offset, got, ok)
		}
	}
	r.Write([]byte("0123456789"))
	if got, ok := r.sinceWithPrefix(nil, 12); !ok || string(got) != "56789" {
		t.Fatalf("oversized write: %q %v", got, ok)
	}
}

func TestRejectedRingOffsetDoesNotCopyHistory(t *testing.T) {
	r := mustRing(t, 2<<20)
	r.Write(bytes.Repeat([]byte("x"), 3<<20))
	for _, offset := range []uint64{0, 4 << 20} {
		if allocs := testing.AllocsPerRun(100, func() {
			if got, ok := r.sinceWithPrefix([]byte{9}, offset); ok || got != nil {
				t.Fatal("invalid offset returned history")
			}
		}); allocs != 0 {
			t.Fatalf("invalid offset allocated %v times", allocs)
		}
	}
	if got := r.snapshotWithPrefix([]byte{9}); len(got) != 1+2<<20 || got[0] != 9 || got[1] != 'x' {
		t.Fatal("full snapshot lost retained history")
	}
}

func mustRing(tb testing.TB, capacity int) *RingBuffer {
	tb.Helper()
	r, err := NewRingBuffer(capacity)
	if err != nil {
		tb.Fatal(err)
	}
	return r
}
