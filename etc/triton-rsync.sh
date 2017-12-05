#!/bin/bash

PROGRAM_NAME=$(basename "$0")

if [ "$#" -lt 4 ]; then
    cat <<EOF
Usage: $PROGRAM_NAME -h HOST ::: [RSYNC-OPTION...] FILE... DEST
Usage: $PROGRAM_NAME FILTER-EXPRESSION ::: [RSYNC-OPTION...] FILE... DEST

FILTER-EXPRESSION is explained in http://github.com/cinsk/triton-pssh/README.md

DEST must have the form "{}:[DIRECTORY]".  $PROGRAM_NAME will replace "{}" to
the hostname obtained from FILTER-EXPRESSION.

EOF
    exit 1
fi

script=$(triton-pssh -3 "$@")
if [[ "$DEBUG" != "" && "$DEBUG" -gt 0 ]]; then
    echo "DEBUG: $script" 1>&2
fi
eval "$script"
