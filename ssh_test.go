package main

import (
	"fmt"
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
		Command:      "ls",
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
