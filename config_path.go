package main

import (
	"errors"
	"os"
	"path/filepath"
)

// 已有配置即使读不出来也交给工作台报错，避免意外发布到另一份配置的环境。
func defaultConfig(dir string) string {
	current := filepath.Join(dir, "hivebridge.json")
	if _, err := os.Lstat(current); !errors.Is(err, os.ErrNotExist) {
		return current
	}
	previous := filepath.Join(dir, "hive-panel.json")
	if _, err := os.Lstat(previous); !errors.Is(err, os.ErrNotExist) {
		return previous
	}
	return current
}
