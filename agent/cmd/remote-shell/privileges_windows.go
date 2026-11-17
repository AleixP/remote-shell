//go:build windows

package main

import "golang.org/x/sys/windows"

// isElevated reports whether the agent runs with an elevated (administrator) token.
func isElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }
