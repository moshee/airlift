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

Binaries for common platforms will be coming soon™. To build,

1. [Install Go](http://golang.org/doc/install)
2. `$ cd /path/to/airlift-server`
3. `$ go get github.com/moshee/gas`
4. `$ go build`

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
 Directory | ~/(you)/.airlift-server/uploads | This is where uploaded files will be stored.

You may have to restart the server after modifying the configuration.
