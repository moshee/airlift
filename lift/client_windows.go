package main

/*
#cgo LDFLAGS: -lcrypt32
#include "Windows.h"
#include "Wincrypt.h"
#include <stdio.h>
#include <string.h>

char* geterror(const char* sender) {
	DWORD code = GetLastError();
	char* msg;
	DWORD n = FormatMessage(
		FORMAT_MESSAGE_ALLOCATE_BUFFER|FORMAT_MESSAGE_FROM_SYSTEM|FORMAT_MESSAGE_IGNORE_INSERTS,
		NULL, code, 0, (LPSTR)&msg, 0, NULL);

	char* buf = (char*)malloc((size_t)n + strlen(sender) + 3);
	sprintf(buf, "%s: %s", sender, msg);
	return buf;
}

int GetTermWidth() {
	HANDLE hConsole = CreateConsoleScreenBuffer(
		GENERIC_READ|GENERIC_WRITE, 0, NULL, CONSOLE_TEXTMODE_BUFFER, NULL);
	CONSOLE_SCREEN_BUFFER_INFO info;
	BOOL ok = GetConsoleScreenBufferInfo(hConsole, &info);
	if (!ok) {
		return -1;
	}

	return (int)info.dwSize.X;
}

char* CopyString(size_t len, const char* str) {
	if (!OpenClipboard(NULL)) {
		return geterror("CopyString");
	}

	// overestimate number of wide chars as number of bytes
	size_t wlen = len*sizeof(wchar_t) + 1;

	// alloc and lock global memory
	HGLOBAL hMem = GlobalAlloc(GMEM_SHARE | GMEM_MOVEABLE, wlen);
	LPTSTR glob = (LPTSTR)GlobalLock(hMem);

	// copy our UTF-8 text into a wchar string stored in the global handle
	mbstowcs_s(NULL, glob, wlen, str, len);

	GlobalUnlock(hMem);

	if (!SetClipboardData(CF_UNICODETEXT, hMem)) {
		return geterror("SetClipboardData");
	}

	if (!CloseClipboard()) {
		return geterror("CloseClipboard");
	}

	return NULL;
}

char* CryptPassword(const char* str, int inSize, void** out, int* outSize, int encrypt) {
	DATA_BLOB input, output;
	input.cbData = (DWORD)inSize + 1;
	input.pbData = (BYTE*)str;

	BOOL ok;
	if (encrypt) {
		ok = CryptProtectData(&input, NULL, NULL, NULL, NULL, 0, &output);
		if (!ok) {
			return geterror("CryptProtectData");
		}
	} else {
		ok = CryptUnprotectData(&input, NULL, NULL, NULL, NULL, 0, &output);
		if (!ok) {
			return geterror("CryptUnprotectData");
		}
	}


	*out = output.pbData;
	*outSize = output.cbData;

	return NULL;
}
*/
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

func getTermWidth() int {
	return int(C.GetTermWidth())
}

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
