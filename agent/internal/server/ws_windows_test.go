//go:build windows

package server

// `set /a` prints only the result, so it is absent from the echoed input.
const (
	calcInput = "set /a 123456+654321\r\n"
	calcWant  = "777777"
	exitInput = "exit 3\r\n"
)

// calcN returns input printing a result unique to n, and that result.
func calcN(n int) (input, want string) {
	return "set /a " + itoa(100000+n*1111) + "+0\r\n", itoa(100000 + n*1111)
}
