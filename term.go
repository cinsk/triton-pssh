package main

import (
	"os"

	l "github.com/cinsk/triton-pssh/log"
	"golang.org/x/crypto/ssh/terminal"
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
	w, h, err := terminal.GetSize(1)
	if err != nil {
		l.Debug("cannot determine the terminal size, use default (%vx%v): %s", COL, LINES, err)
		return COL, LINES
	} else {
		return w, h
	}
}

func IsPipe(file *os.File) bool {
	fi, err := file.Stat()
	if err != nil {
		l.ErrQuit(1, "cannot Stat() %s: %s", file.Name(), err)
	}

	if fi.Mode()&os.ModeCharDevice == 0 {
		return true
	}
	return false
}
