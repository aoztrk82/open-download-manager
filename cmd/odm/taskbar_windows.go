//go:build windows

package main

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Taskbar button progress (ITaskbarList3): the green bar behind a window's
// taskbar button, yellow when paused, red on errors. Called on the window's UI
// thread, where WebView2 has already initialized COM (STA).

var (
	clsidTaskbarList = windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11d0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidTaskbarList3  = windows.GUID{Data1: 0xEA1AFB91, Data2: 0x9E28, Data3: 0x4B86, Data4: [8]byte{0x90, 0xE9, 0x9E, 0x9F, 0x8A, 0x5E, 0xEF, 0xAF}}
	coCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")
)

type taskbarVtbl struct {
	QueryInterface, AddRef, Release                      uintptr
	HrInit, AddTab, DeleteTab, ActivateTab, SetActiveAlt uintptr
	MarkFullscreenWindow                                 uintptr
	SetProgressValue, SetProgressState                   uintptr
}

type taskbarObj struct{ vtbl *taskbarVtbl }

type taskbar struct {
	obj  *taskbarObj
	hwnd uintptr
}

// TBPFLAG values.
var taskbarStates = map[string]uintptr{"none": 0, "indeterminate": 1, "normal": 2, "error": 4, "paused": 8}

// newTaskbar returns nil when the shell has no taskbar list (then progress
// simply is not shown on the button).
func newTaskbar(hwnd uintptr) *taskbar {
	var obj *taskbarObj
	const clsctxInprocServer = 1
	r, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidTaskbarList3)), uintptr(unsafe.Pointer(&obj)))
	if r != 0 || obj == nil {
		return nil
	}
	if r, _, _ := syscall.SyscallN(obj.vtbl.HrInit, uintptr(unsafe.Pointer(obj))); r != 0 {
		return nil
	}
	return &taskbar{obj: obj, hwnd: hwnd}
}

// ulonglong passes a 64-bit COM argument: one register on 64-bit Windows, two
// stack slots (low, high) on 32-bit.
func ulonglong(v uint64) []uintptr {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return []uintptr{uintptr(v)}
	}
	return []uintptr{uintptr(uint32(v)), uintptr(v >> 32)}
}

// set shows state ("none", "indeterminate", "normal", "paused", "error") and,
// when total > 0, done/total on the window's taskbar button.
func (t *taskbar) set(state string, done, total uint64) {
	if t == nil {
		return
	}
	st, ok := taskbarStates[state]
	if !ok {
		st = 0
	}
	this := uintptr(unsafe.Pointer(t.obj))
	syscall.SyscallN(t.obj.vtbl.SetProgressState, this, t.hwnd, st)
	if st >= 2 && total > 0 {
		args := append([]uintptr{this, t.hwnd}, ulonglong(min(done, total))...)
		args = append(args, ulonglong(total)...)
		syscall.SyscallN(t.obj.vtbl.SetProgressValue, args...)
	}
}
