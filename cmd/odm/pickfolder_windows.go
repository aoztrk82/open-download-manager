//go:build windows

package main

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// pickFolder shows the standard Explorer folder chooser (IFileOpenDialog with
// FOS_PICKFOLDERS) and returns the chosen path, or "" if the user cancelled.
// The request comes from a browser, so ODM is not the foreground app: the
// dialog is owned by an invisible topmost window so it does not open behind
// the browser.
var pickMu sync.Mutex

var (
	ole32               = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInst    = ole32.NewProc("CoCreateInstance")
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procSHCreateItem    = shell32.NewProc("SHCreateItemFromParsingName")
	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidShellItem        = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	clsctxInprocServer = 0x1
	fosPickFolders     = 0x20
	fosForceFileSystem = 0x40
	sigdnFileSysPath   = 0x80058000
	errCancelled       = 0x800704C7 // HRESULT_FROM_WIN32(ERROR_CANCELLED)

	// IFileDialog vtable slots (IUnknown 0-2, IModalWindow::Show 3, then IFileDialog).
	vtRelease    = 2
	vtShow       = 3
	vtSetOptions = 9
	vtGetOptions = 10
	vtSetFolder  = 12
	vtSetTitle   = 17
	vtGetResult  = 20
	// IShellItem::GetDisplayName
	vtGetDisplayName = 5
)

// comObj is a COM interface pointer: its first word points at the vtable.
type comObj struct{ vtbl *[32]uintptr }

// comCall invokes method slot i of a COM object.
func comCall(obj *comObj, i int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(obj.vtbl[i], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	return r
}

func pickFolder(initial string) (string, error) {
	if !pickMu.TryLock() {
		return "", errors.New("a folder chooser is already open")
	}
	defer pickMu.Unlock()

	type result struct {
		path string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		// COM objects belong to the thread that created them.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		p, err := pickFolderOnThread(initial)
		ch <- result{p, err}
	}()
	r := <-ch
	return r.path, r.err
}

func pickFolderOnThread(initial string) (string, error) {
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		return "", err
	}
	defer windows.CoUninitialize()

	var dlg *comObj
	if hr, _, _ := procCoCreateInst.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidFileOpenDialog)), uintptr(unsafe.Pointer(&dlg))); hr != 0 || dlg == nil {
		return "", errors.New("could not create the folder chooser")
	}
	defer comCall(dlg, vtRelease)

	var opts uint32
	comCall(dlg, vtGetOptions, uintptr(unsafe.Pointer(&opts)))
	comCall(dlg, vtSetOptions, uintptr(opts|fosPickFolders|fosForceFileSystem))
	if title, err := windows.UTF16PtrFromString("ODM"); err == nil {
		comCall(dlg, vtSetTitle, uintptr(unsafe.Pointer(title)))
	}
	if initial != "" {
		if p, err := windows.UTF16PtrFromString(initial); err == nil {
			var item *comObj
			if hr, _, _ := procSHCreateItem.Call(uintptr(unsafe.Pointer(p)), 0,
				uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item))); hr == 0 && item != nil {
				comCall(dlg, vtSetFolder, uintptr(unsafe.Pointer(item)))
				comCall(item, vtRelease)
			}
		}
	}

	owner := topmostOwner()
	if owner != 0 {
		defer procDestroyWindow.Call(owner)
	}
	switch hr := comCall(dlg, vtShow, owner); {
	case uint32(hr) == errCancelled:
		return "", nil
	case hr != 0:
		return "", errors.New("folder chooser failed")
	}

	var item *comObj
	if hr := comCall(dlg, vtGetResult, uintptr(unsafe.Pointer(&item))); hr != 0 || item == nil {
		return "", errors.New("no folder selected")
	}
	defer comCall(item, vtRelease)
	var name *uint16
	if hr := comCall(item, vtGetDisplayName, sigdnFileSysPath, uintptr(unsafe.Pointer(&name))); hr != 0 || name == nil {
		return "", errors.New("the selected item is not a folder on disk")
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(name))
	return windows.UTF16PtrToString(name), nil
}

var (
	user32dll         = windows.NewLazySystemDLL("user32.dll")
	procCreateWindow  = user32dll.NewProc("CreateWindowExW")
	procDestroyWindow = user32dll.NewProc("DestroyWindow")
)

// topmostOwner creates an invisible, topmost tool window to own the dialog;
// owned windows of a topmost window are topmost too.
func topmostOwner() uintptr {
	const wsExTopmost, wsExToolWindow, wsPopup = 0x8, 0x80, 0x80000000
	cls, _ := windows.UTF16PtrFromString("STATIC")
	h, _, _ := procCreateWindow.Call(wsExTopmost|wsExToolWindow, uintptr(unsafe.Pointer(cls)), 0, wsPopup,
		0, 0, 0, 0, 0, 0, 0, 0)
	return h
}
