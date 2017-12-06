package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/terminal"
)

type AuthMethods struct {
	methods []ssh.AuthMethod
}

func (m AuthMethods) String() string {
	buf := bytes.Buffer{}

	buf.WriteString(fmt.Sprintf("AuthMethod(len=%d){ methods={ ", len(m.methods)))
	for _, me := range m.methods {
		buf.WriteString(fmt.Sprintf("[%T]%v ", me, me))
	}
	buf.WriteString("} }")
	return buf.String()
}

func (m *AuthMethods) AddDefaults() {
	// the order is important here
	m.AddPrivateKey(filepath.Join(HomeDirectory, ".ssh", "id_ed25519"))
	m.AddPrivateKey(filepath.Join(HomeDirectory, ".ssh", "id_ecdsa"))
	m.AddPrivateKey(filepath.Join(HomeDirectory, ".ssh", "id_dsa"))
	m.AddPrivateKey(filepath.Join(HomeDirectory, ".ssh", "id_rsa"))
}

func (m *AuthMethods) Methods() []ssh.AuthMethod {
	return m.methods
}

func (m *AuthMethods) prepend(method ssh.AuthMethod) {
	m.methods = append([]ssh.AuthMethod{method}, m.methods...)
}

func (m *AuthMethods) AddPassword() error {
	if !terminal.IsTerminal(int(syscall.Stdin)) {
		return fmt.Errorf("stdin is not a terminal, required by password authentication")
	}

	fmt.Fprintf(os.Stderr, "password: ")
	password, err := terminal.ReadPassword(int(syscall.Stdin))
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		return err
	}
	m.prepend(ssh.Password(string(password)))
	return nil
}

func (m *AuthMethods) AddPrivateKey(filename string) error {
	stat, err := os.Stat(filename)

	if err != nil {
		return err
	}

	bits := stat.Mode().Perm() & (S_IRGRP | S_IWGRP | S_IXGRP | S_IROTH | S_IWOTH | S_IXOTH)
	if int(bits) != 0 {
		return fmt.Errorf("wrong permission for the key file: %s", filename)
	}

	buffer, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("cannot read key file(%s): %s", filename, err)
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return fmt.Errorf("cannot parse key file from %s: %s", filename, err)
	}

	m.prepend(ssh.PublicKeys(key))
	return nil
}

func (m *AuthMethods) AddAgent() error {
	authfile := os.Getenv("SSH_AUTH_SOCK")
	if authfile == "" {
		return fmt.Errorf("SSH_AUTH_SOCK not defined")
	}

	Debug.Printf("found SSH Agent: %s", authfile)

	sshAgent, err := net.Dial("unix", authfile)
	if err != nil {
		return err
	}

	m.prepend(ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
	return nil
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

func PublicKeyAuth(privateKeyFile string) (ssh.AuthMethod, error) {
	stat, err := os.Stat(privateKeyFile)
	if err == nil {
		bits := stat.Mode().Perm() & (S_IRGRP | S_IWGRP | S_IXGRP | S_IROTH | S_IWOTH | S_IXOTH)
		if int(bits) != 0 {
			return nil, fmt.Errorf("wrong permission for the key file: %s", privateKeyFile)
		}
	} else {
		if os.IsNotExist(err) {
			return nil, nil
		} else {
			return nil, err
		}
	}

	buffer, err := ioutil.ReadFile(privateKeyFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read key file(%s): %s", privateKeyFile, err)
	}

	key, err := ssh.ParsePrivateKey(buffer)
	if err != nil {
		return nil, fmt.Errorf("cannot parse key file from %s: %s", privateKeyFile, err)
	}
	return ssh.PublicKeys(key), nil
}
