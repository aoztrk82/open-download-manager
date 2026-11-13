//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

// revealInFolder opens Explorer with the file selected (or the folder, if the
// file is gone).
func revealInFolder(path string) error {
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		dir := filepath.Dir(path)
		if _, derr := os.Stat(dir); derr != nil {
			return derr
		}
		return exec.Command("explorer.exe", dir).Start()
	}
	// Go's argv quoting mangles "/select,<path with spaces>", so pass the raw
	// command line the way explorer.exe expects it.
	c := exec.Command("explorer.exe")
	c.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	return c.Start()
}

// openWithDefault opens a finished download with its default program, as a
// double-click in Explorer would.
func openWithDefault(path string) error {
	path = filepath.Clean(path)
	if _, err := os.Stat(path); err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("open")
	file, _ := windows.UTF16PtrFromString(path)
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(path))
	return windows.ShellExecute(0, verb, file, nil, dir, windows.SW_SHOWNORMAL)
}
