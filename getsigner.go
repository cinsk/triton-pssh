package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/joyent/triton-go/authentication"
)

func GetSigners(account string, keyId string, keyPath string) ([]authentication.Signer, error) {
	signers := []authentication.Signer{}

	if keyPath != "" {
		privateKey, err := ioutil.ReadFile(keyPath)
		if err != nil {
			Info.Printf("cannot read key file matching %s: %s", keyId, err)
		} else {
			signer, err := authentication.NewPrivateKeySigner(keyId, privateKey, account)
			if err != nil {
				Info.Printf("cannot get a signer from %s: %s", keyId, err)
			} else {
				signers = append(signers, signer)
			}
		}
	}

	signer, err := authentication.NewSSHAgentSigner(keyId, account)
	if err != nil {
		Info.Printf("cannot get a signer from the ssh agent: %s", err)
	} else {
		signers = append(signers, signer)
	}

	if len(signers) == 0 {
		default_private_key := filepath.Join(HomeDirectory, ".ssh", "id_rsa")
		privateKey, err := ioutil.ReadFile(default_private_key)
		if err != nil {
			Info.Printf("cannot read key file matching %s: %s", keyId, err)
		} else {
			signer, err := authentication.NewPrivateKeySigner(keyId, privateKey, account)
			if err != nil {
				Info.Printf("cannot get a signer from %s: %s", keyId, err)
			} else {
				signers = append(signers, signer)
			}
		}
	}

	if len(signers) == 0 {
		return signers, fmt.Errorf("no available signers")
	}
	return signers, nil
}
