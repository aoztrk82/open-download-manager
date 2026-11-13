//go:build windows

package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"
)

// The installer must replace odm.exe and the uninstaller must remove it, and
// neither can while the tray or the window runs from it. `odm quit` asks both to
// exit cleanly (downloads are saved and resume on the next start) and waits.
// A named event rather than an API call: no new HTTP surface, and it reaches the
// window process too, which has no server.
const (
	quitEvent   = `Local\odm-quit`
	trayMutex   = `Local\odm-tray`
	windowMutex = "odm-window" // created in runWindow (session-local by default)
)

// quitSignal returns a channel closed when `odm quit` runs. Manual-reset event:
// it stays set, so every process waiting on it (tray and window) sees it.
func quitSignal() <-chan struct{} {
	ch := make(chan struct{})
	name, _ := windows.UTF16PtrFromString(quitEvent)
	ev, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		return ch // never fires; quitting still works from the tray menu
	}
	go func() {
		windows.WaitForSingleObject(ev, windows.INFINITE)
		close(ch)
	}()
	return ch
}

// resetQuit clears a quit request left over from an earlier `odm quit`, so a
// tray started right after an update is not told to exit at once.
func resetQuit() {
	name, _ := windows.UTF16PtrFromString(quitEvent)
	if ev, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name); err == nil {
		_ = windows.ResetEvent(ev)
		windows.CloseHandle(ev)
	}
}

// holdMutex marks this process as alive for `odm quit` to wait on; the handle
// is released by Windows when the process exits.
func holdMutex(name string) {
	p, _ := windows.UTF16PtrFromString(name)
	_, _ = windows.CreateMutex(nil, false, p)
}

func mutexExists(name string) bool {
	p, _ := windows.UTF16PtrFromString(name)
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, p)
	if err != nil {
		return false
	}
	windows.CloseHandle(h)
	return true
}

func quitCmd() *cobra.Command {
	var wait time.Duration
	c := &cobra.Command{
		Use:    "quit",
		Hidden: true,
		Short:  "Stop the running tray app and window (used by the installer)",
		RunE: func(*cobra.Command, []string) error {
			if !mutexExists(trayMutex) && !mutexExists(windowMutex) && !mutexExists(progressMutex) {
				return nil // nothing running
			}
			name, _ := windows.UTF16PtrFromString(quitEvent)
			ev, err := windows.CreateEvent(nil, 1, 0, name)
			if err != nil {
				return fmt.Errorf("quit event: %w", err)
			}
			defer windows.CloseHandle(ev)
			if err := windows.SetEvent(ev); err != nil {
				return fmt.Errorf("quit event: %w", err)
			}
			deadline := time.Now().Add(wait)
			for mutexExists(trayMutex) || mutexExists(windowMutex) || mutexExists(progressMutex) {
				if time.Now().After(deadline) {
					return errors.New("ODM did not exit in time")
				}
				time.Sleep(100 * time.Millisecond)
			}
			return nil
		},
	}
	c.Flags().DurationVar(&wait, "wait", 20*time.Second, "how long to wait for ODM to exit")
	return c
}
