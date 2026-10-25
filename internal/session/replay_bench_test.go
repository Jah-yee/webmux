package session

import (
	"bytes"
	"fmt"
	"testing"
)

func BenchmarkExpiredResumeReplay(b *testing.B) {
	s := &Session{ring: NewRingBuffer(2 << 20), info: Info{Generation: "generation"}}
	s.ring.Write(bytes.Repeat([]byte("x"), 3<<20))
	for _, expired := range []bool{false, true} {
		b.Run(fmt.Sprintf("expired=%v", expired), func(b *testing.B) {
			sub := &Subscriber{Out: make(chan Message, 16), Slow: make(chan struct{})}
			var resume *ResumePoint
			if expired {
				resume = &ResumePoint{Generation: s.info.Generation, Offset: 0}
			}
			b.ReportAllocs()
			for b.Loop() {
				s.replayLocked(sub, resume, nil)
				for len(sub.Out) > 0 {
					<-sub.Out
				}
			}
		})
	}
}
