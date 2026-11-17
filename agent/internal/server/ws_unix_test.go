//go:build !windows

package server

// Input that makes the shell print wantOut, which the echoed input does not
// contain (the shell evaluates the arithmetic).
const (
	calcInput = "echo WS$((20+22))\n"
	calcWant  = "WS42"
	exitInput = "exit 3\n"
)
