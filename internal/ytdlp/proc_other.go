//go:build !windows

package ytdlp

import (
	"os/exec"
	"syscall"
)

// prepare puts yt-dlp in its own process group so cancelling also stops the
// ffmpeg it started.
func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func start(cmd *exec.Cmd) (func(), error) { return func() {}, cmd.Start() }
