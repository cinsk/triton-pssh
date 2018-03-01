package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	l "github.com/cinsk/triton-pssh/log"
	"github.com/joyent/triton-go/authentication"
)

func GetSignersForTritonAPI(account string, keyId string, keyPath string) ([]authentication.Signer, error) {
	l.Debug("GetSigner: account=%v, keyId=%v, keyPath=%v", account, keyId, keyPath)
	signers := []authentication.Signer{}

	if keyPath != "" {
		privateKey, err := ioutil.ReadFile(keyPath)
		if err != nil {
			l.Warn("cannot read key file matching keyid=[%s]: %s", keyId, err)
		} else {
			signer, err := authentication.NewPrivateKeySigner(authentication.PrivateKeySignerInput{
				KeyID:              keyId,
				PrivateKeyMaterial: privateKey,
				AccountName:        account})
			if err != nil {
				l.Warn("cannot get a signer from %s: %s", keyId, err)
			} else {
				signers = append(signers, signer)
			}
		}
	}

	signer, err := authentication.NewSSHAgentSigner(
		authentication.SSHAgentSignerInput{
			KeyID:       keyId,
			AccountName: account})
	if err != nil {
		l.Info("cannot get a signer from the ssh agent: %s", err)
	} else {
		signers = append(signers, signer)
	}

	if len(signers) == 0 {
		default_private_key := filepath.Join(HomeDirectory, ".ssh", "id_rsa")
		privateKey, err := ioutil.ReadFile(default_private_key)
		if err != nil {
			l.Warn("cannot read key file matching %s: %s", keyId, err)
		} else {
			signer, err := authentication.NewPrivateKeySigner(
				authentication.PrivateKeySignerInput{
					KeyID:              keyId,
					PrivateKeyMaterial: privateKey,
					AccountName:        account})
			if err != nil {
				l.Warn("cannot get a signer from %s: %s", keyId, err)
			} else {
				l.Debug("select the private key, \"%s\" as a Triton authentication", default_private_key)
				signers = append(signers, signer)
			}
		}
	}

	if len(signers) == 0 {
		return signers, fmt.Errorf("no available signers")
	}
	return signers, nil
}
