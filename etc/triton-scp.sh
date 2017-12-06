#!/bin/bash

PROGRAM_NAME=$(basename "$0")

if [ "$#" -lt 4 ]; then
    cat <<EOF
Usage: $PROGRAM_NAME -h HOST ::: [SCP-OPTION...] FILE... DEST
Usage: $PROGRAM_NAME FILTER-EXPRESSION ::: [SCP-OPTION...] FILE... DEST

FILTER-EXPRESSION is explained in http://github.com/cinsk/triton-pssh/README.md

Unlike 'triton-pssh', this command requires nc(1) installed on the Bastion host
when '-b BASTION' supplied.

DEST must have the form "{}:[DIRECTORY]".  $PROGRAM_NAME will replace "{}" to
the hostname obtained from FILTER-EXPRESSION.

Examples:
        # copy file1, file2 and file3 to the user's home in the 
        # Triton machine, 'my-instance'
        $ $PROGRAM_NAME -h my-instance ::: file1 file2 file3 {}:

        # copy everything in dir/ recursively to the /tmp of the
        #  Triton machine, 'my-instance':
        $ $PROGRAM_NAME -h my-instance ::: -r dir {}:/tmp/

        # Same as above, except the machine, 'backend' through Bastion
        # server, 'bastion':
        $ $PROGRAM_NAME -b bastion -h backend ::: -r dir {}:/tmp/

        # Same as above, except this command will update the local
        # cache first:
        $ $PROGRAM_NAME --no-cache -b bastion -h backend ::: -r dir {}:/tmp/

        # Copy 'remote-file' from the machine, 'foo' to local:
        $ $PROGRAM_NAME -h foo ::: {}:remote-file .

EOF
    exit 1
fi

script=$(triton-pssh -2 "$@")
if [[ "$DEBUG" != "" && "$DEBUG" -gt 0 ]]; then
    echo "DEBUG: $script" 1>&2
fi
eval "$script"
