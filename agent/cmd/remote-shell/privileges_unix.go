//go:build !windows

package main

import "os"

// isElevated reports whether the agent runs as root.
func isElevated() bool { return os.Geteuid() == 0 }
