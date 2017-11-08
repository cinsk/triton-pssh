package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	triton "github.com/joyent/triton-go"
	"github.com/joyent/triton-go/authentication"
	"github.com/joyent/triton-go/compute"
	"github.com/joyent/triton-go/network"
	"github.com/logrusorgru/aurora"
	"github.com/pborman/getopt/v2"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

var instanceChannel = make(chan compute.Instance, 1)

const MAX_LIMIT = 1000

func getBastion(client *compute.ComputeClient, context context.Context, name string) (string, string, error) {
	instances, err := client.Instances().List(context, &compute.ListInstancesInput{Name: name})

	if err != nil {
		return "", "", err
	} else if len(instances) == 0 {
		return "", "", nil
	} else {
		// ip, user, error

		f := ImageSession.Query(instances[0].Image)
		img, _ := f.Get()
		user := DefaultUser(img)

		return instances[0].PrimaryIP, user, nil
	}
}

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

	Deadline int // time.Duration
	Timeout  int // time.Duration

	InlineOutput bool
	OutDirectory string
	ErrDirectory string
	Parallelism  int

	DefaultUser string

	AskPassword  bool
	AskOnce      sync.Once
	PasswordAuth ssh.AuthMethod
}

var Config TsshConfig = TsshConfig{
	BastionUser: "root",
	BastionPort: 22,
	BastionName: "bastion",

	ServerPort: 22,

	InlineOutput: false,

	Timeout:  5,
	Deadline: 20,

	Parallelism: 32,
	DefaultUser: "root",
}

func init() {
	Config.KeyId = os.Getenv("SDC_KEY_ID")
	Config.AccountName = os.Getenv("SDC_ACCOUNT")
	Config.KeyPath = os.Getenv("SDC_KEY_FILE")
	Config.TritonURL = os.Getenv("SDC_URL")

	getopt.FlagLong(&Config.KeyId, "keyid", 'k', "Triton account Key ID")
	getopt.FlagLong(&Config.KeyPath, "keyfile", 'K', "Triton Key file path")
	getopt.FlagLong(&Config.TritonURL, "url", 0, "Triton DC endpoint")

	getopt.FlagLong(&Config.User, "user", 'u', "user id of the remote server")
	getopt.FlagLong(&Config.ServerPort, "port", 'P', "server(s) port")

	getopt.FlagLong(&Config.BastionUser, "bastion-user", 'U', "user id of the bastion server")
	getopt.FlagLong(&Config.BastionName, "bastion", 'b', "bastion server address")
	getopt.FlagLong(&Config.BastionPort, "bastion-port", 0, "bastion server port")

	getopt.FlagLong(&Config.Timeout, "timeout", 'T', "connection timeout in seconds")
	getopt.FlagLong(&Config.Deadline, "deadline", 't', "timeout for the session in seconds")

	getopt.FlagLong(&Config.Parallelism, "parallel", 'p', "max number of parallel threads")

	getopt.FlagLong(&Config.InlineOutput, "inline", 'i', "inline standard output for each server")
	getopt.FlagLong(&Config.OutDirectory, "outdir", 'o', "output directory for stdout files")
	getopt.FlagLong(&Config.ErrDirectory, "errdir", 'e', "output directory for stderr files")

	getopt.FlagLong(&Config.DefaultUser, "default-user", 0, "default user if user id of the remote server")

	getopt.FlagLong(&Config.AskPassword, "password", 0, "password for the user")
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
	Config.AskOnce.Do(func() {
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
		Config.PasswordAuth = ssh.Password(string(password))
	})
	return Config.PasswordAuth
}

func AuthMethods() []ssh.AuthMethod {
	return []ssh.AuthMethod{
		AgentAuth(),
		PasswordAuth(),
		PublicKeyAuth("/Users/seong-kookshin/.ssh/id_rsa"),
	}
}

func BuildJob(stdin *os.File, instance *compute.Instance, command string, bastionAddress string, bastionUser string) (*SshJob, error) {

	user := Config.User
	if user == "" {
		imgFuture := ImageSession.Query(instance.Image)
		img, _ := imgFuture.Get()
		user = DefaultUser(img)
	}

	public := NetworkSession.HasPublic(instance)

	job := SshJob{}

	job.ServerConfig = &ssh.ClientConfig{
		User:            user,
		Auth:            AuthMethods(),
		Timeout:         time.Duration(Config.Timeout) * time.Second,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil },
	}
	job.Server = fmt.Sprintf("%s:%d", instance.PrimaryIP, Config.ServerPort)

	if !public {
		job.BastionConfig = &ssh.ClientConfig{
			User:            bastionUser,
			Auth:            []ssh.AuthMethod{AgentAuth()},
			Timeout:         time.Duration(Config.Timeout) * time.Second,
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

var ImageSession *ImageDBSession
var NetworkSession *NetworkDBSession

func main() {
	Debug.Printf("Os.Args: %v\n", os.Args)
	getopt.Parse()
	args := getopt.Args()

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

	ImageSession = NewImageSession(tritonClient.Images(), ImageQueryMaxWorkers)
	if nClient, err := network.NewClient(tritonConfig); err != nil {
		Err(1, err, "cannot create Triton network client")
	} else {
		NetworkSession = NewNetworkSession(nClient, NetworkQueryMaxWorkers)
	}

	expr, cmdline := SplitArgs(args)
	Debug.Printf("Filter Expr: %s\n", expr)
	Debug.Printf("Command: %s\n", cmdline)

	// hasPublicNet, userPublicNet := GetHasPublicNetwork(tritonConfig)
	// hasPublicNet, userPublicNet := GetHasPublicNetwork(tritonConfig)
	// UserFunctions["haspublic"] = userPublicNet
	UserFunctions["ispublic"] = NetworkSession.UserFuncIsPublic

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
		result, error := Evaluate(instance, expr)
		if error != nil {
			Warn.Printf("expr evaluation failed: %s", error)
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
			Warn.Printf("cannot create SSH job: %s", err)
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
				aurora.Sprintf(aurora.Cyan("[%d]").Bold(), count),
				result.Time.Format("15:04:05"),
				aurora.Green("[SUCCESS]").Bold(),
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
				aurora.Sprintf(aurora.Cyan("[%d]").Bold(), count),
				result.Time.Format("15:04:05"),
				aurora.Red("[FAILURE]").Bold(),
				result.InstanceID, result.User, result.InstanceName,
				aurora.Red(errmsg).Bold())
		} else {
			fmt.Fprintf(os.Stderr, "%s %s %s %s %s@%s %s\n",
				aurora.Sprintf(aurora.Cyan("[%d]").Bold(), count),
				result.Time.Format("15:04:05"),
				aurora.Red("[FAILURE]").Bold(),
				result.InstanceID, result.User, result.InstanceName,
				aurora.Sprintf(aurora.Red("%s").Bold(), result.Status))
		}

	}

	SSH.Close()
}
