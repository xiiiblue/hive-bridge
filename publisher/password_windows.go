package publisher

import (
	"os"
	"syscall"
)

func hideEcho() (func(), error) {
	h := syscall.Handle(os.Stdin.Fd())
	var old uint32
	if err := syscall.GetConsoleMode(h, &old); err != nil {
		return nil, err
	}
	set := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
	ok, _, err := set.Call(uintptr(h), uintptr(old&^4))
	if ok == 0 {
		return nil, err
	}
	return func() { set.Call(uintptr(h), uintptr(old)) }, nil
}
