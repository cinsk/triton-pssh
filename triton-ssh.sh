#!/bin/bash

PROGRAM_NAME=$(basename "$0")
if [ "$#" -eq 0 ]; then
    cat <<EOF
ssh wrapper to connect a Triton machine
Usage: $PROGRAM_NAME [OPTION...] FILTER-EXPRESSION

OPTION

      --no-cache           read all information directly from Triton Cloud API

  -b, --bastion=ENDPOINT   the endpoint([user@]name[:port]) of bastion server,
                             name must be a Triton instance name


      --default-user=USER  Use USER if the default user cannot be determined

  -k, --keyid=ID           the fingerprint of the SSH key for Triton Cloud API
                             access, this will override the value of SDC_KEY_ID.
  -K, --keyfile=KEYFILE    the private key to access Triton Cloud API, the will
                             override the value of SDC_KEY_FILE.
      --url=URL            the base endpoint for the Triton Cloud API, this
                             will override the value of SDC_URL.

  -u, --user=USER          the username of the remote hosts
  -P, --port=PORT          the SSH port of the remote hosts
   
EOF
    exit 0
fi
script=$(triton-pssh -1 "$@")
eval "$script"
