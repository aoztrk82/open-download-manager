//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func revealInFolder(path string) error {
	path = filepath.Clean(path)
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(path); err == nil {
			return exec.Command("open", "-R", path).Start()
		}
	}
	if _, err := os.Stat(path); err == nil {
		path = filepath.Dir(path)
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, path).Start()
}
