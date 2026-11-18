//go:build !windows

package server

import (
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// TestSoak runs the agent under constant churn for RS_SOAK_SECONDS seconds and
// checks that memory and goroutines stay flat. It is opt-in because it is slow:
//
//	RS_SOAK_SECONDS=600 go test -run TestSoak -v ./internal/server
func TestSoak(t *testing.T) {
	secs, _ := strconv.Atoi(os.Getenv("RS_SOAK_SECONDS"))
	if secs <= 0 {
		t.Skip("set RS_SOAK_SECONDS to run the soak test")
	}
	ts := startServerMax(t, 12)
	tok := ts.pair(t, "phone").AccessToken

	heap := func() (uint64, int) {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc, runtime.NumGoroutine()
	}
	cycle := func() {
		id := ts.newSession(t, tok)
		// dialRaw and no t.Cleanup: the helper that registers one per
		// connection would itself keep every connection alive for the whole run.
		c, _, err := ts.dialRaw(id, ts.ticket(t, tok, id))
		if err != nil {
			t.Fatal(err)
		}
		c.SetReadLimit(1 << 20)
		w := &wsClient{t: t, c: c}
		w.resume(0)
		w.sendInput("seq 1 2000; echo OK$((1+1))\n")
		w.waitOutput("OK2")
		_ = c.CloseNow() // vanishes without closing the session
		time.Sleep(20 * time.Millisecond)
		if resp := ts.call(t, "DELETE", "/api/v1/sessions/"+id, tok, "", nil); resp.StatusCode != 200 {
			t.Fatalf("close = %d", resp.StatusCode)
		}
	}

	for i := 0; i < 20; i++ { // warm-up so one-time allocations are not counted
		cycle()
	}
	baseHeap, baseG := heap()
	t.Logf("baseline: heap %.1f MiB, %d goroutines", float64(baseHeap)/(1<<20), baseG)

	end := time.Now().Add(time.Duration(secs) * time.Second)
	next := time.Now().Add(10 * time.Second)
	cycles := 0
	var peakHeap uint64
	for time.Now().Before(end) {
		cycle()
		cycles++
		if time.Now().After(next) {
			h, g := heap()
			if h > peakHeap {
				peakHeap = h
			}
			t.Logf("t+%3ds: %5d cycles, heap %.1f MiB, %d goroutines", int(time.Since(end.Add(-time.Duration(secs)*time.Second)).Seconds()), cycles, float64(h)/(1<<20), g)
			next = next.Add(10 * time.Second)
		}
	}
	h, g := heap()
	t.Logf("final: %d cycles, heap %.1f MiB (baseline %.1f), %d goroutines (baseline %d)", cycles, float64(h)/(1<<20), float64(baseHeap)/(1<<20), g, baseG)
	if h > baseHeap+4<<20 {
		t.Errorf("heap grew from %.1f to %.1f MiB", float64(baseHeap)/(1<<20), float64(h)/(1<<20))
	}
	if g > baseG+5 {
		t.Errorf("goroutines grew from %d to %d", baseG, g)
	}
	if n := len(ts.sessions.List()); n != 0 {
		t.Errorf("%d sessions left over", n)
	}
}
