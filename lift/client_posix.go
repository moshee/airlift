// +build darwin freebsd linux netbsd openbsd

package main

/*
#include <unistd.h>
#include <termios.h>
#include <sys/ioctl.h>

void toggle_echo(int t) {
	struct termios tty;
	tcgetattr(STDIN_FILENO, &tty);
	if (t) {
		tty.c_lflag |= ECHO;
	} else {
		tty.c_lflag &= ~ECHO;
	}
	(void)tcsetattr(STDIN_FILENO, TCSANOW, &tty);
}

int get_term_width(void) {
	struct winsize ws;
	ioctl(STDOUT_FILENO, TIOCGWINSZ, &ws);
	return ws.ws_col;
}
*/
import "C"

import (
	"log"
	"os/user"
	"path/filepath"
)

func init() {
	u, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	dotfilePath = filepath.Join(u.HomeDir, ".airlift")
}

func toggleEcho(t bool) error {
	if t {
		C.toggle_echo(1)
	} else {
		C.toggle_echo(0)
	}
	return nil
}

func getTermWidth() int {
	return int(C.get_term_width())
}
