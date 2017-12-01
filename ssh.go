package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/joyent/triton-go/compute"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

type PrintConfMode int

const (
	MODE_PSSH PrintConfMode = iota
	MODE_SSH
	MODE_SCP
	MODE_RSYNC
)

var regexpHost = regexp.MustCompile("([ \t]*)\\{\\}(.*)$")

type SshJob struct {
	ServerConfig  *ssh.ClientConfig
	BastionConfig *ssh.ClientConfig

	Server  string
	Bastion string

	InstanceName string
	InstanceID   string

	Pty *RequestPty

	Input io.ReadCloser

	Command string

	Result chan SshResult
}

type RequestPty struct {
	Term   string
	Width  int
	Height int
}

type SshResult struct {
	Server       string
	InstanceName string
	InstanceID   string
	User         string

	Stdout *bytes.Buffer
	Stderr *bytes.Buffer

	Time   time.Time
	Status error
}

type SshSession struct {
	config *TsshConfig
	input  chan *SshJob

	workerGroup sync.WaitGroup
	nworkers    int
}

func NewSshSession(config *TsshConfig, nworkers int) *SshSession {
	session := SshSession{config: config, input: make(chan *SshJob)}

	for i := 0; i < nworkers; i++ {
		session.workerGroup.Add(1)
		session.nworkers++
		go session.worker(i)
	}

	return &session
}

func (s *SshSession) BuildJob(instance *compute.Instance, auths []ssh.AuthMethod, command string, stdin *os.File) (*SshJob, error) {
	user := s.config.User
	if user == "" {
		img, _ := ImgCache.Get(instance.Image)
		user = DefaultUser(img)
	}

	public := NetCache.HasPublic(instance)

	job := SshJob{}

	job.ServerConfig = &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		Timeout:         s.config.Timeout,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil },
	}
	job.Server = fmt.Sprintf("%s:%d", instance.PrimaryIP, s.config.ServerPort)

	if !public {
		if s.config.BastionAddress == "" {
			return nil, fmt.Errorf("cannot connect to the instance(%s) without bastion server", instance.Name)
		}

		job.BastionConfig = &ssh.ClientConfig{
			User:            s.config.BastionUser,
			Auth:            []ssh.AuthMethod{AgentAuth()},
			Timeout:         s.config.Timeout,
			HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil },
		}
		job.Bastion = fmt.Sprintf("%s:%d", s.config.BastionAddress, s.config.BastionPort)
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

func (s *SshSession) Run(job *SshJob) {
	s.input <- job
}

func (s *SshSession) Close() {
	close(s.input)
	s.workerGroup.Wait()
}

func (s *SshSession) worker(wid int) {
	Debug.Printf("SshWorker[%d] started...", wid)
	defer s.workerGroup.Done()
	defer func() { s.nworkers-- }()

	for job := range s.input {
		result := s.doSSH(job, wid)
		Debug.Printf("SshWorker[%d] result.Status = %v", wid, result.Status)
		go func(out chan SshResult, result SshResult) {
			defer close(out)
			out <- result
		}(job.Result, result)
	}
	Debug.Printf("SshWorker[%d] finished", wid)
}

func (s *SshSession) doSSH(job *SshJob, wid int) SshResult {
	var client *ssh.Client
	var err error

	if job.Input != nil {
		defer job.Input.Close()
	}

	if job.BastionConfig != nil {
		Debug.Printf("SshWorker[%d].doSSH: creating ssh.Client for bastion %s", wid, job.Bastion)
		client, err = s.sshClient(job.Bastion, job.BastionConfig)
		if err != nil {
			return SshResult{Status: err,
				Time:   time.Now(),
				Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
		}

		conn, err := client.Dial("tcp", job.Server)
		if err != nil {
			client.Close()
			return SshResult{Status: err, // fmt.Errorf("ssh.Client.Dial() failed: %s\n", err),
				Time:   time.Now(),
				Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
		}

		nc, chans, reqs, err := ssh.NewClientConn(conn, job.Server, job.ServerConfig)
		if err != nil {
			conn.Close()
			client.Close()
			return SshResult{Status: err, // fmt.Errorf("error: ssh.NewClientConn() failed: %s", err),
				Time:   time.Now(),
				Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
		}
		Debug.Printf("SshWorker[%d].doSSH: creating ssh.Client for server %s through bastion %s", wid, job.Server, job.Bastion)
		client = ssh.NewClient(nc, chans, reqs)
	} else {
		Debug.Printf("SshWorker[%d].doSSH: creating ssh.Client for direct connect to %s", wid, job.Server)

		client, err = s.sshClient(job.Server, job.ServerConfig)
		if err != nil {
			return SshResult{Status: err,
				Time:   time.Now(),
				Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
		}
	}
	defer client.Close() // TODO: should I Close() twice for the bastion path?

	session, err := client.NewSession()
	if err != nil {
		return SshResult{Status: err, // fmt.Errorf("ssh.Session.NewSession() failed: %s", err),
			Time:   time.Now(),
			Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
	}

	defer session.Close()
	if job.Pty != nil {
		Debug.Printf("SshWorker[%d].doSSH: allocating Pty: %v", wid, job.Pty)
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := session.RequestPty(job.Pty.Term, job.Pty.Height, job.Pty.Width, modes); err != nil {
			return SshResult{Status: fmt.Errorf("ssh.Session.RequestPty() failed: %s", err),
				Time:   time.Now(),
				Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
		}
	}

	wg := sync.WaitGroup{}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return SshResult{Status: fmt.Errorf("ssh.Session.StdoutPipe() failed: %s", err),
			Time:   time.Now(),
			Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
	} else {
		wg.Add(1)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return SshResult{Status: fmt.Errorf("ssh.Session.StderrPipe() failed: %s", err),
			Time:   time.Now(),
			Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
	} else {
		wg.Add(1)
	}

	var stdin io.WriteCloser
	if job.Input != nil {
		Debug.Printf("creating STDIN pipe, %v", job.Input)
		stdin, err = session.StdinPipe()
		if err != nil {
			return SshResult{Status: fmt.Errorf("ssh.Session.StdinPipe() failed: %s", err),
				Time:   time.Now(),
				Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
		} else {
			wg.Add(1)
		}
	}

	result := SshResult{Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}

	if s.config.InlineOutput { // inline
		var stdoutBuffer bytes.Buffer
		var stderrBuffer bytes.Buffer

		result.Stdout = &stdoutBuffer
		result.Stderr = &stderrBuffer

		go func() {
			defer wg.Done()
			//io.Copy(os.Stdout, stdout)
			n, err := stdoutBuffer.ReadFrom(stdout)
			Debug.Printf("SshWorker[%d].doSSH: copying stdout: %d bytes, err = %v", wid, n, err)

		}()
		go func() {
			defer wg.Done()
			//io.Copy(os.Stdout, stderr)

			n, err := stderrBuffer.ReadFrom(stderr)
			Debug.Printf("SshWorker[%d].doSSH: copying stderr: %d bytes, err = %v", wid, n, err)
		}()
		if stdin != nil {
			go func() {
				defer wg.Done()
				defer stdin.Close()
				//io.Copy(os.Stdout, stderr)

				nwritten, err := io.Copy(stdin, job.Input)

				Debug.Printf("SshWorker[%d].doSSH: copying stdin: %d bytes, err = %v", wid, nwritten, err)
			}()
		}
	} else {
		if stdin != nil {
			go func() {
				defer wg.Done()
				defer stdin.Close()
				//io.Copy(os.Stdout, stderr)

				nwritten, err := io.Copy(stdin, job.Input)

				Debug.Printf("SshWorker[%d].doSSH: copying stdin: %d bytes, err = %v", wid, nwritten, err)
			}()
		}

		if s.config.OutDirectory != "" {
			outname := filepath.Join(s.config.OutDirectory, fmt.Sprintf("%s@%s", job.ServerConfig.User, job.InstanceID))
			out, err := os.Create(outname)
			if err != nil {
				return SshResult{Status: fmt.Errorf("cannot create a file %s: %s", outname, err),
					Time:   time.Now(),
					Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
			}

			go func() {
				defer wg.Done()
				n, err := io.Copy(out, stdout)
				Debug.Printf("SshWorker[%d].doSSH: copying stdout: %d bytes, err = %v", wid, n, err)
			}()
		} else {
			go func() {
				defer wg.Done()
				n, err := io.Copy(ioutil.Discard, stdout)
				Debug.Printf("SshWorker[%d].doSSH: discarding stdout: %d bytes, err = %v", wid, n, err)
			}()
		}

		if s.config.ErrDirectory != "" {
			outname := filepath.Join(s.config.ErrDirectory, fmt.Sprintf("%s@%s", job.ServerConfig.User, job.InstanceID))
			out, err := os.Create(outname)
			if err != nil {
				return SshResult{Status: fmt.Errorf("cannot create a file %s: %s", outname, err),
					Time:   time.Now(),
					Server: job.Server, InstanceID: job.InstanceID, InstanceName: job.InstanceName, User: job.ServerConfig.User}
			}

			go func() {
				defer wg.Done()
				n, err := io.Copy(out, stderr)
				Debug.Printf("SshWorker[%d].doSSH: copying stderr: %d bytes, err = %v", wid, n, err)
			}()
		} else {
			go func() {
				defer wg.Done()
				n, err := io.Copy(ioutil.Discard, stderr)
				Debug.Printf("SshWorker[%d].doSSH: discarding stderr: %d bytes, err = %v", wid, n, err)
			}()
		}
	}

	command := job.Command
	if !s.config.InlineStdoutOnly {
		command = fmt.Sprintf("exec 2>&1; %s", command)
	}

	Debug.Printf("SshWorker[%d].doSSH: executing a command: %s", wid, command)
	err = session.Run(command)
	Debug.Printf("SshWorker[%d].doSSH: command result(err): %v", wid, err)
	result.Time = time.Now()
	result.Status = err

	wg.Wait()

	return result
}

func ExpandPlaceholder(command string, repl string) (string, error) {
	if regexpHost.FindStringIndex(command) == nil {
		return "", fmt.Errorf("placeholder {} not found")
	}

	replaced := regexpHost.ReplaceAllString(command, fmt.Sprintf("$1\"%v$2\"", repl))
	return replaced, nil
}

func PrintScpConf(out *bytes.Buffer, bastion string, bastionPort string, bastionUser string, host string, hostPort string, hostUser string, command string) error {
	// the output should be the bash array literal, (".." "..." ...)
	var bEndpoint, hEndpoint string
	if bastionUser == "" {
		bEndpoint = fmt.Sprintf("%s", bastion)
	} else {
		bEndpoint = fmt.Sprintf("%s@%s", bastionUser, bastion)
	}
	if hostUser == "" {
		hEndpoint = fmt.Sprintf("%s", host)
	} else {
		hEndpoint = fmt.Sprintf("%s@%s", hostUser, host)
	}

	out.WriteString("(")
	out.WriteString("scp ")
	out.WriteString("-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ")

	if bastion != "" {
		out.WriteString(fmt.Sprintf("-o \"ProxyCommand=ssh -p %s -q %s nc %%h %%p\" ", bastionPort, bEndpoint))
	}

	out.WriteString(fmt.Sprintf("-P %s ", hostPort))

	replaced, err := ExpandPlaceholder(command, hEndpoint)
	if err != nil {
		return err
	}
	out.WriteString(replaced)
	out.WriteString(")")

	return nil
}

func PrintRsyncConf(out *bytes.Buffer, bastion string, bastionPort string, bastionUser string, host string, hostPort string, hostUser string, command string) error {
	// the output should be the bash array literal, (".." "..." ...)
	var bEndpoint, hEndpoint string
	if bastionUser == "" {
		bEndpoint = fmt.Sprintf("%s", bastion)
	} else {
		bEndpoint = fmt.Sprintf("%s@%s", bastionUser, bastion)
	}
	if hostUser == "" {
		hEndpoint = fmt.Sprintf("%s", host)
	} else {
		hEndpoint = fmt.Sprintf("%s@%s", hostUser, host)
	}

	out.WriteString("(")
	out.WriteString("rsync ")
	out.WriteString("-e 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ")

	if bastion != "" {
		out.WriteString(fmt.Sprintf("-o \"ProxyCommand=ssh -p %s -q %s nc %%h %%p\" ", bastionPort, bEndpoint))
	}
	out.WriteString(fmt.Sprintf("-p %s' ", hostPort))

	replaced, err := ExpandPlaceholder(command, hEndpoint)
	if err != nil {
		return err
	}
	out.WriteString(replaced)
	out.WriteString(")")

	return nil
}

func PrintSshConf(out *bytes.Buffer, bastion string, bastionPort string, bastionUser string, host string, hostPort string, hostUser string, command string) error {
	// the output should be the bash array literal, (".." "..." ...)
	var bEndpoint, hEndpoint string
	if bastionUser == "" {
		bEndpoint = fmt.Sprintf("%s", bastion)
	} else {
		bEndpoint = fmt.Sprintf("%s@%s", bastionUser, bastion)
	}
	if hostUser == "" {
		hEndpoint = fmt.Sprintf("%s", host)
	} else {
		hEndpoint = fmt.Sprintf("%s@%s", hostUser, host)
	}

	out.WriteString("(")
	out.WriteString("ssh ")

	if os.Getenv("SSH_AUTH_SOCK") != "" {
		out.WriteString("-A ")
	}

	out.WriteString("-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null ")

	if bastion != "" {
		out.WriteString(fmt.Sprintf("-o \"ProxyCommand=ssh -p %s -q %s nc %%h %%p\" ", bastionPort, bEndpoint))
	}

	out.WriteString(fmt.Sprintf("-p %s ", hostPort))
	out.WriteString(command)
	out.WriteString(fmt.Sprintf(" \"%s\"", hEndpoint))
	out.WriteString(")")

	return nil
}

func (s *SshSession) PrintConf(job *SshJob, mode PrintConfMode) error {
	buf := bytes.Buffer{}

	buf.WriteString("cmdline=")

	var bastionHost, bastionPort, bastionUser, host, port, user string

	if job.BastionConfig != nil {
		toks := strings.Split(job.Bastion, ":")
		if len(toks) != 2 {
			return fmt.Errorf("cannot get host:port from %s", job.Bastion)
		}

		bastionHost = toks[0]
		bastionPort = toks[1]
		bastionUser = job.BastionConfig.User
	}
	toks := strings.Split(job.Server, ":")
	if len(toks) != 2 {
		return fmt.Errorf("cannot get host:port from %s", job.Server)
	}
	host = toks[0]
	port = toks[1]
	user = job.ServerConfig.User

	var err error
	switch mode {
	case MODE_RSYNC:
		err = PrintRsyncConf(&buf, bastionHost, bastionPort, bastionUser, host, port, user, job.Command)
	case MODE_SCP:
		err = PrintScpConf(&buf, bastionHost, bastionPort, bastionUser, host, port, user, job.Command)
	case MODE_SSH:
		err = PrintSshConf(&buf, bastionHost, bastionPort, bastionUser, host, port, user, job.Command)
	default:
		panic(fmt.Sprintf("unsupported mode=%s", mode))
	}
	if err != nil {
		return err
	}

	buf.WriteString("\n")
	buf.WriteString("\"${cmdline[@]}\"")

	Debug.Printf("CMDLINE: %v", buf.String())
	fmt.Println(buf.String())
	return nil
}

func (s *SshSession) sshClient(endpoint string, config *ssh.ClientConfig) (*ssh.Client, error) {
	// dialer := net.Dialer{Timeout: config.Timeout, Deadline: time.Now().Add(Config.Deadline)}
	dialer := net.Dialer{Timeout: config.Timeout}
	if s.config.Deadline > 0 {
		dialer.Deadline = time.Now().Add(s.config.Deadline)
	}

	conn, err := dialer.Dial("tcp", endpoint)
	if err != nil {
		return nil, err // fmt.Errorf("error: Dial() failed: %s", err)
	}

	if s.config.Deadline > 0 {
		conn.SetDeadline(time.Now().Add(s.config.Deadline))
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, endpoint, config)
	if err != nil {
		return nil, err // fmt.Errorf("error: ssh.NewClientConn() failed: %s", err)
	}

	client := ssh.NewClient(c, chans, reqs)
	//defer client.Close()

	return client, nil
}

func AgentAuth() ssh.AuthMethod {
	authfile := os.Getenv("SSH_AUTH_SOCK")
	if authfile == "" {
		return nil
	}

	Debug.Printf("found SSH Agent: %s", authfile)

	if sshAgent, err := net.Dial("unix", authfile); err == nil {
		return ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers)
	} else {
		Warn.Printf("warning: fail to create agent client: %s", err)
		return nil
	}
}

func PublicKeyAuth(publicKeyFile string) (ssh.AuthMethod, error) {
	stat, err := os.Stat(publicKeyFile)
	if err == nil {
		bits := stat.Mode().Perm() & (S_IRGRP | S_IWGRP | S_IXGRP | S_IROTH | S_IWOTH | S_IXOTH)
		if int(bits) != 0 {
			return nil, fmt.Errorf("wrong permission for the key file: %s", publicKeyFile)
		}
	} else {
		if os.IsNotExist(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}

	buffer, err := ioutil.ReadFile(publicKeyFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read key file(%s): %s", publicKeyFile, err)
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return nil, fmt.Errorf("cannot parse key file from %s: %s", publicKeyFile, err)
	}
	return ssh.PublicKeys(key), nil
}

/*
func tmp_main() {
	/*
	   1052  joyent-curl /my/machines?name=bastion -s | json
	   1053  ./tssh 'haspublic(networks)'
	   1054  ./tssh 'true'
	   1055  pssh
	   1056  pssh --help
	   1057  file $(which pssh)
	   1058  ./tssh 'true'
	   1059  ./tssh 'true' 2>/dev/null

	//

	sshConfig := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
		// ssh.Password("2bnot2b"),
		// AgentAuth(),
		//PublicKeyAuth("/Users/seong-kookshin/.ssh/id_rsa"),
		},
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil },
	}

	// conn, err := sshClient("165.225.136.229:22", *sshConfig)
	conn, err := sshClient("165.225.165.214:8080", sshConfig)
	// conn, err := ssh.Dial("tcp", "165.225.136.229:22", sshConfig)
	// conn, err := ssh.Dial("tcp", BASTION, sshConfig) // conn == ssh.Client

	if err != nil {
		fmt.Printf("error: sshClient failed: %s\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	if false {
		c, err := conn.Dial("tcp", KAFKA1)
		if err != nil {
			fmt.Printf("error: ssh.Client.Dial() failed: %s\n", err)
			os.Exit(1)
		}

		nc, chans, reqs, err := ssh.NewClientConn(c, KAFKA1, sshConfig)
		if err != nil {
			fmt.Printf("error: ssh.Client.NewClientConn() failed: %s\n", err)
			os.Exit(1)
		}

		conn = ssh.NewClient(nc, chans, reqs)
	}

	session, err := conn.NewSession() // session == ssh.Client
	if err != nil {
		fmt.Printf("error: NewSession() failed: %s\n", err)
		os.Exit(1)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          0,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	w, h := TerminalSize()
	if err := session.RequestPty("xterm", h, w, modes); err != nil {
		session.Close()
		fmt.Printf("error: RequestPty() failed: %s\n", err)
		os.Exit(1)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		fmt.Printf("error: StdinPipe() failed: %s\n", err)
		os.Exit(1)
	}
	go io.Copy(stdin, os.Stdin)

	wg := sync.WaitGroup{}
	wg.Add(2)

	stdout, err := session.StdoutPipe()
	if err != nil {
		fmt.Printf("error: StdoutPipe() failed: %s\n", err)
		os.Exit(1)
	}
	go func() {
		defer wg.Done()
		io.Copy(os.Stdout, stdout)
	}()

	stderr, err := session.StderrPipe()
	if err != nil {
		fmt.Printf("error: StderrPipe() failed: %s\n", err)
		os.Exit(1)
	}
	go func() {
		defer wg.Done()
		io.Copy(os.Stderr, stderr)
	}()

	input := strings.Join(os.Args[1:], " ")
	err = session.Run(input)

	if err != nil {
		switch e := (err).(type) {
		case *ssh.ExitError:
			fmt.Printf("error: Run() failed: Remote command returns %v\n", e.Waitmsg)
		default:
			fmt.Printf("error: Run() failed: [%T] s\n", err, err)
		}
	}

	wg.Wait()
}

func ssh_main() {
	session := NewSshSession(8)

	servers := []string{
		"72.2.112.100:22",
		"165.225.136.229:22",
		//"72.2.119.171:22",
		"72.2.119.25:22",
		"72.2.112.100:22",
		"165.225.136.229:22",
		"72.2.119.25:22",
		"72.2.112.100:22",
		"165.225.136.229:22",
		"72.2.119.25:22",
		"72.2.112.100:22",
		"165.225.136.229:22",
		"72.2.119.25:22",
	}
	aggreg := make(chan SshResult)
	jobwg := sync.WaitGroup{}

	var input *os.File
	var err error
	if IsPipe(os.Stdin) {
		input, err = ioutil.TempFile("", "triton-pssh-input")
		if err != nil {
			fmt.Printf("cannot create tmp file: %s\n", err)
			os.Exit(1)
		}
		defer func() { os.Remove(input.Name()) }()
		nwritten, err := io.Copy(input, os.Stdin)
		if err != nil {
			fmt.Printf("cannot copy STDIN to tmp file(%s): %s\n", input.Name(), err)
			os.Exit(1)
		} else {
			fmt.Printf("read %d bytes from STDIN\n", nwritten)
		}
	}

	for _, s := range servers {
		jobwg.Add(1)
		fmt.Printf("server: %s\n", s)
		var job SshJob

		job.ServerConfig = &ssh.ClientConfig{
			User: "root",
			//Auth:            []ssh.AuthMethod{PublicKeyAuth("/Users/seong-kookshin/.ssh/id_rsa")},
			HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil }}

		job.Server = s
		//job.Pty = &RequestPty{"xterm", 80, 24}

		resultChannel := make(chan SshResult)
		job.Result = resultChannel

		job.Command = strings.Join(os.Args[1:], " ")

		if input != nil {
			in, err := os.Open(input.Name())
			if err != nil {
				Err(1, err, "cannot open %s for input: %s", input.Name(), err)
			}
			job.Input = in
		} else {
			fmt.Printf("NULL INPUT\n")
		}

		session.input <- &job

		// result := <-resultChannel

		go func(input chan SshResult) {
			defer jobwg.Done()
			// defer close(input)

			result := <-input
			aggreg <- result
		}(resultChannel)

		//fmt.Printf("result: %v\n", result)
		//fmt.Printf("result STDOUT: %v\n", result.Stdout.Len())
	}

	go func() {
		defer close(aggreg)
		jobwg.Wait()
		Debug.Printf("Waiting for getting all result..")
	}()

	for result := range aggreg {
		fmt.Fprintf(os.Stderr, "# %s\n", result.Server)
		if result.Status != nil {
			fmt.Fprintf(os.Stderr, "%s\n", result.Status)
		} else {
			if result.Stdout != nil {
				io.Copy(os.Stdout, result.Stdout)
				os.Stdout.Sync()
			}
		}
	}

	session.Close()
}
*/
