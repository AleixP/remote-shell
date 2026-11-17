//go:build windows

package server

// `set /a` prints only the result, so it is absent from the echoed input.
const (
	calcInput = "set /a 123456+654321\r\n"
	calcWant  = "777777"
	exitInput = "exit 3\r\n"
)
