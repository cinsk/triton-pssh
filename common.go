package main

import (
	"os"
	"os/user"
	"path/filepath"
)

var HomeDirectory string
var TsshRoot string
var TritonProfileName string
var ImageQueryMaxWorkers = 2
var ImageQueryMaxTries = 3

var NetworkQueryMaxWorkers = 2
var NetworkQueryMaxTries = 3

const UNKNOWN_TRITON_PROFILE = "__unknown__"
const TSSH_ROOT = ".tssh"

func init() {
	user, err := user.Current()
	if err != nil {
		Err(1, err, "cannot determine current user")
	}

	HomeDirectory = user.HomeDir
	TsshRoot = filepath.Join(HomeDirectory, TSSH_ROOT)
	TritonProfileName = os.Getenv("TRITON_PROFILE")

	if TritonProfileName == "" {
		Err(1, err, "cannot determine Triton Profile")
	}

}
