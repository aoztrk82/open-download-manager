//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const (
	detachedProcess        = 0x00000008
	createNewProcGroup     = 0x00000200
	createBreakawayFromJob = 0x01000000
)

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
