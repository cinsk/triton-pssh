package main

import (
	"fmt"
	"os"
	"path"
)

var ProgramName string

func init() {
	ProgramName = path.Base(os.Args[0])
}

func Err(exitcode int, e error, format string, a ...interface{}) {
	os.Stdout.Sync()

	msg := fmt.Sprintf(format, a...)

	if e != nil {
		msg = fmt.Sprintf("%s: %s: %s\n", ProgramName, msg, e)
	} else {
		msg = fmt.Sprintf("%s: %s\n", ProgramName, msg)
	}
	os.Stderr.WriteString(msg)
	os.Stderr.Sync()

	if exitcode != 0 {
		os.Exit(exitcode)
	}
}
