//go:build !windows

package main

// setConsoleUTF8 is a no-op on non-Windows platforms.
func setConsoleUTF8() {}
