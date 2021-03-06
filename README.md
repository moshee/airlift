# Airlift :package:

[![Build Status](https://travis-ci.org/moshee/airlift.svg?branch=master)](https://travis-ci.org/moshee/airlift)

Airlift is a self-hosted file upload and sharing service. The clients upload
files to the server and return a nice link for you to share. Just bring your
own server and domain.

:bomb: This is unstable software. It is not feature-complete and has lots of bugs.

You should use [deuiore/load.link](https://github.com/deuiore/load.link)
instead of this if...

- ...you like PHP;
- ...you don't like me;
- ...you're on a free/cheap shared host that doesn't allow long-running
  processes.

#### Clients

- Web interface (included in server)
- Cross-platform CLI (included in `cmd/lift`)
- [OS X][osx]

[osx]: https://github.com/moshee/AirliftOSX

# airliftd

`airliftd` is the Airlift server. You drop the server on any dedicated, VPS,
shared host, whatever, as long as it supports running a binary and gives you
access to ports or frontend server reverse proxying. A client sends files to it
and recieves nice URLs to share. The server itself also provides a web-based
client to upload files from, as well as manage existing uploads and customize
some behaviors.

The server is packaged as a statically compiled binary with a few text assets
with no system dependencies apart from maybe libc for networking. Just download
(or clone and build), add to your init system of choice, and run.

You can choose to run it behind a frontend server or standalone. 

## Installing

### If you just want a binary

Download a release from [the GitHub Releases tab][1]. Put the included binary
wherever you want in your `$PATH`.

[1]: https://github.com/moshee/airlift/releases

### Or if you want to build it yourself

1. [Install Go](http://golang.org/doc/install) and git
2. If you don't already have a [`GOPATH`][2]: `$ mkdir ~/go && export GOPATH=~/go` (you can use any place as your `GOPATH`)
3. `$ go get ktkr.us/pkg/airlift/cmd/airliftd`

[2]: https://github.com/golang/go/wiki/GOPATH

I haven't tried to build or run it on Windows, YMMV. Works on macOS and
GNU+Linux.

## Updating

1. Replace binary with new one.
2. There is no step 2.

## Usage

To run server:

```
$ airliftd
```

In development, you can pass the flag `-rsrc .` to instruct it to load files from disk
rooted in the working directory.

The server runs in the console. You can use whatever tools you want to
background it.

### Command line options

```
Usage of airliftd:
  -debug
        Enable debug/pprof server
  -p int
        Override port in config (default -1)
  -rsrc DIR
        Look for static and template resources in DIR (empty = use embedded resources)
  -v    Show version and exit
```

### Sample nginx config

```nginx
server {
	listen 80;
	server_name i.example.com;
	location / {
		proxy_pass http://localhost:60606;

		# tell the proxied server its own host (not localhost)
		proxy_set_header Host $http_host;

		# tell the proxied server the remote host (not localhost either)
		proxy_set_header X-Forwarded-For $remote_addr;
	}
}
```

### Configuration settings

When you start the server for the first time, it will generate a dotfolder in
your home directory for local configuration. Visit
`http(s)://<yourhost>/config` to set up a password and change other
configuration parameters. On the first setup, an empty password will not be
accepted.

If you manually edit the config file while the server is running, you should
send the server process a SIGHUP to force a config reload.

If the server fails to start with a config error, you probably want to delete
`~/.airlift-server/config` and reconfigure from scratch.

**Base URL** []: The base URL that links will be returned on. This includes domain
and path.

If you are proxying the server behind a frontend at a certain subdirectory,
make sure you rewrite the leading path out of the request URL so that the URLs
sent to `airlift` are rooted. Unfortunately, since URLs are rewritten, the
redirecting behavior of /-/login and /-/config won't work properly, so you'll have
to do your configuration on the internal port (60606 or whatever). Could use a
meta redirect instead of internal redirect to fix this, but that doesn't play
well with how sessions and stuff are set up in here.

Leaving the host field empty will cause the server to return whatever host the
file was posted to.

**Length of File ID** [4]: Number of characters in subsequently generated file
IDs. In general, more characters gives better collision characteristics (and
are harder to guess).

**Append File Extensions** [off]: If enabled, links generated by the upload
tool will end with the original file's extension, e.g.
`i.example.com/f9gW.zip` instead of `i.example.com/f9gW`.

**Limit Upload Age** [off]: Enable this to automatically limit the maximum age
of uploads by periodically pruning old uploads.

**Max Age** [0]: If **Limit Upload Age** is on, uploads older than this number
of days will be automatically deleted.

**Limit Total Uploads Size** [off]: Enable this to automatically limit the size
of the uploads folder on disk.

**Max Size** [0]: If **Limit Total Uploads Size** is on, the oldest uploads
will be pruned on every new upload until the total size is less than this many
megabytes.

**Enable Twitter Cards** [off]: If enabled, image uploads (which can be
thumbnailed) will provide a Twitter Card preview when their URLs are
mentioned in Tweets. This is achieved by serving an alternate page with
relevant metadata for the file when the User-Agent of the visitor includes
Twitterbot.

**Twitter Handle** []: Twitter Cards require that the Twitter handle of the
source's creator is included in the metadata.

**Syntax Highlighting** [off]: Enable to serve text-based files with syntax
highlighting. The raw file can be requested by appending `?raw=1` to the URL.

**Syntax Theme** []: Set the syntax highlighting color scheme.

**Upload Directory** [~/.airlift-server/uploads]: This is where uploaded files
will be stored.

**New Password** []: Change your password here.

**Confirm New Password** []: Enter the new password again to confirm.

### HTTPS

In order to use SSL/TLS standalone, set the following environment variables:

 Variable       | Value
----------------|---------------------------------------------
 `GAS_TLS_PORT` | The port for the secure server to listen on
 `GAS_TLS_CERT` | The path to your certificate
 `GAS_TLS_KEY`  | The path to your key
 `GAS_PORT`     | *Optional:* set this to -1 if you **only** want HTTPS, not regular HTTP.

If both HTTP and HTTPS are enabled, they will both serve from the same
executable and HTTP requests will redirect to HTTPS.

## Development

- After making modifications to static assets, use `go generate` in `cmd/airliftd`
  to create the source files for them
- After tagging a release, use `cmd/airlift/gen_version.bash` to create the
  source file with the tagged version
- Build with `go build`

# lift

`lift` is a CLI client interface to `airliftd`. It takes a filename as an
argument and uploads the server at the configured host, which is stored as a
JSON file in an OS-dependent location (`~/.airlift` on POSIX,
`%LOCALAPPDATA%\airlift\airlift_config` on Windows). These may also be
configured by the client.

If the server requires a password, the client will prompt for it and it will be
saved in a secure system-dependent fashion:

- **OS X**: Keychain
- **Windows**: encrypted in conf file using current user info
- **Linux**: I'm not really sure so I just used Twofish

### Installing

Binaries will be made available for common platforms. To build it yourself,

1. [Install Go](http://golang.org/doc/install)
2. Assuming `GOPATH` is set up, `$ go install` should do it if you have a sane
   build environment. This client uses cgo, so there may be some
   platform-specific issues to take into consideration.

#### Windows

If on Windows, set `CC` to the name of your MinGW32 compiler if needed. If the
linker complains, you will need to add the location of crypt32.lib (or
libcrypt32.a) to the linker path.

#### Cygwin

Since Go doesn't officially support Cygwin, you have to use MinGW32 to compile.
You don't have to *install* MinGW32, though, just get the MinGW32 gcc suite for
your architecture from the Cygwin installer and compile with either

```
$ CC=x86_64-w64-mingw32-gcc go build
```

for 64-bit, or whatever the equivalent for 32-bit is.

Note that since the Windows versions of the Go packages use all Windows APIs, it
won't understand anything Cygwin-specific such as symbolic links and the like.

### Usage

When you use it for the first time, you'll need to set up a host. The following
are equivalent:

```
$ lift -h i.example.com -p 80
$ lift -a http://i.example.com
$ lift -a http://i.example.com:80
```

If the server requires a password, it will be prompted for:

```
$ lift "today's lunch.jpg"
Server returned error: password required
You'll need a new password. If the request is successful,
it will be saved in the OS X Keychain.
Password:
[▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉▉]
http://i.example.com/dGp9
(Copied to clipboard)
```
