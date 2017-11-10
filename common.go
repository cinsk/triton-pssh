package main

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type TsshConfig struct {
	KeyId       string
	KeyPath     string
	AccountName string

	TritonURL string

	User       string
	ServerPort int

	BastionUser string
	BastionName string // Triton instance name
	BastionPort int

	Deadline time.Duration // time.Duration
	Timeout  time.Duration // time.Duration

	InlineOutput bool
	OutDirectory string
	ErrDirectory string
	Parallelism  int

	DefaultUser string

	AskPassword  bool
	askOnce      sync.Once
	passwordAuth ssh.AuthMethod

	KeyFiles []string

	NoCache bool
}

var Config TsshConfig = TsshConfig{
	BastionUser: "root",
	BastionPort: 22,
	BastionName: "bastion",

	ServerPort: 22,

	InlineOutput: false,

	Timeout:  time.Duration(10) * time.Second,
	Deadline: time.Duration(20) * time.Second,

	Parallelism: 32,
	DefaultUser: "root",

	KeyFiles: make([]string, 0),
}

var HomeDirectory string
var TsshRoot string
var TritonProfileName string
var ImageQueryMaxWorkers = 2
var ImageQueryMaxTries = 3

var NetworkQueryMaxWorkers = 2
var NetworkQueryMaxTries = 3

const VERSION_STRING = "0.1"
const UNKNOWN_TRITON_PROFILE = "__unknown__"
const TSSH_ROOT = ".triton-pssh"
const (
	S_IRUSR = 0000400
	S_IWUSR = 0000200
	S_IXUSR = 0000100

	S_IRGRP = 0000040
	S_IWGRP = 0000020
	S_IXGRP = 0000010

	S_IROTH = 0000004
	S_IWOTH = 0000002
	S_IXOTH = 0000001
)

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

func ReverseStrings(ss []string) {
	last := len(ss) - 1
	for i := 0; i < len(ss)/2; i++ {
		ss[i], ss[last-i] = ss[last-i], ss[i]
	}
}

func CheckOutputDirectory(dir string, createDirectory bool) error {
	stat, err := os.Stat(dir)

	if err == nil {
		if !stat.IsDir() {
			return fmt.Errorf("%s is not a directory", dir)
		}
		return nil
	} else {
		if os.IsNotExist(err) {
			if createDirectory {
				if e := os.MkdirAll(dir, 0755); e != nil {
					return fmt.Errorf("cannot create a directory(%s): %s", dir, e)
				}
				return nil
			} else {
				return err
			}
		} else {
			return fmt.Errorf("os.Stat(%s) failed: %s", dir, err)
		}
	}
}
