//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detach makes the child outlive the CLI, with no console window.
func detach(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008 | 0x00000200, // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	}
}
