package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
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
	OPTION_BASTION_PORT
	OPTION_DEFAULT_USER
	OPTION_PASSWORD
	OPTION_NOCACHE
)

var Options = []OptionSpec{
	{OPTION_HELP, "help", NO_ARGUMENT},
	{OPTION_VERSION, "version", NO_ARGUMENT},
	{'k', "keyid", ARGUMENT_REQUIRED},
	// {'K', "keyfile", ARGUMENT_REQUIRED},
	{OPTION_URL, "url", ARGUMENT_REQUIRED},
	{'u', "user", ARGUMENT_REQUIRED},
	{'P', "port", ARGUMENT_REQUIRED},
	{'U', "bastion-user", ARGUMENT_REQUIRED},
	{'b', "bastion", ARGUMENT_REQUIRED},
	{OPTION_BASTION_PORT, "bastion-port", ARGUMENT_REQUIRED},
	{'T', "timeout", ARGUMENT_REQUIRED},
	{'t', "deadline", ARGUMENT_REQUIRED},
	{'p', "parallel", ARGUMENT_REQUIRED},
	{'i', "inline", NO_ARGUMENT},
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
	msg := `triton-pssh usage`
	fmt.Printf(msg)
	os.Exit(0)
}

func VersionAndExit() {
	fmt.Printf("%s version %s", ProgramName, VERSION_STRING)
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
		case "bastion-user":
			Config.BastionUser = opt.Argument
		case "bastion":
			Config.BastionName = opt.Argument
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
	Debug.Printf("SDC_ACCOUNT: %s", config.AccountName)
	Debug.Printf("SDC_KEY_ID: %s", config.KeyId)
	Debug.Printf("SDC_KEY_FILE: %s", config.KeyPath)
	signer, err := GetSigner(config.AccountName, config.KeyId, config.KeyPath)
	if err != nil {
		Err(1, err, "error")
	}

	c := triton.ClientConfig{TritonURL: config.TritonURL, MantaURL: os.Getenv("MANTA_URL"),
		AccountName: config.AccountName,
		Signers:     []authentication.Signer{signer},
	}

	return &c
}

func SplitArgs(args []string) (string, string) {
	if len(args) < 2 {
		Err(1, nil, "wrong number of argument(s)")
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
		Err(1, nil, "empty command")
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

func BuildJob(stdin *os.File, instance *compute.Instance, command string, bastionAddress string, bastionUser string) (*SshJob, error) {

	user := Config.User
	if user == "" {
		img, _ := ImgCache.Get(instance.Image)
		user = DefaultUser(img)
	}

	public := NetCache.HasPublic(instance)

	job := SshJob{}

	job.ServerConfig = &ssh.ClientConfig{
		User:            user,
		Auth:            AuthMethods(),
		Timeout:         Config.Timeout,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil },
	}
	job.Server = fmt.Sprintf("%s:%d", instance.PrimaryIP, Config.ServerPort)

	if !public {
		job.BastionConfig = &ssh.ClientConfig{
			User:            bastionUser,
			Auth:            []ssh.AuthMethod{AgentAuth()},
			Timeout:         Config.Timeout,
			HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil },
		}
		job.Bastion = fmt.Sprintf("%s:%d", bastionAddress, Config.BastionPort)
	}
	job.Command = command
	job.InstanceID = instance.ID
	job.InstanceName = instance.Name

	if stdin != nil {
		in, err := os.Open(stdin.Name())
		if err != nil {
			return nil, fmt.Errorf("cannot open input file %s", stdin.Name())
		}
		job.Input = in
	}

	result := make(chan SshResult)
	job.Result = result

	return &job, nil
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

	Debug.Printf("Config.Keyfiles: %v\n", Config.KeyFiles)
	Debug.Printf("ARGS: %v", args)
	Debug.Printf("AskPassword: %v", Config.AskPassword)

	if Config.TritonURL == "" {
		Err(1, nil, "missing Triton endpoint. SDC_URL undefined")
	}

	tritonConfig := TritonClientConfig(&Config)
	Debug.Printf("TritonConfig: %v", tritonConfig)

	tritonClient, err := compute.NewClient(tritonConfig)
	if err != nil {
		Err(1, err, "cannot create Triton compute client")
	}

	ImgCache = NewImageCache(tritonClient.Images())
	if nClient, err := network.NewClient(tritonConfig); err != nil {
		Err(1, err, "cannot create Triton network client")
	} else {
		NetCache = NewNetworkCache(nClient)
	}

	expr, cmdline := SplitArgs(args)
	Debug.Printf("Filter Expr: %s\n", expr)
	Debug.Printf("Command: %s\n", cmdline)

	// hasPublicNet, userPublicNet := GetHasPublicNetwork(tritonConfig)
	// hasPublicNet, userPublicNet := GetHasPublicNetwork(tritonConfig)
	// UserFunctions["haspublic"] = userPublicNet
	UserFunctions["ispublic"] = NetCache.UserFuncIsPublic

	color := aurora.NewAurora(terminal.IsTerminal(int(syscall.Stderr)))

	SSH := NewSshSession(Config.Parallelism)

	instanceChan := ListInstances(tritonClient, context.Background())

	bastionAddress, bastionUser, _ := getBastion(tritonClient, context.Background(), Config.BastionName)
	// useBastion := false
	Debug.Printf("Bastion: ADDRESS=%s, USER=%s", bastionAddress, bastionUser)

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

		job, err := BuildJob(inputFile, instance, cmdline, bastionAddress, bastionUser)
		if err != nil {
			Warn.Printf("warning: cannot create SSH job: %s", err)
			continue
		}

		jobWg.Add(1)
		SSH.input <- job

		go func(input chan SshResult) {
			defer jobWg.Done()
			result := <-input
			resultChannel <- result
		}(job.Result)

		/*
			if NetworkSession.HasPublic(instance) {
				fmt.Printf("%s@%s\n", user, instance.PrimaryIP)

			} else {
				fmt.Printf("%s@%s through %s\n", user, instance.PrimaryIP, bastionIp)
				// useBastion = true
			}
		*/
	}

	go func() {
		defer close(resultChannel)
		jobWg.Wait()
	}()

	count := 0
	for result := range resultChannel {
		count++

		if result.Status == nil {
			fmt.Fprintf(os.Stderr, "%s %s %s %s %s@%s\n",
				color.Sprintf(color.Cyan("[%d]").Bold(), count),
				result.Time.Format("15:04:05"),
				color.Green("[SUCCESS]").Bold(),
				result.InstanceID, result.User, result.InstanceName)

			if Config.InlineOutput && result.Stdout != nil {
				io.Copy(os.Stdout, result.Stdout)
				os.Stdout.Sync()
			}
		} else if ee, ok := result.Status.(*ssh.ExitError); ok {
			var errmsg string
			if ee.Signal() == "" {
				errmsg = fmt.Sprintf("%s, returning %d", ee.Error(), ee.ExitStatus())
			} else {
				errmsg = fmt.Sprintf("%s, returning %d, signaled with %s", ee.Error(), ee.ExitStatus(), ee.Signal())
			}
			fmt.Fprintf(os.Stderr, "%s %s %s %s %s@%s %s\n",
				color.Sprintf(color.Cyan("[%d]").Bold(), count),
				result.Time.Format("15:04:05"),
				color.Red("[FAILURE]").Bold(),
				result.InstanceID, result.User, result.InstanceName,
				color.Red(errmsg).Bold())
		} else {
			fmt.Fprintf(os.Stderr, "%s %s %s %s %s@%s %s\n",
				color.Sprintf(color.Cyan("[%d]").Bold(), count),
				result.Time.Format("15:04:05"),
				color.Red("[FAILURE]").Bold(),
				result.InstanceID, result.User, result.InstanceName,
				color.Sprintf(color.Red("%s").Bold(), result.Status))
		}

	}

	SSH.Close()
}
