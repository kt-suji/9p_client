//go:build windows

package main

import (
	"context"
	"log"
	"os/exec"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
)

const (
	ERROR_ALREADY_EXISTS = 183
)

func ensureSingleInstance(appName string) {
	namePtr, _ := windows.UTF16PtrFromString(appName)
	hMutex, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if hMutex != 0 && err == windows.ERROR_ALREADY_EXISTS {
		log.Println("[INFO] 既に 9P Client GUI が起動しています。既存ウィンドウを復元します。")
		clsPtr, _ := windows.UTF16PtrFromString("9PClientExplorerWindowClass")
		hExisting, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(clsPtr)), 0)
		if hExisting != 0 {
			user32.NewProc("ShowWindow").Call(hExisting, uintptr(SW_SHOW))
			user32.NewProc("SetForegroundWindow").Call(hExisting)
		}
		windows.ExitProcess(0)
	}
}

func buildClientShellCommand(ctx context.Context, cmdStr string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe", "/c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}

// decodeClientConsoleOutput は Windows CP932 (Shift-JIS) コンソール出力を UTF-8 に変換します。
func decodeClientConsoleOutput(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	const cp932 = 932
	n, _, _ := procMultiByteToWideChar.Call(
		uintptr(cp932),
		0,
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		0,
		0,
	)
	if n == 0 {
		return string(raw)
	}
	wbuf := make([]uint16, n)
	procMultiByteToWideChar.Call(
		uintptr(cp932),
		0,
		uintptr(unsafe.Pointer(&raw[0])),
		uintptr(len(raw)),
		uintptr(unsafe.Pointer(&wbuf[0])),
		n,
	)
	return windows.UTF16ToString(wbuf)
}
