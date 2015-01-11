# lift

`lift` is a CLI client interface to `airlift-server`. It takes a filename as an
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
