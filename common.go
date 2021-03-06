package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"
	"time"

	l "github.com/cinsk/triton-pssh/log"
	"golang.org/x/crypto/ssh"
)

type TsshConfig struct {
	KeyId       string
	KeyPath     string
	AccountName string

	TritonURL string

	User       string
	ServerPort int

	ServerNames []string // each element has the form 'name == "machine name"'

	BastionUser    string
	BastionName    string // Triton instance name
	BastionPort    int
	BastionAddress string

	ForceBastionOnPublicHost bool

	Deadline time.Duration // time.Duration
	Timeout  time.Duration // time.Duration

	InlineOutput     bool
	InlineStdoutOnly bool

	OutDirectory string
	ErrDirectory string
	Parallelism  int

	DefaultUser string

	AskPassword  bool
	askOnce      sync.Once
	passwordAuth ssh.AuthMethod

	Auth AuthMethods

	NoCache                 bool
	NetworkCacheExpiration  time.Duration
	ImageCacheExpiration    time.Duration
	InstanceCacheExpiration time.Duration

	PrintMode PrintConfMode

	DryRun bool

	InstanceLimits uint64
}

var Config TsshConfig = TsshConfig{
	BastionUser: "root",
	BastionPort: 22,

	ServerPort: 22,

	InlineOutput: false,

	Timeout:  time.Duration(10) * time.Second,
	Deadline: time.Duration(20) * time.Second,

	Parallelism: runtime.NumCPU(),
	DefaultUser: "root",

	NetworkCacheExpiration:  time.Duration(24*7) * time.Hour,
	ImageCacheExpiration:    time.Duration(24*7) * time.Hour,
	InstanceCacheExpiration: time.Duration(24) * time.Hour,

	InstanceLimits: math.MaxUint64,
}

var HomeDirectory string
var TsshRoot string
var TritonProfileName string
var ImageQueryMaxWorkers = 4
var ImageQueryMaxTries = 2

var NetworkQueryMaxWorkers = 4
var NetworkQueryMaxTries = 2

var ImgCache *ImageCache
var NetCache *NetworkCache

const VERSION_STRING = "1.0.5"
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
	HomeDirectory = UserHomeDirectory()
	TsshRoot = filepath.Join(HomeDirectory, TSSH_ROOT)
	TritonProfileName = os.Getenv("TRITON_PROFILE")
	Config.Auth.AddDefaults()
}

func UserHomeDirectory() string {
	home := os.Getenv("HOME")
	if home == "" {
		usr, err := user.Current()
		if err != nil {
			l.ErrQuit(1, "cannot determine home directory: %v", err)
		}
		return usr.HomeDir
	}
	return home
}

func ExpandPath(s string) string {
	if len(s) >= 2 && s[0:2] == "~/" {
		return filepath.Join(HomeDirectory, s[2:])
	}
	return s
}

func IsExist(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return os.IsExist(err)
}

func (config TsshConfig) String() string {
	buf := bytes.Buffer{}

	buf.WriteString(fmt.Sprintf("KeyId=%s, ", config.KeyId))
	buf.WriteString(fmt.Sprintf("KeyPath=%s, ", config.KeyPath))
	buf.WriteString(fmt.Sprintf("AccountName=%s, ", config.AccountName))
	buf.WriteString(fmt.Sprintf("TritonURL=%s, ", config.TritonURL))
	buf.WriteString(fmt.Sprintf("User=%s, ", config.User))
	buf.WriteString(fmt.Sprintf("ServerPort=%d, ", config.ServerPort))
	buf.WriteString(fmt.Sprintf("BastionUser=%s, ", config.BastionUser))
	buf.WriteString(fmt.Sprintf("BastionName=%s, ", config.BastionName))
	buf.WriteString(fmt.Sprintf("BastionPort=%d, ", config.BastionPort))
	buf.WriteString(fmt.Sprintf("Deadline=%v, ", config.Deadline))
	buf.WriteString(fmt.Sprintf("Timeout=%s, ", config.Timeout))
	buf.WriteString(fmt.Sprintf("InlineOutput=%v, ", config.InlineOutput))
	buf.WriteString(fmt.Sprintf("InlineStdoutOnly=%v, ", config.InlineStdoutOnly))
	buf.WriteString(fmt.Sprintf("OutDirectory=%s, ", config.OutDirectory))
	buf.WriteString(fmt.Sprintf("ErrDirectory=%s, ", config.ErrDirectory))
	buf.WriteString(fmt.Sprintf("Parallelism=%d, ", config.Parallelism))
	buf.WriteString(fmt.Sprintf("DefaultUser=%s, ", config.DefaultUser))
	buf.WriteString(fmt.Sprintf("AskPassword=%v, ", config.DefaultUser))
	buf.WriteString(fmt.Sprintf("NoCache=%v, ", config.NoCache))
	buf.WriteString(fmt.Sprintf("Auth=%v", config.Auth))

	return buf.String()
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

var ParseUserHostPort = func() func(string) (string, string, int, error) {
	re := regexp.MustCompile("(([^@]+)@)?([^:]+)(:([0-9]+))?")
	//                          ^^^^^2    ^^^^^3   ^^^^^^^5

	return func(s string) (string, string, int, error) {
		// s == "user@name"
		match := re.FindStringSubmatch(s)

		if len(match) != 6 {
			return "", "", 0, fmt.Errorf("cannot retrive user, host and port from %s", s)
		}

		user := ""
		if match[2] != "" {
			user = match[2]
		}
		host := match[3]
		port := 22
		if match[5] != "" {
			p, err := strconv.Atoi(match[5])
			if err != nil {
				return "", "", 0, err
			}
			if p <= 0 {
				return "", "", 0, fmt.Errorf("port number must be greater than zero")
			}

			port = p
		}

		return user, host, port, nil
	}
}()
