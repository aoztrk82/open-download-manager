//go:build windows

package ffmpeg

import (
	"os/exec"
	"syscall"
)

// createNoWindow keeps Windows from opening a console window for ffmpeg when
// the parent (the tray daemon) has none.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}
