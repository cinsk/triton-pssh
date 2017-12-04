package main

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"golang.org/x/crypto/ssh"
)

const TEST_SERVER = "165.225.136.229"

func Test_SshSession_Returns_Error(env *testing.T) {
	config := Config

	resultChannel := make(chan SshResult)
	instanceID := "test-server-id"
	instanceName := "test-server"

	job := SshJob{
		ServerConfig: &ssh.ClientConfig{},
		Server:       fmt.Sprintf("%s:80", TEST_SERVER), // port 80 was intentional to fail the connection

		InstanceName: instanceName,
		InstanceID:   instanceID,
		Command:      []string{"ls"},
		Result:       resultChannel,
	}

	s := NewSshSession(&config, 1)
	s.Run(&job)
	result := <-resultChannel

	env.Logf("SshResult: %v", result)
	env.Logf("SshResult.Status: [%T]%v", result.Status, result.Status)

	if result.InstanceID != instanceID {
		env.Errorf("SshResult.InstanceID(%s) does not match to the input(%s)", result.InstanceID, instanceID)
	}
	if result.InstanceName != instanceName {
		env.Errorf("SshResult.InstanceName(%s) does not match to the input(%s)", result.InstanceName, instanceName)
	}

	if result.Status == nil {
		env.Errorf("SshResult.InstanceName(%s) does not match to the input(%s)", result.InstanceName, instanceName)
	}

	s.Close()
}

func Test_SshSession_workers(env *testing.T) {
	config := Config

	nworkers := 1

	s := NewSshSession(&config, nworkers)
	if s.nworkers != nworkers {
		env.Errorf("number of workers(%d) does not match to the input to NewSshSession(%d)", s.nworkers, nworkers)
	}
	s.Close()

	nworkers = 10
	s = NewSshSession(&config, nworkers)
	if s.nworkers != nworkers {
		env.Errorf("number of workers(%d) does not match to the input to NewSshSession(%d)", s.nworkers, nworkers)
	}
	s.Close()
}

func TestSsh_PrintScpConf_WithoutBastion(env *testing.T) {
	out := bytes.Buffer{}

	err := PrintScpConf(&out, "", "", "", "HOST", "PORT", "USER", []string{"-SCP_OPT", "SCP ARG", "{}:THE DIR"})
	if err != nil {
		env.Errorf("unexpected error: %v", err)
	}

	expected := `(scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -P PORT -SCP_OPT 'SCP ARG' 'USER@HOST:THE DIR')`

	if out.String() != expected {
		env.Errorf("expected |%v|, got |%v|", expected, out.String())
	}
}

func TestSsh_PrintScpConf_WithBastion(env *testing.T) {
	out := bytes.Buffer{}

	err := PrintScpConf(&out, "BHOST", "BPORT", "BUSER", "HOST", "PORT", "USER", []string{"-SCP_OPT", "SCP ARG", "{}:THE DIR"})
	if err != nil {
		env.Errorf("unexpected error: %v", err)
	}

	expected := `(scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o "ProxyCommand=ssh -p BPORT -q BUSER@BHOST nc %h %p" -P PORT -SCP_OPT 'SCP ARG' 'USER@HOST:THE DIR')`

	if out.String() != expected {
		env.Errorf("expected |%v|, got |%v|", expected, out.String())
	}
}

func TestSsh_PrintRsyncConf_WithoutBastion(env *testing.T) {
	os.Setenv("SSH_AUTH_SOCK", "TEST")
	out := bytes.Buffer{}

	err := PrintRsyncConf(&out, "", "", "", "HOST", "PORT", "USER", []string{"-RSYNC_OPT", "RSYNC ARG", "{}:THE DIR"})
	if err != nil {
		env.Errorf("unexpected error: %v", err)
	}

	expected := `(rsync -e 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p PORT' -RSYNC_OPT 'RSYNC ARG' 'USER@HOST:THE DIR')`

	if out.String() != expected {
		env.Errorf("expected |%v|, got |%v|", expected, out.String())
	}
}

func TestSsh_PrintRsyncConf_WithBastion(env *testing.T) {
	os.Setenv("SSH_AUTH_SOCK", "TEST")
	out := bytes.Buffer{}

	err := PrintRsyncConf(&out, "BHOST", "BPORT", "BUSER", "HOST", "PORT", "USER", []string{"-RSYNC_OPT", "RSYNC ARG", "{}:THE DIR"})
	if err != nil {
		env.Errorf("unexpected error: %v", err)
	}

	expected := `(rsync -e 'ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o "ProxyCommand=ssh -p BPORT -q BUSER@BHOST nc %h %p" -p PORT' -RSYNC_OPT 'RSYNC ARG' 'USER@HOST:THE DIR')`

	if out.String() != expected {
		env.Errorf("expected |%v|, got |%v|", expected, out.String())
	}
}

func TestSsh_PrintSshConf_WithoutBastion(env *testing.T) {
	os.Setenv("SSH_AUTH_SOCK", "TEST")
	out := bytes.Buffer{}

	err := PrintSshConf(&out, "", "", "", "HOST", "PORT", "USER", []string{"-M", "-v"})
	if err != nil {
		env.Errorf("unexpected error: %v", err)
	}

	expected := `(ssh -A -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p PORT -M -v "USER@HOST")`

	if out.String() != expected {
		env.Errorf("expected |%v|, got |%v|", expected, out.String())
	}
}

func TestSsh_PrintSshConf_WithBastion(env *testing.T) {
	os.Setenv("SSH_AUTH_SOCK", "TEST")
	out := bytes.Buffer{}

	err := PrintSshConf(&out, "BHOST", "BPORT", "BUSER", "HOST", "PORT", "USER", []string{"-M", "-v"})
	if err != nil {
		env.Errorf("unexpected error: %v", err)
	}

	expected := `(ssh -A -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o "ProxyCommand=ssh -p BPORT -q BUSER@BHOST nc %h %p" -p PORT -M -v "USER@HOST")`

	if out.String() != expected {
		env.Errorf("expected |%v|, got |%v|", expected, out.String())
	}
}
