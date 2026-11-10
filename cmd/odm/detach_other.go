//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// startDetached launches exe in its own session so it outlives us.
func startDetached(exe string, _ bool, args ...string) error {
	c := exec.Command(exe, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := c.Start(); err != nil {
		return err
	}
	return c.Process.Release()
}
