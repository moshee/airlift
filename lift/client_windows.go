package main

// #cgo LDFLAGS: -lcrypt32
// #include "client_windows.h"
import "C"
import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func init() {
	appdata := os.Getenv("LOCALAPPDATA")
	if appdata == "" {
		log.Fatal("Couldn't locate current user's AppData\\Local")
	}
	dotfilePath = filepath.Join(appdata, "airlift", "airlift_config")
}

type Config struct {
	Scheme string
	Host   string
	Port   string
	Pass   []byte
}

const (
	ENABLE_ECHO_INPUT        = 0x0004
	PasswordStorageMechanism = "the configuration file, encrypted with your user info"
)

var SetConsoleMode = syscall.MustLoadDLL("Kernel32.dll").MustFindProc("SetConsoleMode")

func toggleEcho(t bool) error {
	stdin, err := syscall.GetStdHandle(syscall.STD_INPUT_HANDLE)
	if err != nil {
		return err
	}
	var mode uint32
	err = syscall.GetConsoleMode(stdin, &mode)
	if err != nil {
		return err
	}

	if t {
		mode |= ENABLE_ECHO_INPUT
	} else {
		mode &^= ENABLE_ECHO_INPUT
	}
	/*r1, r2, err := */ SetConsoleMode.Call(uintptr(stdin), uintptr(mode))
	//return err
	return nil
}

func updatePassword(conf *Config, newPass string) error {
	var (
		p unsafe.Pointer
		n C.int
	)
	err := C.CryptPassword(C.CString(newPass), C.int(len(newPass)), &p, &n, 1)
	if err != nil {
		return errors.New(C.GoString(err))
	}

	conf.Pass = C.GoBytes(p, n)
	return writeConfig(conf)
}

func getPassword(conf *Config) (string, error) {
	if len(conf.Pass) == 0 {
		return "", errPassNotFound
	}
	var (
		p unsafe.Pointer
		n C.int
	)

	// no, conf.Pass isn't really a string.
	msg := C.CryptPassword(C.CString(string(conf.Pass)), C.int(len(conf.Pass)), &p, &n, 0)
	if msg != nil {
		return "", errors.New(C.GoString(msg))
	}

	// n-1 here to strip off the null byte
	return string(C.GoBytes(p, n-1)), nil
}

func copyString(s string) error {
	err := C.GoString(C.CopyString(C.size_t(len(s)), C.CString(s)))
	if err == "" {
		return nil
	}
	return errors.New(err)
}

func getTermWidth() int {
	var c C.CONSOLE_SCREEN_BUFFER_INFO
	C.GetTermInfo(&c)
	w := int(c.dwSize.X)

	// if we're not actually on a windows console, GetConsoleScreenBufferInfo
	// will fail and the width will be 0. In that case, just pick 80 since
	// there's no way I can think of to alternatively find the width without
	// shelling out.
	if w == 0 {
		w = 80
	}
	return w
}

func termClearLine() {
	// clearing line for cygwin
	os.Stderr.WriteString("\033[J")
	// this will remove the junk characters above in the windows console (noop
	// for cygwin)
	C.ClearLine()
}

func termReturn0() {
	// move down (reset cursor to x = 0) and then up for cygwin
	os.Stderr.WriteString("\n\033[A")
	C.ClearLine() // remove junk
	C.MoveUp()    // move up for windows console (noop for cygwin)
}
