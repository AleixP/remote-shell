// Package safego runs goroutines so that a panic in one of them is logged and
// contained instead of terminating the whole agent and every open terminal.
package safego

import (
	"fmt"
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a new goroutine. A panic is recovered and logged with the
// goroutine's name and stack; it never propagates to the process.
func Go(name string, fn func()) {
	go Run(name, fn)
}

// Run calls fn and recovers from a panic, logging it. It reports whether fn
// returned normally.
func Run(name string, fn func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("goroutine panicked and was contained",
				"goroutine", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			ok = false
		}
	}()
	fn()
	return true
}
