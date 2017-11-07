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

You'll see the output that `uptime` successful on the machine where the id is `6cc24b15-1df7-4492-f893-92fe3aff91b0`. 

TODO: complete the documentation
