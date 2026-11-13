//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

const (
	detachedProcess        = 0x00000008
	createNewProcGroup     = 0x00000200
	createBreakawayFromJob = 0x01000000
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	freeConsole           = kernel32.NewProc("FreeConsole")
)

// dropOwnConsole closes the console window Windows created just for us. odm.exe
// is a console program, so the Run-key autostart (`odm.exe tray --background`)
// gets a cmd window; closing it would kill the tray. A console shared with a
// shell (`odm tray` typed in a terminal) is left alone so its output stays.
func dropOwnConsole() {
	var pids [2]uint32
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), 2)
	if n == 1 {
		freeConsole.Call()
	}
}

// startDetached launches exe so that it outlives us. It asks to break away from
// our job object: browsers run native-messaging hosts inside a job that kills
// everything when the host exits, which would take the tray down with it. Some
// jobs forbid breakaway, so fall back to a plain detached start.
func startDetached(exe string, hideWindow bool, args ...string) error {
	try := func(flags uint32) error {
		c := exec.Command(exe, args...)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: hideWindow, CreationFlags: flags}
		if err := c.Start(); err != nil {
			return err
		}
		return c.Process.Release()
	}
	const base = detachedProcess | createNewProcGroup
	if err := try(base | createBreakawayFromJob); err == nil {
		return nil
	}
	return try(base)
}
