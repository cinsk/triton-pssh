# triton-pssh

`triton-pssh` is a program executing ssh in parallel on Triton machine instances.  It provides features such as sending input to all of the processes, saving output to files, and timing out.


# installation

`triton-pssh` depends on [Triton client tool](https://github.com/joyent/node-triton).  Install it first.   You may also need to configure a Triton profile with it if you haven't used this tool before.

Make sure that you have [Go](https://golang.org) language environment. Then,

        $ go get github.com/cinsk/triton-pssh

You can find the binary in `$GOPATH/bin` directory.  You may copy the binary to someplace under `$PATH`, or add `$GOPATH/bin` in your `$PATH`.

# Usage

For the tutorial session, here is the list of machines in my Triton environment:

        $ triton instance ls
        SHORTID   NAME       IMG                STATE    FLAGS  AGE
        6cc24b15  gong       centos-7@20170327  running  K      9w
        f1099abb  varnish    a4bebde7           running  DF     9w
        19a154e6  smartos    base-64@17.2.0     running  -      8w
        8c824e06  zk3        centos-7@20161213  running  -      6w
        f1251ff1  zk2        centos-7@20161213  running  -      6w
        aadf93e3  zk1        centos-7@20161213  running  -      6w
        70db2402  bastion    centos-7@20161213  running  -      6w
        7d670f65  kafka3     centos-7@20161213  running  -      6w
        2981f890  kafka2     centos-7@20161213  running  -      6w
        af359c18  kafka1     centos-7@20161213  running  -      6w
        94dd157d  kvmcentos  centos-7@20170327  running  K      5w
        d3bff1f5  nexus      centos-7@20170327  running  K      5w
        ed41e692  nexus2     centos-7@20161213  running  -      2w

`triton-pssh` need to select one or more hosts for the command.  The basic command line looks like this:

        $ triton-pssh [HOST-SELECTING-EXPRESSION] ::: COMMAND...

First, select a machine named `gong`, and let's run `uptime` on it:

        $ triton-pssh 'name == "gong"' ::: uptime
        [1] 15:38:21 [SUCCESS] 6cc24b15-1df7-4492-f893-92fe3aff91b0 root@gong

You'll see the output that `uptime` successful on the machine where the id is `6cc24b15-1df7-4492-f893-92fe3aff91b0`.  However, it only shows that the command `uptime` was successful, without the actual output of the command.   Currently, there are two ways to get the output of the command.  With `-i` option, all standard output will be aggregated and report back to you:

        $ triton-pssh -i 'name == "gong"' ::: uptime
        [1] 18:48:47 [SUCCESS] 6cc24b15-1df7-4492-f893-92fe3aff91b0 root@gong
         02:48:47 up 69 days,  3:11,  3 users,  load average: 0.00, 0.01, 0.05

Let's add one more host by updating the expression:

        $ triton-pssh -i 'name == "gong" || name == "nexus"' ::: uptime
        [1] 18:49:58 [SUCCESS] 6cc24b15-1df7-4492-f893-92fe3aff91b0 root@gong
         02:49:58 up 69 days,  3:12,  3 users,  load average: 0.00, 0.01, 0.05
        [2] 18:49:59 [SUCCESS] d3bff1f5-239c-cf52-9468-a4db4e630b2e root@nexus
         02:49:59 up 40 days,  4:30,  0 users,  load average: 0.00, 0.01, 0.05

With `-i` option, you can easily see the output of the command in multiple hosts.  However, this may be not convenient if the output of the command is getting bigger, nor it will show you the ourput of standard error.   `triton-pssh` provides alternative way to save standard output and standard error in a seperate file per host.   To use this feature, you should provide `-o OUTDIR` or `-e ERRDIR` options.  If *OUTDIR* or *ERRDIR* are not exist, `triton-pssh` will create them for you.

        $ triton-pssh -o stdout 'name == "gong" || name == "nexus"' ::: uptime
        [1] 18:54:25 [SUCCESS] 6cc24b15-1df7-4492-f893-92fe3aff91b0 root@gong
        [2] 18:54:25 [SUCCESS] d3bff1f5-239c-cf52-9468-a4db4e630b2e root@nexus
        $ ls stdout/
        root@6cc24b15-1df7-4492-f893-92fe3aff91b0	root@d3bff1f5-239c-cf52-9468-a4db4e630b2e
        $ cat stdout/root@6cc24b15-1df7-4492-f893-92fe3aff91b0
         02:54:25 up 69 days,  3:17,  3 users,  load average: 0.00, 0.01, 0.05
        $ cat stdout/root@d3bff1f5-239c-cf52-9468-a4db4e630b2e 
         02:54:25 up 40 days,  4:34,  0 users,  load average: 0.08, 0.03, 0.05
        $ rm -rf stdout

Of course, by using `-e ERRDIR`, you can save standard error output of the command, too.

Another feature of `triton-pssh` is, it can send its standard input to all Triton machine instances. You can use this feature to execute very large script, or transfer a file from your local machine to multiple Triton machine instances.

        $ # Executing large-bash-script.sh in multiple machines
        $ cat large-bash-script.sh | triton-pssh 'name == "bastion" || name == "gong"' ::: bash -s

        $ # Copying source-file to destination-file in multiple machines
        $ cat source-file | triton-pssh 'name == "bastion" || name == "gong"' ::: 'cat >destination-file'

Occasionally, if the SSH session takes long, you'd see an error like this:

        $ cat a-large-file | triton-pssh -i 'name == "bastion"' ::: 'cat > destiation'
        [1] 21:03:36 [FAILURE] 70db2402-ad4b-6297-a8ea-f84b361160d4 root@bastion wait: remote command exited without exit status or exit signal

This usually means, that the SSH session timed out. A quick solution is to give longer timeout using `-d TIMEOUT` option:

        $ cat a-large-file | triton-pssh -t 30 -i 'name == "bastion"' ::: 'cat > destiation'
        [1] 21:03:36 [SUCCESS] 70db2402-ad4b-6297-a8ea-f84b361160d4 root@bastion





## Expressions

`triton-pssh` uses [govaluate](https://github.com/Knetic/govaluate) to parse and to evaluate the expression.  Most simple C-like expressions are supported.  Check [govaluate manual](https://github.com/Knetic/govaluate/blob/master/MANUAL.md) for details.

Here are `triton-pssh` specific parameters:
   
* id -- Triton instance ID
* name -- Triton instance name
* type -- Triton instance type
* brand -- Triton zone brand
* state -- instance state
* memory -- memory size (unit: GB)
* disk -- disk size
* ips -- an array of IP addresses in string
* primaryIp -- primary IP address of this machine
* package -- Triton package name of this instance

Parameters below are subjected to change due to the internal expression evalator implementation:

* tags -- a set of tags (key: string, value: string) 
* image -- Triton image ID of this instance 
* image_id -- Triton image ID of this instance
* image_name -- Triton image name of this instance
* image_version -- Triton image version of this instance
* image_os -- Triton image OS of this instance
* image_type -- Triton image type of this instance
* image_public -- boolean value whether this image is public
* image_state -- Triton image state
* image_tags -- Tags associated with the image
* image_owner -- Triton image owner of this image
* image_published_at -- date of Triton image published

* networks -- an array of network IDs in string 
* has_public_net -- boolean that indicates whether this instance has public IP address

Note that running `triton instance get` will give you the complete list of parameters.

If the value of the parameter is simple string or number, you could use something like this as an expression

        memory == 40
        disk > 150
        name == "foo"
        
Regular expression comparisons are also supported by `=~` and `!~` operators:

        name =~ "kafka.*"         # matched to instances with name starting with kafka
        
Unformatunately, there is no simple support for a map or an array.  You should use function `contains`:

        contains(ARRAY, ELEM)                 # true if ARRAY contains ELEM
        contains(MAP, KEY)                    # true if MAP contains a pair with KEY
        contains(MAP, KEY1, VAL1, ...)        # true if MAP contains KEY1=VAL1, ...

For example, if you want to find instances with a tag `role=zookeeper`, you could use:

        $ triton-pssh 'contains(tags, "role", "zookeeper")' ::: command...

If you want to find instances with specific network id, you could do:

        $ triton-pssh 'contains(networks, "1234-1234-1234-1234")' ::: command...

To find any instance without public IP address:

        $ triton-pssh '!has_public_net' ::: command...

To find any instance that uses Joyent SmartOS or LX brand:

        $ triton-pssh 'brand == "joyent" || brand == "lx"' ::: command...

### Experimental shortcut

Note that any feature in this section may be removed if the implementation of the expression evaluator changes.

If the expression is just one word, not a string, and neither  `true` or `false`, and an identifier which is not a parameter name above, then it will considered as a name of the instance.   For example, following two command lines are identical:

        $ triton-pssh --no-cache 'name == "foo"' ::: uptime
        $ triton-pssh --no-cache name ::: uptime



## Authentication

`triton-pssh` will use the ssh-agent if the environment variable `SSH_AUTH_SOCK` exists.  Also, it will read your private key for the PublicKey authentication if `$HOME/.ssh/id_rsa` exists.

Use `-i KEYFILE` to provide additional private key file for public key authentication.

Use `--password` to use password authentication.  However, you cannot provide extra input through a pipe to `triton-pssh` in this case.

## User name

`triton-pssh` will automatically determine the user of the remote host by looking at the Triton image of the instance.  For example, if the instance uses certain Ubuntu machine, it will use *ubuntu* as the user name.   If you want to override the user name of all instances, use `-u` option to override it.  In this case, default user name in the Triton image will be ignored.

If you did not specify `-u` option, and if `triton-pssh` cannot determine the user name from Triton image API, it will use *root* by default.  You can override the default user name via `--default-user=USER` option.  Note that this value only works if querying to Triton image API failed.


## File Cache

`triton-pssh` will cache all information acquired from [Triton Cloud API](https://apidocs.joyent.com/cloudapi/) for certain period.  The location of the cache is `$HOME/.triton-pssh/cache`.   If you do not want to use cached information, add `--no-cache` option.  This is epecially helpful, if you remove an instance and create another one with the same name.  

Note that `--no-cache` option will instruct `triton-pssh` to query Triton Cloud API and update the cache, but unaffected cached entries will still stay.    If there is a corrupted cache file in that directory, try to remove the directory, `$HOME/.triton-pssh/cache` manually.

By default, `triton-pssh` will cache instance information for one day, and will cache network/image information for one week.


# Bastion server

Thanks to [Golang SSH package](https://godoc.org/golang.org/x/crypto/ssh), `triton-pssh` does not require a proxy utility in a Bastion server.  Any server that has appropriate network will serve.   For example,

        $ triton instance create -n bastion -N Joyent-SDC-Public -N Joyent-SDC-Private -N My-Fabric-Network base-64 g4-highcpu-1G

Or, you could use the Terraform to create a Bastion server.  I also created a module for that.  See [terraform-triton-bastion](https://github.com/cinsk/terraform-triton-bastion) for more.

# Limitation

Unlike `pssh`, `triton-pssh` does not depend on ssh(1) client but uses [Golang SSH package](https://godoc.org/golang.org/x/crypto/ssh).   Some of the features like *ControlMaster* in ssh(1) may not available.

# Utility: triton-ssh.sh

FYI, `triton` command line tool gives you to connect a Triton instance via

        $ triton ssh INSTANCE-NAME

Currently, it only allows you to connect a Triton instance if and only if the instance has a public IP address.

This package provides `triton-ssh.sh` which allows you to use ssh(1) client to connect a Triton instance interactively.

You can use the same expression syntax that you'd use with `triton-pssh` in `triton-ssh.sh`:

        $ triton-ssh.sh my-instance      # connect the instance with the name, "my-instance"
        $ triton-ssh.sh -b bastion 'name == "foo"'  # connect the instance, "foo" through the Bastion, "bastion"

If the expression matches to more than one instance, `triton-ssh.sh` will just connect the first machine it matches.




