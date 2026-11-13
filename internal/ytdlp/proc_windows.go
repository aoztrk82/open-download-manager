//go:build windows

package ytdlp

import (
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

func prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}

// start runs cmd inside a job object. yt-dlp.exe is a PyInstaller bundle: the
// exe starts a second yt-dlp.exe that does the work, and that one starts
// ffmpeg to merge. Killing only the first (what exec does on cancel) would
// leave the others downloading. Closing the job kills them all.
func start(cmd *exec.Cmd) (func(), error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return func() {}, cmd.Start()
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	_, _ = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	cmd.Cancel = func() error { return windows.TerminateJobObject(job, 1) }
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(job)
		return func() {}, err
	}
	// The bootloader unpacks for a moment before it starts its child, so the
	// child is created inside the job.
	if h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid)); err == nil {
		_ = windows.AssignProcessToJobObject(job, h)
		windows.CloseHandle(h)
	}
	return func() { windows.CloseHandle(job) }, nil
}
