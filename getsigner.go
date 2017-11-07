package main

import (
	"fmt"
	"io/ioutil"

	"github.com/joyent/triton-go/authentication"
)

func GetSigner(account string, keyId string, keyPath string) (authentication.Signer, error) {
	if keyPath != "" {
		privateKey, err := ioutil.ReadFile(keyPath)
		if err != nil {
			return &authentication.PrivateKeySigner{}, fmt.Errorf("cannot find key file matching %s\n%s", keyId, err)
		}
		signer, err := authentication.NewPrivateKeySigner(keyId, privateKey, account)
		if err != nil {
			return &authentication.PrivateKeySigner{}, fmt.Errorf("cannot get a signer for Triton: %s", err)
		}
		return signer, nil
	} else {
		signer, err := authentication.NewSSHAgentSigner(keyId, account)
		if err != nil {
			return &authentication.SSHAgentSigner{}, fmt.Errorf("cannot get a signer from the agent: %s", err)
		}
		return signer, nil
	}
}
