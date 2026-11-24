package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "hivebridge.json")
	previous := filepath.Join(dir, "hive-panel.json")
	check := func(want string) {
		t.Helper()
		if got := defaultConfig(dir); got != want {
			t.Fatalf("defaultConfig() = %q, want %q", got, want)
		}
	}
	check(current)
	if err := os.WriteFile(previous, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	check(previous)
	if err := os.WriteFile(current, []byte("invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	check(current)
}
