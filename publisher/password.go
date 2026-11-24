package publisher

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
)

// Restore terminal echo even when the user cancels login with Ctrl+C.
func password(u terminal) (string, error) {
	restore, err := hideEcho()
	if err != nil {
		return "", fmt.Errorf("请在终端或双击程序输入密码：%w", err)
	}
	var once sync.Once
	interrupts := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(interrupts, os.Interrupt)
	defer func() { once.Do(restore); signal.Stop(interrupts); close(done) }()
	go func() {
		select {
		case <-interrupts:
			once.Do(restore)
			os.Exit(130)
		case <-done:
		}
	}()
	return u.line("")
}
