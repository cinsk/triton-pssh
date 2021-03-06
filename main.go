package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	l "github.com/cinsk/triton-pssh/log"
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
	{'B', "force-bastion", NO_ARGUMENT},

	{'T', "timeout", ARGUMENT_REQUIRED},
	{'t', "deadline", ARGUMENT_REQUIRED},
	{'p', "parallel", ARGUMENT_REQUIRED},
	{'i', "inline", NO_ARGUMENT},
	{'h', "host", ARGUMENT_REQUIRED},
	{OPTION_INLINE_STDOUT, "inline-stdout", NO_ARGUMENT},
	{'o', "outdir", ARGUMENT_REQUIRED},
	{'e', "errdir", ARGUMENT_REQUIRED},
	{OPTION_DEFAULT_USER, "default-user", ARGUMENT_REQUIRED},
	{OPTION_PASSWORD, "password", NO_ARGUMENT},
	{'A', "agent", NO_ARGUMENT},
	{'I', "identity", ARGUMENT_REQUIRED},
	{'d', "dryrun", NO_ARGUMENT},
	{OPTION_NOCACHE, "no-cache", NO_ARGUMENT},
	{'1', "ssh", NO_ARGUMENT},
	{'2', "scp", NO_ARGUMENT},
	{'3', "rsync", NO_ARGUMENT},

	{'n', "limit", ARGUMENT_REQUIRED},
}

var ProgramName = path.Base(os.Args[0])

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

      --default-user=USER  Use USER if the default user cannot be determined

  -k, --keyid=ID           the fingerprint of the SSH key for Triton Cloud API
                             access, this will override the value of SDC_KEY_ID.
  -K, --keyfile=KEYFILE    the private key to access Triton Cloud API, the will
                             override the value of SDC_KEY_FILE.
      --url=URL            the base endpoint for the Triton Cloud API, this
                             will override the value of SDC_URL.

  -I, --identity=KEYFILE   select a private key for public key authentication
                             for SSH session
  -A, --agent              use SSH agent for the authentication for SSH session
      --password           use password authentication for SSH session

  -u, --user=USER          the username of the remote hosts
  -P, --port=PORT          the SSH port of the remote hosts

  -b, --bastion=ENDPOINT   the endpoint([user@]NAME[:port]) of bastion server,
                             NAME must be a Triton instance name
  -B, --force-bastion      use bastion even for an instance with public IP.

  -T, --timeout=TIMEOUT    the connection timeout of the SSH session
  -t, --deadline=TIMEOUT   the timeout of the SSH session

  -p, --parallel=MAXPROC   the max number of SSH connection at a time
  -n, --limit=LIMIT        Use only LIMIT instances at most

      --help               display this help and exit
      --version            output version information and exit

See https://github.com/cinsk/triton-pssh for FILTER-EXPRESSION and examples.

`
	fmt.Printf(msg)
	os.Exit(0)
}

func VersionAndExit() {
	msg := `
triton-pssh and other scripts come without warranty of any kind.
triton-pssh and other scripts are covered by MPL 2.0 License.`

	fmt.Printf("%s version %s", ProgramName, VERSION_STRING)
	fmt.Println(msg)
	os.Exit(0)
}

func ParseOptions(args []string) []string {
	context := GetoptContext{Options: Options, Args: args}
	for {
		opt, err := context.Getopt()

		if err != nil {
			l.ErrQuit(1, "Getopt() failed: %v", err)
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
			Config.KeyPath = ExpandPath(opt.Argument)
			if !IsExist(Config.KeyPath) {
				l.ErrQuit(1, "keyfile \"%s\" not found", Config.KeyPath)
			}

		case "identity":
			if err := Config.Auth.AddPrivateKey(ExpandPath(opt.Argument)); err != nil {
				l.ErrQuit(1, "cannot add publicKey authentication: %v", err)
			}
		case "agent":
			if err := Config.Auth.AddAgent(); err != nil {
				l.ErrQuit(1, "cannot add SSH agent authentication: %v", err)
			}
		case "password":
			if err := Config.Auth.AddPassword(); err != nil {
				l.ErrQuit(1, "cannot add password authentication: %v", err)
			}
		case "user":
			Config.User = opt.Argument
		case "port":
			i, err := strconv.Atoi(opt.Argument)
			if err == nil {
				Config.ServerPort = i
			} else {
				l.ErrQuit(1, "cannot convert %s to numeric value: %v", opt.Argument, err)
			}
		case "host":
			Config.ServerNames = append(Config.ServerNames, fmt.Sprintf("name == \"%s\"", opt.Argument))

		case "bastion":
			user, host, port, err := ParseUserHostPort(opt.Argument)
			if err != nil {
				l.ErrQuit(1, "cannot parse bastion endpoint: %v", err)
			}
			Config.BastionName = host
			Config.BastionPort = port
			Config.BastionUser = user

		case "force-bastion":
			Config.ForceBastionOnPublicHost = true

		case "timeout":
			f, err := strconv.ParseFloat(opt.Argument, 0)
			if err != nil {
				l.ErrQuit(1, "cannot convert %s to numeric value: %v", opt.Argument, err)
			}
			Config.Timeout = time.Duration(f * float64(time.Second))
			l.Debug("TIMEOUT: %v\n", Config.Timeout)

		case "deadline":
			f, err := strconv.ParseFloat(opt.Argument, 0)
			if err != nil {
				l.ErrQuit(1, "cannot convert %s to numberic value: %v", opt.Argument, err)
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
				l.ErrQuit(1, "cannot convert %s to numeric value: %v", opt.Argument, err)
			}
		case "inline":
			Config.InlineOutput = true
		case "inline-stdout":
			Config.InlineOutput = true
			Config.InlineStdoutOnly = true
		case "outdir":
			dir := ExpandPath(opt.Argument)
			if err := CheckOutputDirectory(dir, true); err != nil {
				l.ErrQuit(1, "invalid argument: %v", err)
			}
			Config.OutDirectory = dir
		case "errdir":
			dir := ExpandPath(opt.Argument)
			if err := CheckOutputDirectory(dir, true); err != nil {
				l.ErrQuit(1, "invalid argument: %v", err)
			}
			Config.ErrDirectory = dir
		case "default-user":
			Config.DefaultUser = opt.Argument
		case "no-cache":
			Config.NoCache = true
		case "ssh":
			Config.PrintMode = MODE_SSH
		case "scp":
			Config.PrintMode = MODE_SCP
		case "rsync":
			Config.PrintMode = MODE_RSYNC
		case "dryrun":
			Config.DryRun = true
		case "limit":
			i, err := strconv.ParseUint(opt.Argument, 10, 64)
			if err == nil {
				Config.InstanceLimits = i
			} else {
				l.ErrQuit(1, "cannot convert %s to numeric value: %v", opt.Argument, err)
			}
		default:
			l.ErrQuit(1, "unrecognized option -- %s: %v", opt.LongOption, err)
		}
	}

	if Config.InlineOutput && (Config.OutDirectory != "" || Config.ErrDirectory != "") {
		l.ErrQuit(1, "inline output(-i,--inline) cannot be used with (-o,--outdir,-e,--errdir)")
	}

	return context.Arguments()
}

func TritonClientConfig(config *TsshConfig) *triton.ClientConfig {
	signers := []authentication.Signer{}

	if config.AccountName == "" {
		l.Err("SDC_ACCOUNT enviornment variable is not set")
		l.ErrQuit(1, "Consider running 'eval \"$(triton env YOUR-PROFILE)\"'.")
	}
	if config.KeyId == "" {
		l.Err("SDC_KEY_ID environment variable is not set.")
		l.ErrQuit(1, "Consider running 'eval \"$(triton env YOUR-PROFILE)\"'.")
	}

	signers, err := GetSignersForTritonAPI(config.AccountName, config.KeyId, config.KeyPath)
	if err != nil {
		l.ErrQuit(1, "cannot get a signer for Triton Cloud API: %v", err)
	}

	c := triton.ClientConfig{TritonURL: config.TritonURL, MantaURL: os.Getenv("MANTA_URL"),
		AccountName: config.AccountName,
		Signers:     signers,
	}

	return &c
}

func SplitArgs(args []string) (string, []string) {
	if Config.PrintMode == MODE_PSSH && len(args) < 2 {
		l.Err("wrong number of argument(s)")
		l.ErrQuit(1, "Try with '--help' for more")
	}

	var exprbuf bytes.Buffer
	var commands []string

	var i int
	for i = 0; i < len(args); i++ {
		if args[i] == ":::" {
			i++
			break
		}
		exprbuf.WriteString(args[i])
		exprbuf.WriteString(" ")
	}
	for ; i < len(args); i++ {
		commands = append(commands, args[i])
	}

	p := strings.Trim(exprbuf.String(), " \t\v\n\r")

	if len(Config.ServerNames) > 0 {
		expr := strings.Join(Config.ServerNames, "||")

		if p == "" {
			p = expr
		} else {
			p = fmt.Sprintf("%s || ( %s )", expr, p)
		}
	}

	if p == "" {
		l.ErrQuit(1, "no expression specified")
	}

	if Config.PrintMode == MODE_PSSH && len(commands) == 0 {
		l.Err("no command specified")
		l.ErrQuit(1, "you might miss to use ':::' delimiter")
	}

	return p, commands
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

	l.Debug("read %d bytes from STDIN stored to %s\n", nwritten, input.Name())
	return input, nil
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

func main() {
	l.InitFromEnvironment("TP_LOG")

	l.Debug("Os.Args: %v\n", os.Args)

	initOptions := OptionsFromInitFile()
	l.Debug("Options From the option file: %v\n", initOptions)
	args := ParseOptions(append(initOptions, os.Args[1:]...))
	l.Debug("Config: %v", Config)

	if TritonProfileName == "" {
		l.Err("cannot determine Triton Profile from TRITON_PROFILE environment variable")
		l.ErrQuit(1, "Consider running 'eval \"$(triton env YOUR-PROFILE)\"'.")
	}

	expr, cmdline := SplitArgs(args)
	// if Config.Interactive && cmdline != "" {
	// 	Err(1, nil, "interactive mode cannot accept COMMAND...")
	// }

	l.Debug("Filter Expr: %s\n", expr)
	l.Debug("Command: %s\n", cmdline)

	if Config.TritonURL == "" {
		l.ErrQuit(1, "missing Triton endpoint. SDC_URL undefined")
	}

	tritonConfig := TritonClientConfig(&Config)

	tritonClient, err := compute.NewClient(tritonConfig)
	if err != nil {
		l.ErrQuit(1, "cannot create Triton compute client")
	}

	ImgCache = NewImageCache(tritonClient.Images(), Config.ImageCacheExpiration)
	if nClient, err := network.NewClient(tritonConfig); err != nil {
		l.ErrQuit(1, "cannot create Triton network client")
	} else {
		NetCache = NewNetworkCache(nClient, Config.NetworkCacheExpiration)
	}

	if Config.BastionName != "" {
		addr, user, err := getBastion(tritonClient, context.Background(), Config.BastionName)
		if err != nil {
			l.ErrQuit(1, "cannot determine bastion server: %v", err)
		}
		Config.BastionAddress = addr

		if Config.BastionUser == "" {
			Config.BastionUser = user
		}
	}

	l.Debug("Config: %v", Config)

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
	var matched uint64 = 0
	for instance := range instanceChan {
		if IsDockerContainer(instance) {
			continue
		}
		img, err := ImgCache.Get(instance.Image)
		if err != nil {
			img = &compute.Image{ID: instance.Image}
		}

		result, error := Evaluate(instance, img, expr)
		if error != nil {
			l.ErrQuit(1, "evaluation failed: %v", error)
		}
		if r := bool(result); !r {
			continue
		}

		matched++
		// fmt.Printf("INSTANCE[%v]: hasPublicNet(%v)\n", instance.Name, hasPublicNet(instance))
		// fmt.Printf("# %s [%v]:\n", instance.ID, instance.Name)

		if matched > Config.InstanceLimits {
			break
		}

		job, err := SSH.BuildJob(instance, &Config, cmdline, inputFile)
		if err != nil {
			l.Warn("warning: cannot create SSH job: %s", err)
			continue
		}
		job.DryRun = Config.DryRun

		if Config.PrintMode != MODE_PSSH {
			err := SSH.PrintConf(job, Config.PrintMode)
			if err != nil {
				l.ErrQuit(1, "failed to build the command-line: %v", err)
			}
			if matched == 0 {
				l.Err("no instance matched to your request.")
				l.ErrQuit(1, "Consider using `--no-cache' option to update the cache")
			}
			os.Exit(0)
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
		l.Debug("Status: [%T] %v", result.Status, result.Status)

		if Config.InlineOutput && result.Stdout != nil {
			io.Copy(os.Stdout, result.Stdout)
			os.Stdout.Sync()
		}
	}

	SSH.Close()

	if matched == 0 {
		l.Err("no instance matched to your request.")
		l.ErrQuit(1, "Consider using `--no-cache' option to update the cache")
	}
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
