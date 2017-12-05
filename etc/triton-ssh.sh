#!/bin/bash

PROGRAM_NAME=$(basename "$0")

if [ "$#" -eq 0 ]; then
    cat <<EOF
Usage: $PROGRAM_NAME -h HOST [::: SSH-OPTION...]
       $PROGRAM_NAME FILTER-EXPRESSION [::: SSH-OPTION...]

FILTER-EXPRESSION is explained in http://github.com/cinsk/triton-pssh/README.md

EOF
    exit 1
fi

script=$(triton-pssh -1 "$@")
if [[ "$DEBUG" != "" && "$DEBUG" -gt 0 ]]; then
    echo "DEBUG: $script" 1>&2
fi
eval "$script"
