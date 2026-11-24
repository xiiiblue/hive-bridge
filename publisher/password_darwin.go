package publisher

import (
	"os"
	"syscall"
	"unsafe"
)

func hideEcho() (func(), error) {
	fd := os.Stdin.Fd()
	ioctl := func(cmd uintptr, t *syscall.Termios) error {
		_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, cmd, uintptr(unsafe.Pointer(t)))
		if e != 0 {
			return e
		}
		return nil
	}
	var old syscall.Termios
	if err := ioctl(syscall.TIOCGETA, &old); err != nil {
		return nil, err
	}
	next := old
	next.Lflag &^= syscall.ECHO
	if err := ioctl(syscall.TIOCSETA, &next); err != nil {
		return nil, err
	}
	return func() { _ = ioctl(syscall.TIOCSETA, &old) }, nil
}
