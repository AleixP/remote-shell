package terminal

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// collector accumulates everything a terminal prints.
type collector struct {
	mu  sync.Mutex
	buf strings.Builder
}

func collectSession(s *Session) *collector {
	c := &collector{}
	go func() {
		var seq uint64
		for {
			frames, _, closed, wake := s.Output.Snapshot(seq)
			for _, f := range frames {
				c.mu.Lock()
				c.buf.Write(f.Data)
				c.mu.Unlock()
				seq = f.Seq
			}
			if closed && len(frames) == 0 {
				return
			}
			<-wake
		}
	}()
	return c
}

func (c *collector) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *collector) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(c.String(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %q, got %q", want, c.String())
}
