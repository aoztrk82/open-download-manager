//go:build windows

package main

import (
	"errors"
	"regexp"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"

	"github.com/aoztrk82/open-download-manager/internal/store"
)

// Progress windows: a small window per download (like other download
// managers) with speed, time left, the connections map and Pause / Cancel,
// plus progress on its taskbar button. At the end it turns into the "download
// complete" window (Open, Show in folder). Settings > Downloads switches both.
// Each is its own `odm.exe progress <id>` process, a view on the daemon like
// the main window.

const progressMutex = `Local\odm-progress` // held by every progress window, for `odm quit`

var downloadID = regexp.MustCompile(`^[0-9a-f]{16}$`)

// maxProgressWindows keeps a long playlist from covering the screen.
const maxProgressWindows = 6

var openProgressWindows atomic.Int32

func runProgress(id string) error {
	if !downloadID.MatchString(id) {
		return errors.New("bad download id")
	}
	// One window per download: a second start (e.g. "complete" while the
	// progress window is still open) just leaves the open one.
	name, _ := windows.UTF16PtrFromString(`Local\odm-progress-` + id)
	if _, err := windows.CreateMutex(nil, false, name); err == windows.ERROR_ALREADY_EXISTS {
		return nil
	}
	holdMutex(progressMutex)
	cl := localClient()
	cl.HTTP.Timeout = 2 * time.Second
	if _, err := cl.Get(id); err != nil {
		return err // removed meanwhile, or no daemon
	}
	url := cl.Base + "/progress.html#id=" + id + "&lang=" + progLang{}.Lang()

	w, user32 := newWebView("ODM", 500, 330)
	if w == nil {
		return nil // no WebView2: the main window (browser fallback) still shows progress
	}
	defer w.Destroy()
	hwnd := uintptr(w.Window())
	setWindowIcon(hwnd, user32)
	tb := newTaskbar(hwnd)
	_ = w.Bind("odmProgress", func(state string, done, total float64, title string) {
		if title != "" {
			w.SetTitle(title)
		}
		tb.set(state, uint64(done), uint64(total))
	})
	postMsg := user32.NewProc("PostMessageW")
	_ = w.Bind("odmClose", func() { postMsg.Call(hwnd, wmClose, 0, 0) })
	closeWithDaemon(w, cl)
	w.Navigate(url)
	w.Run()
	return nil
}

// openProgress starts a progress window process unless too many are open.
func openProgress(id string) {
	if openProgressWindows.Load() >= maxProgressWindows {
		return
	}
	openProgressWindows.Add(1)
	go func() {
		// The count is a guess at what is still open: windows close themselves
		// or are closed by the user, so forget each after a while.
		time.Sleep(10 * time.Minute)
		openProgressWindows.Add(-1)
	}()
	_ = spawnSelf("progress", id)
}

// watchDownloads opens progress windows as downloads start and "complete"
// windows as they finish, following Settings > Downloads.
func watchDownloads(d *daemon) {
	ch, cancel := d.q.Subscribe()
	defer cancel()
	status := map[string]store.Status{}
	// Downloads that were queued or running when ODM started resume on their
	// own; a window for each of them at sign-in would be a nuisance.
	resumed := map[string]bool{}
	if all, err := d.q.List(); err == nil {
		for _, x := range all {
			status[x.ID] = x.Status
			if x.Status == store.Queued || x.Status == store.Active {
				resumed[x.ID] = true
			}
		}
	}
	shown := map[string]bool{}
	for {
		// The subscription lives as long as the tray process. (d.Done() is not
		// watched here: the tray reads that channel itself.)
		ev := <-ch
		if ev.Type == "removed" {
			delete(status, ev.ID)
			delete(resumed, ev.ID)
			delete(shown, ev.ID)
			continue
		}
		x := ev.Download
		if x == nil {
			continue
		}
		prev, known := status[x.ID]
		status[x.ID] = x.Status
		if known && prev == x.Status {
			continue // progress tick
		}
		s := d.settings.Settings()
		switch x.Status {
		case store.Active:
			if resumed[x.ID] {
				delete(resumed, x.ID)
				continue
			}
			if s.ShowProgress && !shown[x.ID] {
				shown[x.ID] = true
				openProgress(x.ID)
			}
		case store.Completed:
			delete(resumed, x.ID)
			// An open progress window turns into the complete window itself;
			// otherwise (never shown, or closed with Hide) open one now.
			if s.ShowComplete {
				openProgress(x.ID)
			}
		case store.Paused, store.Failed:
			delete(resumed, x.ID)
			delete(shown, x.ID) // resuming it later shows the window again
		}
	}
}
