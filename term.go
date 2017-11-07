package main

import (
	"syscall"
	"unsafe"
)

type winsize struct {
	Row    uint16
	Col    uint16
	XPixel uint16
	YPixel uint16
}

const LINES = 24
const COL = 80

func TerminalSize() (int, int) {
	ws := winsize{}

	ret, _, _ := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdin), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if int(ret) == -1 {
		return COL, LINES
	} else {
		return int(ws.Col), int(ws.Row)
	}
}
