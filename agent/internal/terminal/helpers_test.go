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

func collect(t *Terminal) *collector {
	c := &collector{}
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := t.Read(b)
			if n > 0 {
				c.mu.Lock()
				c.buf.Write(b[:n])
				c.mu.Unlock()
			}
			if err != nil {
				return
			}
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
