//go:build windows

package server

// The default Windows shell is PowerShell. The evaluated result is absent from
// the echoed input.
const (
	calcInput       = "Write-Output (123456+654321)\r\n"
	calcWant        = "777777"
	calcInputMarker = "Write-Output (123456"
	exitInput       = "exit 3\r\n"

	testShellField = ""
)

// calcN returns input printing a result unique to n, and that result.
func calcN(n int) (input, want string) {
	return "Write-Output (100000+" + itoa(1111*(n+1)) + ")\r\n", itoa(100000 + 1111*(n+1))
}
