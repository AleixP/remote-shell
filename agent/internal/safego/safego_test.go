package safego

import (
	"sync"
	"testing"
	"time"
)

func TestPanicIsContained(t *testing.T) {
	if Run("test", func() { panic("boom") }) {
		t.Fatal("Run reported success for a panicking function")
	}
	if !Run("test", func() {}) {
		t.Fatal("Run reported failure for a normal function")
	}
}

func TestGoKeepsTheProcessAlive(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	Go("panics", func() {
		defer wg.Done()
		var m map[string]int
		m["x"] = 1 // nil map write: a runtime panic
	})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine did not finish")
	}
	// Reaching this line means the panic did not take the test process down.
}

func TestDeferredCleanupStillRuns(t *testing.T) {
	cleaned := false
	Run("test", func() {
		defer func() { cleaned = true }()
		panic("boom")
	})
	if !cleaned {
		t.Fatal("deferred cleanup did not run before the panic was contained")
	}
}
