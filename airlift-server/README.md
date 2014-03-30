# airlift-server

`airlift-server(1)` is the Airlift server. You drop the server on any
dedicated, VPS, shared host, whatever, as long as it supports running a binary
and gives you access to ports or frontend server reverse proxying. A client
sends files to it and recieves nice URLs to share.

The server is packaged as a statically compiled binary with a few text assets
with no system dependencies apart from maybe libc for networking. Just download
(or clone and build), add to your init system of choice, and run.

You can choose to run it behind a frontend server or standalone. 

### Installing

#### If you just want a binary

       | linux      | darwin
-------|------------|------------
 386   | [~2.5M][1] | —
 amd64 | [~2.5M][2] | [~2.4M][3]

[1]: http://static.displaynone.us/airlift-server/airlift-server-linux_386.tar.bz2
[2]: http://static.displaynone.us/airlift-server/airlift-server-linux_amd64.tar.bz2
[3]: http://static.displaynone.us/airlift-server/airlift-server-darwin_amd64.tar.bz2

I can add more platforms if anyone wants. I'll try to keep them current.

#### Or if you want to build it yourself

Before you build, if you have not already:

1. [Install Go](http://golang.org/doc/install), git, and mercurial (if you
   haven't already)
2. `$ mkdir ~/go && export GOPATH=~/go` (you can use any place as your GOPATH)

Then,

```
$ go get -u github.com/moshee/airlift/airlift-server
```

I haven't tried to build or run it on Windows, YMMV. Works on OS X and
GNU+Linux.

`go get` should clone this repo along with any dependencies and place it
in your `$GOPATH`, which should be set (see [here][GOPATH] for more info).
This can be anywhere that isn't `$GOROOT`; you can set it to any arbitrary
place like `~/go`. Assuming that's what it is, then

```
~/go/src/github.com/moshee/airlift-server$ go build
~/go/src/github.com/moshee/airlift-server$ ./airlift-server
```

to build and run.

[GOPATH]: https://code.google.com/p/go-wiki/wiki/GOPATH

The binary produced by `go build` will be in your working
directory at the moment you built it. By default, `go get` will install the
binary to `$GOPATH/bin` after building. It isn't very useful there, because...

### Usage

...the server must be run with the `templates` subdirectory in its working
directory. Whatever else you do is up to you. Just `$ ./airlift-server` to run
it in your terminal. Use your favorite tools to background it.

When you start the server for the first time, it will generate a dotfolder in
your home directory for local configuration. Visit
`http(s)://<yourhost>/config` to set up a password and change other
configuration parameters. On the first setup, an empty password will not be
accepted.

**Host** []: The base URL that links will be returned on. This includes domain
and path.

If you are proxying the server behind a frontend at a certain subdirectory,
make sure you rewrite the leading path out of the request URL so that the URLs
sent to `airlift-server` are rooted. Unfortunately, since URLs are rewritten,
the redirecting behavior of /login and /config won't work properly, so you'll
have to do your configuration on the internal port (60606 or whatever). Could
use a meta redirect instead of internal redirect to fix this, but that doesn't
play well with how sessions and stuff are set up in here.

Leaving the host field empty will cause the server to return whatever host the
file was posted to.

**Port** [60606]: This is the port the server executable listens on.

The `-p` flag overrides the configured port.

If you are using e.g. nginx, you can just add a
`proxy_pass http://localhost:60606;` directive inside a server block for the
host you choose.

**Directory** [~/.airlift-server/uploads]: This is where uploaded files will be
stored.

**Max upload age** [0]: If this value is greater than 0, uploads older than
that many days will be automatically deleted.

**Max upload size** [0]: If this value is greater than 0, the oldest uploads
will be pruned on every new upload until the total size is less than that many
megabytes.

You may have to restart the server after modifying the configuration.

If the server fails to start with a config error, you probably want to delete
`~/.airlift-server/config` and reconfigure from scratch.

#### HTTPS

In order to use SSL/TLS standalone, set the following environment variables:

 Variable       | Value
----------------|---------------------------------------------
 `GAS_TLS_PORT` | The port for the secure server to listen on
 `GAS_TLS_CERT` | The path to your certificate
 `GAS_TLS_KEY`  | The path to your key
 `GAS_PORT`     | *Optional:* set this to -1 if you **only** want HTTPS, not regular HTTP.

If both HTTP and HTTPS are enabled, they will both serve from the same
executable and HTTP requests will redirect to HTTPS.
