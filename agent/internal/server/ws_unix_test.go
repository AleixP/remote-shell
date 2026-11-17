//go:build !windows

package server

// Input that makes the shell print wantOut, which the echoed input does not
// contain (the shell evaluates the arithmetic).
const (
	calcInput       = "echo WS$((20+22))\n"
	calcWant        = "WS42"
	calcInputMarker = "echo WS"
	exitInput       = "exit 3\n"

	// testShellField picks plain sh: it starts instantly, unlike a user's
	// interactive zsh or bash with a heavy profile.
	testShellField = `,"shell":"sh"`
)

// calcN returns input printing a result unique to n, and that result.
func calcN(n int) (input, want string) {
	return "echo Q$((1000+" + itoa(n) + "))\n", "Q" + itoa(1000+n)
}
