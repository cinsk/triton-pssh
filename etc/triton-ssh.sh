#!/bin/bash

PROGRAM_NAME=$(basename "$0")

if [ "$#" -eq 0 ]; then
    cat <<EOF
Usage: $PROGRAM_NAME [TRITON-PSSH-OPTION...] -h HOST [::: SSH-OPTION...]
       $PROGRAM_NAME [TRITON-PSSH-OPTION...] FILTER-EXPRESSION [::: SSH-OPTION...]

FILTER-EXPRESSION is explained in http://github.com/cinsk/triton-pssh/README.md

Unlike 'triton-pssh', this command requires nc(1) installed on the Bastion host
when '-b BASTION' supplied.

Examples:
        # connect the Triton machine named 'my-instance' with public IP
        $ $PROGRAM_NAME -h my-instance

        # update local cache, then connect the Triton machine named 'my-instance'
        $ $PROGRAM_NAME --no-cache -h my-instance

        # connect the Triton machine named 'backend' through Bastion, 'bastion'.
        $ $PROGRAM_NAME -b bastion -h backend

        # connect the Triton machine named 'backend' with additional 
        # ssh options '-M -v'
        $ $PROGRAM_NAME -b bastion -h backend ::: -M -v

        # run uptime(1) in the Triton machine named 'frontend'
        $ $PROGRAM_NAME -h frontend :::: -- uptime
        
        # run uptime(1) in the Triton machine named 'frontend' with 
        # additional ssh option '-v'
        $ $PROGRAM_NAME -h frontend ::: -v -- uptime

EOF
    exit 1
fi

script=$(triton-pssh -1 "$@")
if [[ "$DEBUG" != "" && "$DEBUG" -gt 0 ]]; then
    echo "DEBUG: $script" 1>&2
fi
eval "$script"
