package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	triton "github.com/joyent/triton-go"
	"github.com/joyent/triton-go/authentication"
	"github.com/joyent/triton-go/compute"
	"github.com/joyent/triton-go/network"
	"github.com/logrusorgru/aurora"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

var instanceChannel = make(chan compute.Instance, 1)

func getBastion(client *compute.ComputeClient, context context.Context, name string) (string, string, error) {
	instances, err := client.Instances().List(context, &compute.ListInstancesInput{Name: name})

	if err != nil {
		return "", "", err
	} else if len(instances) == 0 {
		return "", "", nil
	} else {
		// ip, user, error

		img, _ := ImgCache.Get(instances[0].Image)
		user := DefaultUser(img)

		return instances[0].PrimaryIP, user, nil
	}
}

const (
	OPTION_HELP = iota
	OPTION_VERSION
	OPTION_URL
	OPTION_INLINE_STDOUT
	OPTION_DEFAULT_USER
	OPTION_PASSWORD
	OPTION_NOCACHE
)

var Options = []OptionSpec{
	{OPTION_HELP, "help", NO_ARGUMENT},
	{OPTION_VERSION, "version", NO_ARGUMENT},
	{'k', "keyid", ARGUMENT_REQUIRED},
	{'K', "keyfile", ARGUMENT_REQUIRED},
	{OPTION_URL, "url", ARGUMENT_REQUIRED},
	{'u', "user", ARGUMENT_REQUIRED},
	{'P', "port", ARGUMENT_REQUIRED},

	{'b', "bastion", ARGUMENT_REQUIRED},

	{'T', "timeout", ARGUMENT_REQUIRED},
	{'t', "deadline", ARGUMENT_REQUIRED},
	{'p', "parallel", ARGUMENT_REQUIRED},
	{'i', "inline", NO_ARGUMENT},
	{OPTION_INLINE_STDOUT, "inline-stdout", NO_ARGUMENT},
	{'o', "outdir", ARGUMENT_REQUIRED},
	{'e', "errdir", ARGUMENT_REQUIRED},
	{OPTION_DEFAULT_USER, "default-user", ARGUMENT_REQUIRED},
	{OPTION_PASSWORD, "password", NO_ARGUMENT},
	{'I', "identity", ARGUMENT_REQUIRED},
	{OPTION_NOCACHE, "no-cache", NO_ARGUMENT},
}

func init() {
	Config.KeyId = os.Getenv("SDC_KEY_ID")
	Config.AccountName = os.Getenv("SDC_ACCOUNT")
	Config.KeyPath = os.Getenv("SDC_KEY_FILE")
	Config.TritonURL = os.Getenv("SDC_URL")
}

func HelpAndExit() {
	msg := `Parallel SSH program for Joyent Triton instances
Usage: triton-pssh [OPTION] FILTER-EXPRESSION... ::: COMMAND...

Option:

  -i, --inline             inline standard output and standard error for each server
      --inline-stdout      inline standard output only

  -o, --outdir=DIR         output directory for stdout files
  -e, --errdir=DIR         output directory for stderr files

      --no-cache           read all information directly from Triton Cloud API

  -p, --password           Ask for a password

      --default-user=USER  Use USER if the default user cannot be determined

  -k, --keyid=ID           the fingerprint of the SSH key for Triton Cloud API
                             access, this will override the value of SDC_KEY_ID.
  -K, --keyfile=KEYFILE    the private key to access Triton Cloud API, the will
                             override the value of SDC_KEY_FILE.
      --url=URL            the base endpoint for the Triton Cloud API, this
                             will override the value of SDC_URL.

  -u, --user=USER          the username of the remote hosts
  -P, --port=PORT          the SSH port of the remote hosts

  -b, --bastion=ENDPOINT   the endpoint([user@]name[:port]) of bastion server,
                             name must be a Triton instance name

  -T, --timeout=TIMEOUT    the connection timeout of the SSH session
  -t, --deadline=TIMEOUT   the timeout of the SSH session

  -p, --parallel=MAXPROC   the max number of SSH connection at a time
  -I, --identity=KEYFILE

      --help               display this help and exit
      --version            output version information and exit

`
	fmt.Printf(msg)
	os.Exit(0)
}

func VersionAndExit() {
	fmt.Printf("%s version %s\n", ProgramName, VERSION_STRING)
	os.Exit(0)
}

func ParseOptions(args []string) []string {
	context := GetoptContext{Options: Options, Args: args}
	for {
		opt, err := context.Getopt()

		if err != nil {
			Err(1, err, "Getopt() failed")
		}
		if opt == nil {
			break
		}
		switch opt.LongOption {
		case "help":
			HelpAndExit()
		case "version":
			VersionAndExit()
		case "keyid":
			Config.KeyId = opt.Argument
		case "keyfile":
			Config.KeyPath = opt.Argument

		case "identity":
			Config.KeyFiles = append(Config.KeyFiles, opt.Argument)
		case "user":
			Config.User = opt.Argument
		case "port":
			i, err := strconv.Atoi(opt.Argument)
			if err != nil {
				Config.ServerPort = i
			} else {
				Err(1, err, "cannot convert %s to numeric value", opt.Argument)
			}
		case "bastion":
			user, host, port, err := ParseUserHostPort(opt.Argument)
			if err != nil {
				Err(1, err, "cannot parse bastion endpoint")
			}
			Config.BastionName = host
			Config.BastionPort = port
			Config.BastionUser = user

		case "timeout":
			f, err := strconv.ParseFloat(opt.Argument, 0)
			if err != nil {
				Err(1, err, "cannot convert %s to numeric value", opt.Argument)
			}
			Config.Timeout = time.Duration(f * float64(time.Second))
			Debug.Printf("TIMEOUT: %v\n", Config.Timeout)

		case "deadline":
			f, err := strconv.ParseFloat(opt.Argument, 0)
			if err != nil {
				Err(1, err, "cannot convert %s to numberic value", opt.Argument)
			}
			Config.Deadline = time.Duration(f * float64(time.Second))
		case "parallel":
			i, err := strconv.Atoi(opt.Argument)
			if err == nil {
				if i <= 0 {
					i = 1
				}
				Config.Parallelism = i
			} else {
				Err(1, err, "cannot convert %s to numeric value", opt.Argument)
			}
		case "inline":
			Config.InlineOutput = true
		case "inline-stdout":
			Config.InlineOutput = true
			Config.InlineStdoutOnly = true
		case "outdir":
			if err := CheckOutputDirectory(opt.Argument, true); err != nil {
				Err(1, err, "invalid argument")
			}
			Config.OutDirectory = opt.Argument
		case "errdir":
			if err := CheckOutputDirectory(opt.Argument, true); err != nil {
				Err(1, err, "invalid argument")
			}
			Config.ErrDirectory = opt.Argument
		case "default-user":
			Config.DefaultUser = opt.Argument
		case "password":
			Config.AskPassword = true
		case "no-cache":
			Config.NoCache = true
		default:
			Err(1, err, "unrecognized option -- %s", opt.LongOption)
		}
	}

	Config.KeyFiles = append(Config.KeyFiles, filepath.Join(HomeDirectory, ".ssh", "id_rsa"))
	Config.KeyFiles = append(Config.KeyFiles, filepath.Join(HomeDirectory, ".ssh", "id_dsa"))
	Config.KeyFiles = append(Config.KeyFiles, filepath.Join(HomeDirectory, ".ssh", "id_ecdsa"))
	Config.KeyFiles = append(Config.KeyFiles, filepath.Join(HomeDirectory, ".ssh", "id_ed25519"))

	if Config.InlineOutput && (Config.OutDirectory != "" || Config.ErrDirectory != "") {
		Err(1, nil, "inline output(-i,--inline) cannot be used with (-o,--outdir,-e,--errdir)")
	}

	return context.Arguments()
}

func TritonClientConfig(config *TsshConfig) *triton.ClientConfig {
	// Debug.Printf("SDC_ACCOUNT: %s", config.AccountName)
	// Debug.Printf("SDC_KEY_ID: %s", config.KeyId)
	// Debug.Printf("SDC_KEY_FILE: %s", config.KeyPath)

	signers := []authentication.Signer{}

	signers, err := GetSigners(config.AccountName, config.KeyId, config.KeyPath)
	if err != nil {
		Err(1, err, "cannot get a signer for Triton Cloud API")
	}

	c := triton.ClientConfig{TritonURL: config.TritonURL, MantaURL: os.Getenv("MANTA_URL"),
		AccountName: config.AccountName,
		Signers:     signers,
	}

	return &c
}

func SplitArgs(args []string) (string, string) {
	if len(args) < 2 {
		Err(0, nil, "wrong number of argument(s)")
		Err(1, nil, "Try with --help for more")
	}

	var patbuf bytes.Buffer
	var cmdbuf bytes.Buffer

	var i int
	for i = 0; i < len(args); i++ {
		if args[i] == ":::" {
			i++
			break
		}
		patbuf.WriteString(args[i])
		patbuf.WriteString(" ")
	}
	for ; i < len(args); i++ {
		cmdbuf.WriteString(args[i])
		cmdbuf.WriteString(" ")
	}

	p := strings.Trim(patbuf.String(), " \t\v\n\r")
	c := strings.Trim(cmdbuf.String(), " \t\v\n\r")

	if p == "" {
		p = "true"
	}

	if c == "" {
		Err(0, nil, "no command specified")
		Err(1, nil, "you might miss to use ::: delimiter")
	}

	return p, c
}

func StdinFile() (*os.File, error) {
	if terminal.IsTerminal(int(syscall.Stdin)) {
		return nil, nil
	}

	input, err := ioutil.TempFile("", "triton-pssh-input")
	if err != nil {
		return nil, fmt.Errorf("cannot create tmp file: %s\n", err)
	}

	nwritten, err := io.Copy(input, os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("cannot copy STDIN to tmp file(%s): %s\n", input.Name(), err)
	}

	Debug.Printf("read %d bytes from STDIN stored to %s\n", nwritten, input.Name())
	return input, nil
}

func PasswordAuth() ssh.AuthMethod {
	Config.askOnce.Do(func() {
		if !Config.AskPassword {
			return
		}

		if !terminal.IsTerminal(int(syscall.Stdin)) {
			Err(1, nil, "stdin is not a terminal, required by password authentication")
		}

		fmt.Fprintf(os.Stderr, "password: ")
		password, err := terminal.ReadPassword(int(syscall.Stdin))
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			Err(1, err, "cannot read password")
		}
		Config.passwordAuth = ssh.Password(string(password))
	})
	return Config.passwordAuth
}

func AuthMethods() []ssh.AuthMethod {
	methods := make([]ssh.AuthMethod, 0)

	if auth := AgentAuth(); auth != nil {
		methods = append(methods, auth)
	}
	if auth := PasswordAuth(); auth != nil {
		methods = append(methods, auth)
	}

	for _, kfile := range Config.KeyFiles {
		auth, err := PublicKeyAuth(kfile)

		if err != nil {
			Warn.Printf("warning: %s", kfile, err)
		}

		// auth can be nil, which should be ignored.
		if auth != nil {
			methods = append(methods, auth)
		}
	}

	return methods
}

func IsDockerContainer(instance *compute.Instance) bool {
	val, ok := instance.Tags["sdc_docker"]
	if !ok {
		return false
	}

	switch v := val.(type) {
	case string:
		b, err := strconv.ParseBool(v)
		if err == nil && b {
			return true
		}
		return false
	case bool:
		return v
	default:
		return false
	}
}

var ImgCache *ImageCache
var NetCache *NetworkCache

func main() {
	Debug.Printf("Os.Args: %v\n", os.Args)
	args := ParseOptions(os.Args[1:])
	Debug.Printf("Config: %v", Config)

	expr, cmdline := SplitArgs(args)
	Debug.Printf("Filter Expr: %s\n", expr)
	Debug.Printf("Command: %s\n", cmdline)

	if Config.TritonURL == "" {
		Err(1, nil, "missing Triton endpoint. SDC_URL undefined")
	}

	tritonConfig := TritonClientConfig(&Config)

	tritonClient, err := compute.NewClient(tritonConfig)
	if err != nil {
		Err(1, err, "cannot create Triton compute client")
	}

	ImgCache = NewImageCache(tritonClient.Images(), Config.ImageCacheExpiration)
	if nClient, err := network.NewClient(tritonConfig); err != nil {
		Err(1, err, "cannot create Triton network client")
	} else {
		NetCache = NewNetworkCache(nClient, Config.NetworkCacheExpiration)
	}

	if Config.BastionName != "" {
		addr, user, err := getBastion(tritonClient, context.Background(), Config.BastionName)
		if err != nil {
			Err(1, err, "cannot determine bastion server")
		}
		Config.BastionAddress = addr

		if Config.BastionUser == "" {
			Config.BastionUser = user
		}
	}

	// hasPublicNet, userPublicNet := GetHasPublicNetwork(tritonConfig)
	// hasPublicNet, userPublicNet := GetHasPublicNetwork(tritonConfig)
	// UserFunctions["haspublic"] = userPublicNet
	UserFunctions["ispublic"] = NetCache.UserFuncIsPublic

	color := aurora.NewAurora(terminal.IsTerminal(int(syscall.Stderr)))

	SSH := NewSshSession(&Config, Config.Parallelism)

	instanceChan := ListInstances(tritonClient, context.Background(), Config.InstanceCacheExpiration)

	inputFile, err := StdinFile()
	if inputFile != nil {
		defer os.Remove(inputFile.Name())
		defer inputFile.Close()
	}

	jobWg := sync.WaitGroup{}
	resultChannel := make(chan SshResult)

	for instance := range instanceChan {
		if IsDockerContainer(instance) {
			continue
		}

		result, error := Evaluate(instance, expr)
		if error != nil {
			Warn.Printf("warning: expr evaluation failed: %s", error)
			continue
		}
		if r := bool(result); !r {
			Debug.Printf("INSTANCE[%v]: skipped \n", instance.Name)
			continue
		}

		// fmt.Printf("INSTANCE[%v]: hasPublicNet(%v)\n", instance.Name, hasPublicNet(instance))
		// fmt.Printf("# %s [%v]:\n", instance.ID, instance.Name)

		job, err := SSH.BuildJob(instance, cmdline, inputFile)
		if err != nil {
			Warn.Printf("warning: cannot create SSH job: %s", err)
			continue
		}

		jobWg.Add(1)
		SSH.Run(job)

		go func(input chan SshResult) {
			defer jobWg.Done()
			result := <-input
			resultChannel <- result
		}(job.Result)
	}

	go func() {
		defer close(resultChannel)
		jobWg.Wait()
	}()

	count := 0
	for result := range resultChannel {
		count++

		header := BuildResultHeader(count, &result, color)
		fmt.Fprintf(os.Stderr, "%s\n", header)
		Debug.Printf("Status: [%T] %v", result.Status, result.Status)

		if Config.InlineOutput && result.Stdout != nil {
			io.Copy(os.Stdout, result.Stdout)
			os.Stdout.Sync()
		}
	}

	SSH.Close()
}

func BuildResultHeader(index int, result *SshResult, color aurora.Aurora) string {
	var header string
	if result.Status == nil {
		header = fmt.Sprintf("%s %s %s %s %s@%s",
			color.Sprintf(color.Cyan("[%d]").Bold(), index),
			result.Time.Format("15:04:05"),
			color.Green("[SUCCESS]").Bold(),
			result.InstanceID, result.User, result.InstanceName)
	} else if ee, ok := result.Status.(*ssh.ExitError); ok {
		var errmsg string
		if ee.Signal() == "" {
			errmsg = fmt.Sprintf("%s, returning %d", ee.Error(), ee.ExitStatus())
		} else {
			errmsg = fmt.Sprintf("%s, returning %d, signaled with %s", ee.Error(), ee.ExitStatus(), ee.Signal())
		}
		header = fmt.Sprintf("%s %s %s %s %s@%s %s",
			color.Sprintf(color.Cyan("[%d]").Bold(), index),
			result.Time.Format("15:04:05"),
			color.Red("[FAILURE]").Bold(),
			result.InstanceID, result.User, result.InstanceName,
			color.Red(errmsg).Bold())
	} else {
		header = fmt.Sprintf("%s %s %s %s %s@%s [%T] %s",
			color.Sprintf(color.Cyan("[%d]").Bold(), index),
			result.Time.Format("15:04:05"),
			color.Red("[FAILURE]").Bold(),
			result.InstanceID, result.User, result.InstanceName,
			result.Status,
			color.Sprintf(color.Red("%s").Bold(), result.Status))
	}
	return header
}
