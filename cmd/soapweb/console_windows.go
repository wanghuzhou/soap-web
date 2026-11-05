//go:build windows

package main

import "syscall"

// setConsoleUTF8 switches the Windows console output code page to UTF-8 (65001)
// so the program's Chinese log lines render correctly.
func setConsoleUTF8() {
	kernel32, err := syscall.LoadDLL("kernel32.dll")
	if err != nil {
		return
	}
	const cpUTF8 = 65001
	if proc, e := kernel32.FindProc("SetConsoleOutputCP"); e == nil {
		_, _, _ = proc.Call(uintptr(cpUTF8))
	}
	if proc, e := kernel32.FindProc("SetConsoleCP"); e == nil {
		_, _, _ = proc.Call(uintptr(cpUTF8))
	}
}
