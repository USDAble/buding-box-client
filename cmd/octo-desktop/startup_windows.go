//go:build windows

package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows"
)

func platformProcessStartedAt() (time.Time, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, created.Nanoseconds()), nil
}

// OCTO-FORK: Win32 controls render independently of WebView2's environment startup.
// Created on main before Wails; cleanup may also come from a shutdown goroutine.
func platformStartupWindow(title, message string, cancel func()) (func(), func(string), error) {
	threadID := w32.GetCurrentThreadId()
	instance := w32.GetModuleHandle("")
	className := w32.MustStringToUTF16Ptr("DesktopStartupFeedback")
	class := w32.WNDCLASSEX{
		Size:       uint32(unsafe.Sizeof(w32.WNDCLASSEX{})),
		Instance:   instance,
		ClassName:  className,
		Background: w32.COLOR_BTNFACE + 1,
		Cursor:     w32.LoadCursorWithResourceID(0, uint16(w32.IDC_ARROW)),
		WndProc: syscall.NewCallback(func(hwnd w32.HWND, msg uint32, wp, lp uintptr) uintptr {
			if msg == w32.WM_CLOSE {
				go cancel()
				return 0
			}
			return w32.DefWindowProc(hwnd, msg, wp, lp)
		}),
	}
	if w32.RegisterClassEx(&class) == 0 {
		return nil, nil, fmt.Errorf("register startup window: %w", syscall.GetLastError())
	}
	controls := w32.INITCOMMONCONTROLSEX{DwSize: uint32(unsafe.Sizeof(w32.INITCOMMONCONTROLSEX{})), DwICC: w32.ICC_PROGRESS_CLASS}
	if !w32.InitCommonControlsEx(&controls) {
		return nil, nil, fmt.Errorf("initialize startup progress control: %w", syscall.GetLastError())
	}
	const width, height = 360, 180
	x, y := (w32.GetSystemMetrics(w32.SM_CXSCREEN)-width)/2, (w32.GetSystemMetrics(w32.SM_CYSCREEN)-height)/2
	window := w32.CreateWindowEx(w32.WS_EX_TOOLWINDOW, className, w32.MustStringToUTF16Ptr(title),
		w32.WS_POPUP|w32.WS_CAPTION, x, y, width, height, 0, 0, instance, nil)
	if window == 0 {
		return nil, nil, fmt.Errorf("create startup window: %w", syscall.GetLastError())
	}
	label := w32.CreateWindowEx(0, w32.MustStringToUTF16Ptr("STATIC"), w32.MustStringToUTF16Ptr(message),
		w32.WS_CHILD|w32.WS_VISIBLE|w32.SS_CENTER, 20, 40, 320, 24, window, 0, instance, nil)
	font := w32.GetStockObject(w32.DEFAULT_GUI_FONT)
	w32.SendMessage(label, w32.WM_SETFONT, uintptr(font), 1)
	// These are the documented Win32 marquee progress constants.
	const pbsMarquee, pbmSetMarquee = 0x08, w32.WM_USER + 10
	progress := w32.CreateWindowEx(0, w32.MustStringToUTF16Ptr(w32.PROGRESS_CLASS), nil,
		w32.WS_CHILD|w32.WS_VISIBLE|pbsMarquee, 40, 78, 280, 16, window, 0, instance, nil)
	if label == 0 || progress == 0 {
		w32.DestroyWindow(window)
		return nil, nil, fmt.Errorf("create startup controls: %w", syscall.GetLastError())
	}
	w32.SendMessage(progress, pbmSetMarquee, 1, 30)
	w32.ShowWindow(window, w32.SW_SHOWNORMAL)
	w32.UpdateWindow(window)
	closeWindow := func() {
		close := func() { w32.DestroyWindow(window) }
		if w32.GetCurrentThreadId() == threadID {
			close()
		} else {
			application.InvokeSync(close)
		}
	}
	return closeWindow, func(message string) {
		w32.SetWindowText(label, message)
		w32.UpdateWindow(label)
	}, nil
}
