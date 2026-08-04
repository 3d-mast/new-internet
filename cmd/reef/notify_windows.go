//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var messageBoxW = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

func showFatalError(message string) {
	showMessage("Reef 4 — ошибка запуска", message, 0x00000010)
}

func showWarning(message string) {
	showMessage("Reef 4", message, 0x00000030)
}

func showMessage(title, message string, flags uintptr) {
	titleUTF16, _ := syscall.UTF16PtrFromString(title)
	messageUTF16, _ := syscall.UTF16PtrFromString(message)
	_, _, _ = messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messageUTF16)),
		uintptr(unsafe.Pointer(titleUTF16)),
		flags,
	)
}
