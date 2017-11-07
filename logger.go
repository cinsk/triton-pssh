package main

import (
	"io/ioutil"
	"log"
	"os"
)

var (
	// ioutil.Discard
	Debug *log.Logger
	Info  *log.Logger
	Warn  *log.Logger
)

func init() {

	if os.Getenv("DEBUG") != "" {
		Debug = log.New(os.Stderr, "DEBUG: ", log.Lshortfile)
	} else {
		Debug = log.New(ioutil.Discard, "DEBUG: ", log.Lshortfile)
	}

	// Info = log.New(os.Stderr, "INFO: ", log.Lshortfile)
	Warn = log.New(os.Stderr, "WARNING: ", log.Lshortfile)

}
