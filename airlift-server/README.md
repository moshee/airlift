# airlift-server

`airlift-server(1)` is the Airlift server. You drop the server on any
dedicated, VPS, shared host, whatever, as long as it supports running a binary
and gives you access to ports or frontend server reverse proxying. A client
sends files to it and recieves nice URLs to share.

The server is packaged as a statically compiled binary with a few text assets
with no system dependencies apart from maybe libc for networking. Just download
(or clone and build), add to your init system of choice, and run.

You can choose to run it behind a frontend server or standalone. Currently it
doesn't support SSL/TLS standalone, but it's planned.

### Installing

Pick a binary:

       | linux      | darwin
-------|------------|------------
 386   | [~9.5M][1] | —
 amd64 | [~12M][2]  | [~12M][3]

[1]: http://static.displaynone.us/airlift-server/linux/386/airlift-server
[2]: http://static.displaynone.us/airlift-server/linux/amd64/airlift-server
[3]: http://static.displaynone.us/airlift-server/darwin/amd64/airlift-server

I can add more platforms if anyone wants. I'll try to keep them current.

#### Building

Before you build, if you have not already:

1. [Install Go](http://golang.org/doc/install), git, and mercurial (if you
   haven't already)
2. `$ mkdir ~/go && export GOPATH=~/go` (you can use any place as your GOPATH)

Then,

1. `$ go get -u -d github.com/moshee/airlift/airlift-server`
2. `$ cd $GOPATH/src/github.com/moshee/airlift/airlift-server`
3. `$ go build`

`go get` should clone this repo along with any dependencies and place it
in your `$GOPATH`. The binary produced by `go build` will be in your working
directory at the moment you built it.

I haven't tried to build or run it on Windows, YMMV. Works on OS X and
GNU+Linux.

### Usage

The server must be run with the `templates` subdirectory in its working
directory. Whatever else you do is up to you. Just `$ ./airlift-server` to run
it in your terminal. Use your favorite tools to background it.

When you start the server for the first time, it will generate a dotfolder in
your home directory for local configuration. Visit
`http(s)://<yourhost>/config` to set up a password, or change the uploads
directory, listen port, and the hostname it uses for URLs. The default values
are:

 Field | Value     | Notes
-------|-----------|----------------------------------------------
 Host  | (empty)   | Leaving the host field empty will cause the server to return whatever host the file was posted to.
 Port  | 60606     | This is the port the server executable listens on. If you are using e.g. nginx, you can just add a `proxy_pass http://localhost:60606` directive inside a server block for the host you choose.
 Directory | ~/.airlift-server/uploads | This is where uploaded files will be stored.

You may have to restart the server after modifying the configuration.

If the server fails to start with a config error, you probably want to delete
`~/.airlift-server/config` and reconfigure from scratch.
