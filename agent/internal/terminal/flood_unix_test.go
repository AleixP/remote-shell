//go:build !windows

package terminal

import (
	"strings"
	"testing"
	"time"
)

// A real terminal flooding tens of megabytes must neither block nor make the
// agent hold more than the configured buffer.
func TestRealFloodIsBoundedAndTheShellStaysUsable(t *testing.T) {
	m := NewManager()
	m.SetOutputBufferBytes(256 << 10)
	defer m.CloseAll()
	s, c := newTestSession(t, m, "flood")

	if _, err := s.Terminal.Write([]byte("head -c 40000000 /dev/zero | tr '\\0' x; echo DONE$((1+1))\n")); err != nil {
		t.Fatal(err)
	}
	c.waitFor(t, "DONE2")

	frames, truncated, _, _ := s.Output.Snapshot(0)
	var held int
	for _, f := range frames {
		held += len(f.Data)
	}
	if held > 256<<10+MaxFramePayload {
		t.Fatalf("the session holds %d bytes, limit %d", held, 256<<10)
	}
	if !truncated {
		t.Error("40 MB through a 256 KiB buffer must report truncation")
	}
	if s.Output.LastSeq() < 40_000_000/MaxFramePayload {
		t.Errorf("only %d frames were produced; the flood did not all arrive", s.Output.LastSeq())
	}
	// Still interactive afterwards.
	if _, err := s.Terminal.Write([]byte("echo BACK$((2+3))\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(c.String(), "BACK5") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(c.String(), "BACK5") {
		t.Error("the shell did not answer after the flood")
	}
}
