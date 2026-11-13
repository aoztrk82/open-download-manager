// Command odmw starts odm.exe without a console window, like pythonw.exe.
//
// odm.exe is a console program (the CLI needs that), so Windows gives it a cmd
// window whenever it is started from outside a terminal: the Run-key autostart,
// the Start menu, the installer's "start now". The tray drops that console at
// once, but it still flashes. odmw.exe is built with -H windowsgui, so it never
// gets a console, and starts odm.exe (next to it) with CREATE_NO_WINDOW.
// Arguments are passed through: `odmw.exe tray --background` for autostart.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const createNoWindow = 0x08000000

func main() {
	self, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	c := exec.Command(filepath.Join(filepath.Dir(self), "odm.exe"), os.Args[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if err := c.Start(); err != nil {
		os.Exit(1)
	}
	_ = c.Process.Release()
}
