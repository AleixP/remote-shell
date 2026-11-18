//go:build !windows

package server

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// settle waits for the goroutine count to drop to at most max.
func settle(t *testing.T, max int) int {
	t.Helper()
	var n int
	for i := 0; i < 100; i++ {
		runtime.GC()
		if n = runtime.NumGoroutine(); n <= max {
			return n
		}
		time.Sleep(50 * time.Millisecond)
	}
	return n
}

// outputBetween returns the terminal lines printed between two marker lines.
//
// macOS PTYs occasionally emit "\r\r\n" instead of "\r\n" when many terminals
// are flooding at once, even when reading the PTY directly with no agent
// involved. It is a kernel quirk and harmless (xterm.js renders both the same),
// so it is normalized here: the test is about lost, repeated or reordered data.
func outputBetween(out, start, end string) []string {
	out = strings.ReplaceAll(out, "\r\r\n", "\r\n")
	i := strings.LastIndex(out, "\n"+start+"\r")
	j := strings.LastIndex(out, "\n"+end+"\r")
	if i < 0 || j < i {
		return nil
	}
	return strings.Split(strings.Trim(out[i+len(start)+2:j], "\r\n"), "\r\n")
}

// Many terminals at once, each printing a long numbered stream: every client
// must receive exactly its own stream, complete and in order.
func TestManyConcurrentSessionsStayIsolated(t *testing.T) {
	const sessions, lines = 12, 3000
	ts := startServerMax(t, sessions+4)
	tok := ts.pair(t, "phone").AccessToken

	clients := make([]*wsClient, sessions)
	for i := range clients {
		clients[i] = ts.dial(t, tok, ts.newSession(t, tok))
		clients[i].resume(0)
	}
	for i, w := range clients {
		w.sendInput(fmt.Sprintf("echo S$((%d+1000)); seq 1 %d; echo E$((%d+1000))\n", i, lines, i))
	}
	for i, w := range clients {
		end := strconv.Itoa(1000 + i) // the result of $((i+1000))
		w.waitOutput("E" + end)
		got := outputBetween(w.out.String(), "S"+end, "E"+end)
		if len(got) != lines {
			t.Fatalf("session %d received %d lines, want %d", i, len(got), lines)
		}
		for n, l := range got {
			if l != strconv.Itoa(n+1) {
				t.Fatalf("session %d line %d = %q, want %d", i, n, l, n+1)
			}
		}
		for j := range clients {
			if j != i && strings.Contains(w.out.String(), "E"+strconv.Itoa(1000+j)+"\r") {
				t.Errorf("session %d received the output of session %d", i, j)
			}
		}
	}
	for i := 1; i < len(clients); i++ {
		if clients[i].seqs[0] != 1 {
			t.Errorf("session %d output does not start at sequence 1", i)
		}
	}
}

// Clients that vanish mid-stream, again and again, must leave the session
// alive, never lose or repeat output, and not leak goroutines.
func TestAbruptDisconnectsNeverLoseOrRepeatOutput(t *testing.T) {
	ts := startServerOpts(t, 8, 0)
	tok := ts.pair(t, "phone").AccessToken
	id := ts.newSession(t, tok)

	first := ts.dial(t, tok, id)
	first.resume(0)
	first.sendInput("i=0; while true; do i=$((i+1)); echo N$i; sleep 0.005; done\n")
	first.waitOutput("N3\r")
	last := first.seqs[len(first.seqs)-1]
	_ = first.c.CloseNow()
	base := runtime.NumGoroutine()

	seen := map[uint64]bool{}
	for round := 0; round < 25; round++ {
		var c *websocket.Conn
		var err error
		for deadline := time.Now().Add(5 * time.Second); ; {
			if c, _, err = ts.dialRaw(id, ts.ticket(t, tok, id)); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("round %d: cannot reconnect: %v", round, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		c.SetReadLimit(1 << 20)
		w := &wsClient{t: t, c: c}
		res := w.resume(last)
		if res["buffer_truncated"] == true {
			t.Fatalf("round %d: output lost although the buffer is far from full", round)
		}
		// Read a few frames, then vanish without a closing handshake.
		for n := len(w.seqs); n < 5; {
			if err := w.next(); err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
			n = len(w.seqs)
		}
		for _, s := range w.seqs {
			if seen[s] {
				t.Fatalf("round %d: sequence %d delivered twice", round, s)
			}
			if s <= last {
				t.Fatalf("round %d: sequence %d at or before %d", round, s, last)
			}
			seen[s] = true
			last = s
		}
		_ = c.CloseNow()
	}

	sess, err := ts.sessions.Get(id)
	if err != nil {
		t.Fatalf("session lost: %v", err)
	}
	select {
	case <-sess.Terminal.Done():
		t.Fatal("the shell died with its clients")
	default:
	}
	if n := settle(t, base+3); n > base+3 {
		t.Errorf("goroutines grew from %d to %d after 25 abrupt disconnects", base, n)
	}
}

// A client that stops reading while its terminal floods must not stall the
// terminal, other sessions, or the agent.
func TestSlowClientDoesNotStallOthers(t *testing.T) {
	ts := startServerOpts(t, 8, 0)
	tok := ts.pair(t, "phone").AccessToken
	slowID := ts.newSession(t, tok)
	okID := ts.newSession(t, tok)

	// The slow client connects, asks for a flood and never reads again.
	c, _, err := ts.dialRaw(slowID, ts.ticket(t, tok, slowID))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	slow := &wsClient{t: t, c: c}
	slow.resume(0)
	slow.sendInput("seq 1 400000; echo FLOODDONE$((1+1))\n")

	// Meanwhile another session answers promptly.
	good := ts.dial(t, tok, okID)
	good.resume(0)
	good.sendInput(calcInput)
	good.waitOutput(calcWant)

	// The flooded shell is not blocked: its output keeps being buffered.
	sess, _ := ts.sessions.Get(slowID)
	deadline := time.Now().Add(20 * time.Second)
	for sess.Output.LastSeq() < 20 {
		if time.Now().After(deadline) {
			t.Fatal("the flooding terminal made no progress while its client was not reading")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := ts.sessions.Get(slowID); err != nil {
		t.Fatalf("flooded session lost: %v", err)
	}
	_ = context.Background()
}

// Opening and closing many sessions must not leave goroutines behind.
func TestSessionChurnDoesNotLeakGoroutines(t *testing.T) {
	ts := startServerMax(t, 8)
	tok := ts.pair(t, "phone").AccessToken
	base := runtime.NumGoroutine()

	for i := 0; i < 40; i++ {
		id := ts.newSession(t, tok)
		w := ts.dial(t, tok, id)
		w.resume(0)
		if i%2 == 0 {
			w.sendInput(exitInput) // some end by themselves
			w.waitControl("session.exited")
		} else {
			if resp := ts.call(t, "DELETE", "/api/v1/sessions/"+id, tok, "", nil); resp.StatusCode != 200 {
				t.Fatalf("close = %d", resp.StatusCode)
			}
		}
		_ = w.c.CloseNow()
	}
	if n := settle(t, base+3); n > base+3 {
		t.Errorf("goroutines grew from %d to %d after 40 sessions", base, n)
	}
	if n := len(ts.sessions.List()); n != 0 {
		t.Errorf("%d sessions left in the manager", n)
	}
}
