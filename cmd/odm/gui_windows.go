//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
	"unsafe"

	"github.com/getlantern/systray"
	"github.com/jchv/go-webview2"
	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/aoztrk82/open-download-manager/internal/api"
	"github.com/aoztrk82/open-download-manager/internal/config"
	"github.com/aoztrk82/open-download-manager/internal/extbundle"
)

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

// guiCommands returns the root action (double-click) and the hidden
// subcommands it uses. Double-click = no arguments.
func guiCommands() (func(*cobra.Command, []string) error, []*cobra.Command) {
	launch := func(*cobra.Command, []string) error {
		// Re-launch detached so the console window of a double-click goes away
		// and the tray app survives its parent.
		return spawnSelf("tray")
	}
	var background bool
	tray := &cobra.Command{Use: "tray", Hidden: true, Short: "Tray app (daemon + notification-area icon)",
		RunE: func(*cobra.Command, []string) error { return runTray(background) }}
	tray.Flags().BoolVar(&background, "background", false, "start without showing the window")
	win := &cobra.Command{Use: "window", Hidden: true, Short: "Open the ODM window",
		RunE: func(*cobra.Command, []string) error { return runWindow() }}
	prog := &cobra.Command{Use: "progress <id>", Hidden: true, Short: "Open the progress window of a download",
		Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return runProgress(args[0]) }}
	return launch, []*cobra.Command{tray, win, prog, quitCmd()}
}

func spawnSelf(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// HideWindow is applied by Windows to the process's first ShowWindow call,
	// so it must stay off for the GUI window or that window would be invisible.
	shows := len(args) > 0 && (args[0] == "window" || args[0] == "progress")
	return startDetached(exe, !shows, args...)
}

// startLocalDaemon starts the tray app without a window (used by the CLI and by
// the browser extension through the native-messaging host).
func startLocalDaemon() error { return spawnSelf("tray", "--background") }

func localClient() *api.Client { return api.NewClient(localURL(), "") }

// ---- tray ----

func runTray(background bool) error {
	// Make the browser extension able to start us later (idempotent, cheap).
	if err := registerNativeHost(); err != nil {
		fmt.Fprintln(os.Stderr, "native messaging registration:", err)
	}
	// Keep the unpacked Chrome/Edge extension in step with this exe; the browser
	// picks it up on its next start. Not bundled is fine (Go-only build).
	if _, err := syncChromeFolder(); err != nil && !errors.Is(err, extbundle.ErrNotBundled) {
		fmt.Fprintln(os.Stderr, "chrome extension folder:", err)
	}
	cl := localClient()
	if _, err := cl.Info(); err == nil {
		// A daemon (and probably a tray) is already running: just show the window.
		if background {
			return nil
		}
		return spawnSelf("window")
	}
	resetQuit()
	holdMutex(trayMutex)
	quitReq := quitSignal()
	trayLang.Store(progLang{}.Lang())
	langCh := make(chan string, 1)
	autoCh := make(chan bool, 1)
	d, err := startDaemon(daemonOpts{Addr: "127.0.0.1:" + strconv.Itoa(config.DefaultPort), Reveal: true,
		OnLang: func(l string) {
			select { // keep only the newest change
			case <-langCh:
			default:
			}
			langCh <- l
		},
		// Double-click in the list: the user asked, so no window limit.
		ShowWindow: func(id string) error { return spawnSelf("progress", id) },
		// Settings > General changes autostart: keep the tray's checkbox in step.
		Autostart: winAutostart{onChange: func(on bool) {
			select {
			case <-autoCh:
			default:
			}
			autoCh <- on
		}},
	})
	if err != nil {
		return err
	}
	defer d.Close()
	go watchDownloads(d)

	systray.Run(func() {
		systray.SetIcon(trayIcon())
		systray.SetTooltip("ODM - Open Download Manager")
		open := systray.AddMenuItem(trayText("open"), trayText("openTip"))
		folder := systray.AddMenuItem(trayText("folder"), "")
		systray.AddSeparator()
		auto := systray.AddMenuItemCheckbox(trayText("autostart"), "", autostartEnabled())
		systray.AddSeparator()
		quit := systray.AddMenuItem(trayText("quit"), trayText("quitTip"))

		if !background {
			_ = spawnSelf("window") // first launch shows the window
		}
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					_ = spawnSelf("window")
				case <-folder.ClickedCh:
					_ = exec.Command("explorer", d.settings.Settings().Dir()).Start()
				case <-auto.ClickedCh:
					if err := setAutostart(!auto.Checked()); err == nil {
						if auto.Checked() {
							auto.Uncheck()
						} else {
							auto.Check()
						}
					}
				case on := <-autoCh:
					if on {
						auto.Check()
					} else {
						auto.Uncheck()
					}
				case l := <-langCh:
					trayLang.Store(l)
					open.SetTitle(trayText("open"))
					open.SetTooltip(trayText("openTip"))
					folder.SetTitle(trayText("folder"))
					auto.SetTitle(trayText("autostart"))
					quit.SetTitle(trayText("quit"))
					quit.SetTooltip(trayText("quitTip"))
				case <-quit.ClickedCh:
					systray.Quit()
					return
				case <-quitReq: // `odm quit` (installer / uninstaller)
					systray.Quit()
					return
				case err := <-d.Done():
					fmt.Fprintln(os.Stderr, "daemon stopped:", err)
					systray.Quit()
					return
				}
			}
		}()
	}, func() {})
	return nil
}

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("ODM")
	return err == nil
}

func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		return k.DeleteValue("ODM")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// --background: signing in to Windows should not pop up the window. The
	// installer's autostart task writes the same value (installer/odm.iss).
	return k.SetStringValue("ODM", `"`+exe+`" tray --background`)
}

// winAutostart is Settings > General > "Start with Windows".
type winAutostart struct{ onChange func(bool) }

func (a winAutostart) Enabled() bool { return autostartEnabled() }

func (a winAutostart) Set(on bool) error {
	if err := setAutostart(on); err != nil {
		return err
	}
	if a.onChange != nil {
		a.onChange(on)
	}
	return nil
}

// ---- window ----

func runWindow() error {
	// One window at a time.
	name, _ := windows.UTF16PtrFromString("odm-window")
	if _, err := windows.CreateMutex(nil, false, name); err == windows.ERROR_ALREADY_EXISTS {
		return nil
	}
	cl := localClient()
	cl.HTTP.Timeout = 2 * time.Second
	// The program's language goes in the URL so the first paint is already in
	// it (the Web UI would otherwise start in WebView2's language and switch).
	url := cl.Base + "/#lang=" + progLang{}.Lang()

	w, user32 := newWebView("ODM", 1000, 680)
	if w == nil {
		// WebView2 runtime missing: fall back to the default browser.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	defer w.Destroy()
	makeFrameless(w, user32)
	// Overall progress of running downloads on the taskbar button.
	tb := newTaskbar(uintptr(w.Window()))
	_ = w.Bind("odmProgress", func(state string, done, total float64, _ string) {
		tb.set(state, uint64(done), uint64(total))
	})
	closeWithDaemon(w, cl)
	w.Navigate(url)
	w.Run()
	return nil
}

// newWebView creates a centered WebView2 window sized in 96-DPI pixels; nil
// when the WebView2 runtime is missing. Locks the calling goroutine to its
// thread: the window's message loop (w.Run) must run on it.
func newWebView(title string, width, height int) (webview2.WebView, *windows.LazyDLL) {
	runtime.LockOSThread()
	// Without this Windows bitmap-scales the window (blurry, mis-centered) on
	// high-DPI screens. -4 = PER_MONITOR_AWARE_V2.
	user32 := windows.NewLazySystemDLL("user32.dll")
	user32.NewProc("SetProcessDpiAwarenessContext").Call(^uintptr(3))
	scale := 1.0
	if dpi, _, _ := user32.NewProc("GetDpiForSystem").Call(); dpi > 0 {
		scale = float64(dpi) / 96
	}
	cfg, _ := config.Dir()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(cfg, "webview2"),
		WindowOptions: webview2.WindowOptions{
			Title: title, Width: uint(float64(width) * scale), Height: uint(float64(height) * scale), Center: true,
		},
	})
	return w, user32
}

// closeWithDaemon closes the window when the tray's daemon stops answering
// (a window is only a view on it) or when `odm quit` asks (the installer waits
// for this exe to be free).
func closeWithDaemon(w webview2.WebView, cl *api.Client) {
	quitReq := quitSignal()
	go func() {
		fails := 0
		for {
			select {
			case <-quitReq:
				w.Destroy()
				return
			case <-time.After(time.Second):
			}
			if _, err := cl.Info(); err != nil {
				if fails++; fails >= 3 {
					w.Destroy()
					return
				}
			} else {
				fails = 0
			}
		}
	}()
}

// ---- frameless window ----

const (
	gwlStyle        = ^uintptr(15) // -16
	wsCaption       = 0xC00000
	swpFlags        = 0x0002 | 0x0001 | 0x0004 | 0x0020 // NOMOVE|NOSIZE|NOZORDER|FRAMECHANGED
	wmNCLButtonDown = 0x00A1
	htCaption       = 2
	wmClose         = 0x0010
	swMinimize      = 6
)

type rect struct{ Left, Top, Right, Bottom int32 }

// makeFrameless drops the native title bar (the Web UI draws its own in the
// menu bar, like VS Code) but keeps WS_THICKFRAME so edges still resize, and
// exposes window.odmWin("min"|"max"|"close"|"drag") to the page.
func makeFrameless(w webview2.WebView, user32 *windows.LazyDLL) {
	hwnd := uintptr(w.Window())
	setWindowIcon(hwnd, user32)
	getLong, setLong := user32.NewProc("GetWindowLongPtrW"), user32.NewProc("SetWindowLongPtrW")
	setPos, showWin := user32.NewProc("SetWindowPos"), user32.NewProc("ShowWindow")
	getRect, getCursor := user32.NewProc("GetWindowRect"), user32.NewProc("GetCursorPos")
	releaseCap, sendMsg, postMsg := user32.NewProc("ReleaseCapture"), user32.NewProc("SendMessageW"), user32.NewProc("PostMessageW")

	style, _, _ := getLong.Call(hwnd, gwlStyle)
	setLong.Call(hwnd, gwlStyle, style&^wsCaption)
	setPos.Call(hwnd, 0, 0, 0, 0, 0, swpFlags)

	// A frameless WS_THICKFRAME window overshoots the monitor by the border
	// width when maximized by the system, so maximize manually to the work area.
	var maximized bool
	var restore rect
	monFromWin, monInfo := user32.NewProc("MonitorFromWindow"), user32.NewProc("GetMonitorInfoW")
	workArea := func() rect {
		mon, _, _ := monFromWin.Call(hwnd, 2) // MONITOR_DEFAULTTONEAREST
		var mi struct {
			Size          uint32
			Monitor, Work rect
			Flags         uint32
		}
		mi.Size = uint32(unsafe.Sizeof(mi))
		monInfo.Call(mon, uintptr(unsafe.Pointer(&mi)))
		return mi.Work
	}
	place := func(r rect) {
		setPos.Call(hwnd, 0, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0x0004) // NOZORDER
	}
	toggleMax := func() {
		if maximized {
			maximized = false
			place(restore)
			return
		}
		getRect.Call(hwnd, uintptr(unsafe.Pointer(&restore)))
		maximized = true
		place(workArea())
	}

	_ = w.Bind("odmWin", func(action string) {
		switch action {
		case "min":
			showWin.Call(hwnd, swMinimize)
		case "max":
			toggleMax()
		case "close":
			postMsg.Call(hwnd, wmClose, 0, 0)
		case "drag":
			if maximized { // dragging a maximized window restores it under the cursor
				var pt struct{ X, Y int32 }
				getCursor.Call(uintptr(unsafe.Pointer(&pt)))
				maximized = false
				width, height := restore.Right-restore.Left, restore.Bottom-restore.Top
				restore.Left, restore.Top = pt.X-width/2, pt.Y-15
				restore.Right, restore.Bottom = restore.Left+width, restore.Top+height
				place(restore)
			}
			releaseCap.Call()
			sendMsg.Call(hwnd, wmNCLButtonDown, htCaption, 0)
		}
	})
}
