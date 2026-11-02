//go:build windows

package main

import (
	"fmt"
	"io"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/proto"
	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	comctl32 = windows.NewLazySystemDLL("comctl32.dll")

	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	procAppendMenuW          = user32.NewProc("AppendMenuW")
	procTrackPopupMenu       = user32.NewProc("TrackPopupMenu")
	procDestroyMenu          = user32.NewProc("DestroyMenu")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procLoadIconW            = user32.NewProc("LoadIconW")
	procLoadCursorW          = user32.NewProc("LoadCursorW")
	procSendMessageW         = user32.NewProc("SendMessageW")
	procSetWindowTextW       = user32.NewProc("SetWindowTextW")
	procGetWindowTextW       = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procSetFocus             = user32.NewProc("SetFocus")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	procShell_NotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	procCallWindowProcW      = user32.NewProc("CallWindowProcW")
	procSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	procGetKeyState          = user32.NewProc("GetKeyState")
	procMultiByteToWideChar  = kernel32.NewProc("MultiByteToWideChar")
)

const (
	WM_USER          = 0x0400
	WM_TRAYICON      = WM_USER + 1
	WM_COMMAND       = 0x0111
	WM_NOTIFY        = 0x004E
	WM_CLOSE         = 0x0010
	WM_RBUTTONUP     = 0x0205
	WM_LBUTTONDBLCLK = 0x0203

	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	MF_STRING    = 0x00000000
	MF_SEPARATOR = 0x00000800

	TPM_BOTTOMALIGN = 0x0020
	TPM_RIGHTBUTTON = 0x0002

	IDI_APPLICATION = 32512
	IDC_ARROW       = 32512

	SW_HIDE = 0
	SW_SHOW = 5

	MB_OK              = 0x00000000
	MB_ICONERROR       = 0x00000010
	MB_ICONINFORMATION = 0x00000040

	ICC_TREEVIEW_CLASSES = 0x00000002

	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_VSCROLL          = 0x00200000
	WS_HSCROLL          = 0x00100000
	WS_BORDER           = 0x00800000

	ES_MULTILINE   = 0x0004
	ES_AUTOVSCROLL = 0x0040
	ES_AUTOHSCROLL = 0x0080
	ES_WANTRETURN  = 0x1000

	TVS_HASLINES      = 0x0002
	TVS_LINESATROOT   = 0x0004
	TVS_HASBUTTONS    = 0x0001
	TVS_SHOWSELALWAYS = 0x0020

	TV_FIRST           = 0x1100
	TVM_INSERTITEMW    = TV_FIRST + 50
	TVM_DELETEALLITEMS = TV_FIRST + 1
	TVM_EXPAND         = TV_FIRST + 2
	TVM_SELECTITEM     = TV_FIRST + 11

	TVGN_CARET = 0x0009
	TVE_EXPAND = 0x0002

	TVIF_TEXT  = 0x0001
	TVIF_PARAM = 0x0004

	TVN_FIRST       = ^uint32(400 - 1)
	TVN_SELCHANGEDW = TVN_FIRST - 51

	ID_BTN_RECONNECT   = 2001
	ID_BTN_MOUNT       = 2002
	ID_BTN_REFRESH     = 2003
	ID_BTN_SAVE        = 2004
	ID_BTN_CONFIG      = 2005
	ID_TREEVIEW        = 2006
	ID_EDIT_ADDR       = 2007
	ID_EDIT_CONTENT    = 2008
	ID_BTN_RELOAD_FILE = 2009

	CMD_TRAY_OPEN   = 2101
	CMD_TRAY_MOUNT  = 2102
	CMD_TRAY_CONFIG = 2103
	CMD_TRAY_EXIT   = 2104
)

type NOTIFYICONDATAW struct {
	CbSize            uint32
	HWnd              windows.Handle
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             windows.Handle
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          windows.GUID
	HBalloonIcon      windows.Handle
}

type POINT struct {
	X int32
	Y int32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type MSG struct {
	HWnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

type TVITEMW struct {
	Mask           uint32
	HItem          uintptr
	State          uint32
	StateMask      uint32
	PszText        *uint16
	CchTextMax     int32
	IImage         int32
	ISelectedImage int32
	CChildren      int32
	LParam         uintptr
}

type TVINSERTSTRUCTW struct {
	HParent      uintptr
	HInsertAfter uintptr
	Item         TVITEMW
}

type NMHDR struct {
	HWndFrom windows.Handle
	IdFrom   uintptr
	Code     uint32
}

type NMTREEVIEWW struct {
	Hdr     NMHDR
	Action  uint32
	ItemOld TVITEMW
	ItemNew TVITEMW
	PtDrag  POINT
}

type NativeExplorer struct {
	hWndMain     windows.Handle
	hTree        windows.Handle
	hEdit        windows.Handle
	hEditAddr    windows.Handle
	hBtnMount    windows.Handle
	hStatusLbl   windows.Handle
	currentPath  string
	client       *client.Client
	cfg          *ClientConfig
	pathMap      map[uintptr]string
	nextParamID  uintptr
	isPopulating bool
	mu           sync.Mutex
}

func (exp *NativeExplorer) getClient() *client.Client {
	exp.mu.Lock()
	defer exp.mu.Unlock()
	return exp.client
}

func (exp *NativeExplorer) setClient(c *client.Client) {
	exp.mu.Lock()
	exp.client = c
	exp.mu.Unlock()
}

type INITCOMMONCONTROLSEX struct {
	DwSize uint32
	DwICC  uint32
}

func loword(l uint32) uint16 {
	return uint16(l & 0xffff)
}

var (
	globalExplorer  *NativeExplorer
	origEditWndProc uintptr
)

const (
	WM_KEYDOWN = 0x0100
	VK_CONTROL = 0x11
	VK_F5      = 0x74
)

var (
	gwlWndProc = int(-4)
)

func editSubclassWndProc(hWnd windows.Handle, msg uintptr, wParam, lParam uintptr) uintptr {
	if uint32(msg) == WM_KEYDOWN {
		if wParam == 0x53 {
			state, _, _ := procGetKeyState.Call(uintptr(VK_CONTROL))
			if int16(state) < 0 {
				if globalExplorer != nil {
					globalExplorer.saveCurrentFile()
				}
				return 0
			}
		}
		if wParam == VK_F5 {
			if globalExplorer != nil {
				globalExplorer.mu.Lock()
				p := globalExplorer.currentPath
				globalExplorer.mu.Unlock()
				if p != "" {
					go globalExplorer.loadFileContent(p)
				} else {
					go globalExplorer.refreshTree()
				}
			}
			return 0
		}
	}
	ret, _, _ := procCallWindowProcW.Call(origEditWndProc, uintptr(hWnd), msg, wParam, lParam)
	return ret
}

func init() {
	var icex INITCOMMONCONTROLSEX
	icex.DwSize = uint32(unsafe.Sizeof(icex))
	icex.DwICC = ICC_TREEVIEW_CLASSES
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icex)))

	tbMsgPtr, _ := windows.UTF16PtrFromString("TaskbarCreated")
	msgId, _, _ := user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(tbMsgPtr)))
	wmTaskbarCreated = uint32(msgId)
}

func explorerWndProc(hWnd windows.Handle, msg uintptr, wParam, lParam uintptr) uintptr {
	exp := globalExplorer

	if wmTaskbarCreated != 0 && uint32(msg) == wmTaskbarCreated {
		if exp != nil {
			exp.setupTrayIcon(fmt.Sprintf("9P Client (%s)", exp.cfg.Client.MountDrive))
		}
		return 0
	}

	switch uint32(msg) {
	case WM_TRAYICON:
		switch lParam {
		case WM_RBUTTONUP:
			if exp != nil {
				exp.showTrayMenu()
			}
		case WM_LBUTTONDBLCLK:
			procShowWindow.Call(uintptr(hWnd), SW_SHOW)
			procSetForegroundWindow.Call(uintptr(hWnd))
		}
		return 0

	case WM_COMMAND:
		ctrlID := loword(uint32(wParam))
		if exp != nil {
			switch ctrlID {
			case ID_BTN_SAVE:
				exp.saveCurrentFile()
			case ID_BTN_RELOAD_FILE:
				exp.mu.Lock()
				p := exp.currentPath
				exp.mu.Unlock()
				if p != "" {
					go exp.loadFileContent(p)
				} else {
					go exp.refreshTree()
				}
			case ID_BTN_REFRESH:
				go exp.refreshTree()
			case ID_BTN_MOUNT, CMD_TRAY_MOUNT:
				exp.toggleMount()
			case ID_BTN_RECONNECT:
				go exp.reconnectServer()
			case ID_BTN_CONFIG, CMD_TRAY_CONFIG:
				OpenConfigDialog(exp.cfg)
			case CMD_TRAY_OPEN:
				procShowWindow.Call(uintptr(hWnd), SW_SHOW)
				procSetForegroundWindow.Call(uintptr(hWnd))
			case CMD_TRAY_EXIT:
				if globalMountManager.IsMounted() {
					_ = globalMountManager.Unmount()
				}
				exp.removeTrayIcon()
				procPostQuitMessage.Call(0)
			}
		}
		return 0

	case WM_NOTIFY:
		if exp != nil && !exp.isPopulating {
			nmhdr := (*NMHDR)(unsafe.Pointer(lParam))
			if nmhdr.IdFrom == ID_TREEVIEW && nmhdr.Code == TVN_SELCHANGEDW {
				nmtv := (*NMTREEVIEWW)(unsafe.Pointer(lParam))
				paramID := nmtv.ItemNew.LParam
				exp.mu.Lock()
				path, ok := exp.pathMap[paramID]
				exp.mu.Unlock()
				if ok && path != "" {
					go exp.loadFileContent(path)
				}
			}
		}
		return 0

	case WM_CLOSE:
		procShowWindow.Call(uintptr(hWnd), SW_HIDE)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hWnd), uintptr(msg), wParam, lParam)
	return ret
}

func (exp *NativeExplorer) showTrayMenu() {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	mountTitle := fmt.Sprintf("🔌 %s ドライブをマウント", exp.cfg.Client.MountDrive)
	if globalMountManager.IsMounted() {
		mountTitle = fmt.Sprintf("⏹️ %s ドライブを解除", exp.cfg.Client.MountDrive)
	}

	appendMenuItem(hMenu, CMD_TRAY_OPEN, "📁 9Pエクスプローラーを開く")
	appendMenuItem(hMenu, CMD_TRAY_MOUNT, mountTitle)
	appendMenuItem(hMenu, CMD_TRAY_CONFIG, "⚙️ 設定 (S.M.A.R.T.等) を開く")
	procAppendMenuW.Call(hMenu, uintptr(MF_SEPARATOR), 0, 0)
	appendMenuItem(hMenu, CMD_TRAY_EXIT, "❌ 終了")

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(uintptr(exp.hWndMain))
	procTrackPopupMenu.Call(hMenu, uintptr(TPM_BOTTOMALIGN|TPM_RIGHTBUTTON), uintptr(pt.X), uintptr(pt.Y), 0, uintptr(exp.hWndMain), 0)
}

var wmTaskbarCreated uint32

func loadAppIcon() uintptr {
	hInstance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	hIcon, _, _ := procLoadIconW.Call(hInstance, 1)
	if hIcon == 0 {
		hIcon, _, _ = procLoadIconW.Call(0, uintptr(IDI_APPLICATION))
	}
	return hIcon
}

func (exp *NativeExplorer) setupTrayIcon(title string) {
	hIcon := loadAppIcon()

	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = exp.hWndMain
	nid.UID = 1
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAYICON
	nid.HIcon = windows.Handle(hIcon)

	tipChars := windows.StringToUTF16(title)
	copy(nid.SzTip[:], tipChars)

	ret, _, _ := procShell_NotifyIconW.Call(uintptr(NIM_ADD), uintptr(unsafe.Pointer(&nid)))
	if ret == 0 {
		procShell_NotifyIconW.Call(uintptr(NIM_MODIFY), uintptr(unsafe.Pointer(&nid)))
	}
}

func (exp *NativeExplorer) removeTrayIcon() {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = exp.hWndMain
	nid.UID = 1
	procShell_NotifyIconW.Call(uintptr(NIM_DELETE), uintptr(unsafe.Pointer(&nid)))
}

func (exp *NativeExplorer) reconnectServer() {
	length, _, _ := procGetWindowTextLengthW.Call(uintptr(exp.hEditAddr))
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(uintptr(exp.hEditAddr), uintptr(unsafe.Pointer(&buf[0])), length+1)
	newAddr := strings.TrimSpace(windows.UTF16ToString(buf))
	if newAddr == "" {
		return
	}

	exp.setStatus(fmt.Sprintf("接続試行中: %s ...", newAddr))
	c, err := dial9PClient(newAddr, 3*time.Second)
	if err != nil {
		exp.setStatus(fmt.Sprintf("接続失敗: %v", err))
		exp.msgBox("接続エラー", fmt.Sprintf("サーバー (%s) に接続できませんでした:\n%v", newAddr, err), MB_ICONERROR)
		return
	}

	exp.setClient(c)
	exp.cfg.Client.ServerAddr = newAddr
	globalMountManager.UpdateClient(c)
	exp.setStatus(fmt.Sprintf("接続成功: %s", newAddr))
	startClientTelemetryReporter(c, newAddr)
	exp.refreshTree()
}

func (exp *NativeExplorer) toggleMount() {
	if globalMountManager.IsMounted() {
		if err := globalMountManager.Unmount(); err != nil {
			exp.msgBox("アンマウント失敗", err.Error(), MB_ICONERROR)
		} else {
			exp.updateMountBtnText()
		}
	} else {
		c := exp.getClient()
		if c == nil {
			exp.msgBox("マウントエラー", "9Pサーバーに未接続です。上の『再接続』を押してください。", MB_ICONERROR)
			return
		}
		err := globalMountManager.MountAsync(
			c,
			exp.cfg.Client.MountDrive,
			exp.cfg.Server.Disk.TotalBytes,
			exp.cfg.Server.Disk.FreeBytes,
			exp.cfg.Client.VolumeLabel,
			exp.cfg.Client.MountAsNetwork,
		)
		if err != nil {
			exp.msgBox("マウント失敗", err.Error(), MB_ICONERROR)
		} else {
			exp.updateMountBtnText()
		}
	}
}

func (exp *NativeExplorer) updateMountBtnText() {
	if globalMountManager.IsMounted() {
		setControlText(exp.hBtnMount, fmt.Sprintf("⏹️ %s 解除", exp.cfg.Client.MountDrive))
	} else {
		setControlText(exp.hBtnMount, fmt.Sprintf("🔌 %s マウント", exp.cfg.Client.MountDrive))
	}
}

func (exp *NativeExplorer) setStatus(msg string) {
	setControlText(exp.hStatusLbl, msg)
}

func (exp *NativeExplorer) msgBox(title, msg string, flags uint32) {
	tPtr, _ := windows.UTF16PtrFromString(title)
	mPtr, _ := windows.UTF16PtrFromString(msg)
	procMessageBoxW.Call(uintptr(exp.hWndMain), uintptr(unsafe.Pointer(mPtr)), uintptr(unsafe.Pointer(tPtr)), uintptr(flags))
}

func (exp *NativeExplorer) refreshTree() {
	c := exp.getClient()
	if c == nil {
		exp.setStatus("未接続です。バックグラウンドで自動接続を試行中...")
		return
	}

	exp.mu.Lock()
	openedPath := exp.currentPath
	exp.isPopulating = true
	exp.pathMap = make(map[uintptr]string)
	exp.nextParamID = 1
	exp.mu.Unlock()

	procSendMessageW.Call(uintptr(exp.hTree), TVM_DELETEALLITEMS, 0, 0)

	rootItem := exp.insertTreeItem(0, "/", "")
	targetHItem := exp.populateChildren(c, rootItem, "", openedPath)
	procSendMessageW.Call(uintptr(exp.hTree), TVM_EXPAND, TVE_EXPAND, rootItem)

	if targetHItem != 0 {
		procSendMessageW.Call(uintptr(exp.hTree), TVM_SELECTITEM, TVGN_CARET, targetHItem)
	}

	exp.mu.Lock()
	exp.isPopulating = false
	exp.mu.Unlock()

	if openedPath != "" {
		exp.loadFileContent(openedPath)
		exp.setStatus(fmt.Sprintf("✓ ツリーおよび開いているファイル (%s) を最新に更新しました", openedPath))
	} else {
		exp.setStatus("ツリー更新完了")
	}
}

func (exp *NativeExplorer) insertTreeItem(hParent uintptr, text, path string) uintptr {
	exp.mu.Lock()
	paramID := exp.nextParamID
	exp.nextParamID++
	if path != "" {
		exp.pathMap[paramID] = path
	}
	exp.mu.Unlock()

	textPtr, _ := windows.UTF16PtrFromString(text)
	var tvins TVINSERTSTRUCTW
	tvins.HParent = hParent
	tvins.HInsertAfter = 0
	tvins.Item.Mask = TVIF_TEXT | TVIF_PARAM
	tvins.Item.PszText = textPtr
	tvins.Item.LParam = paramID

	hItem, _, _ := procSendMessageW.Call(uintptr(exp.hTree), TVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&tvins)))
	return hItem
}

func (exp *NativeExplorer) populateChildren(c *client.Client, hParent uintptr, parentPath string, targetPath string) uintptr {
	var targetHItem uintptr
	cleanPath := strings.TrimPrefix(parentPath, "/")
	entries, err := c.Readdir(cleanPath)
	if err != nil {
		return 0
	}

	sort.Slice(entries, func(i, j int) bool {
		iDir := (entries[i].Mode & proto.DMDIR) != 0
		jDir := (entries[j].Mode & proto.DMDIR) != 0
		if iDir != jDir {
			return iDir
		}
		return entries[i].Name < entries[j].Name
	})

	for _, e := range entries {
		subPath := e.Name
		if cleanPath != "" {
			subPath = cleanPath + "/" + e.Name
		}

		if (e.Mode & proto.DMDIR) != 0 {
			dirItem := exp.insertTreeItem(hParent, "📁 "+e.Name, "")
			childTarget := exp.populateChildren(c, dirItem, subPath, targetPath)
			if childTarget != 0 {
				targetHItem = childTarget
			}
			procSendMessageW.Call(uintptr(exp.hTree), TVM_EXPAND, TVE_EXPAND, dirItem)
		} else {
			hItem := exp.insertTreeItem(hParent, "📄 "+e.Name, subPath)
			if targetPath != "" {
				cleanSub := strings.TrimPrefix(subPath, "/")
				cleanTgt := strings.TrimPrefix(targetPath, "/")
				if cleanSub == cleanTgt {
					targetHItem = hItem
				}
			}
		}
	}
	return targetHItem
}

func (exp *NativeExplorer) loadFileContent(path string) {
	c := exp.getClient()
	if c == nil {
		return
	}
	exp.mu.Lock()
	exp.currentPath = path
	exp.mu.Unlock()

	cleanPath := strings.TrimPrefix(path, "/")

	f, err := c.Open(cleanPath, proto.Oread)
	if err != nil {
		setControlText(exp.hEdit, fmt.Sprintf("エラー: 開けませんでした: %v", err))
		exp.setStatus(fmt.Sprintf("読み取り失敗: %s", path))
		return
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		setControlText(exp.hEdit, fmt.Sprintf("エラー: 読み取り失敗: %v", err))
		return
	}

	content := strings.ReplaceAll(string(data), "\n", "\r\n")
	setControlText(exp.hEdit, content)
	exp.setStatus(fmt.Sprintf("表示中: %s (%d バイト)  [Ctrl+S: 保存・実行 / F5: 最新再読込]", path, len(data)))
}

func (exp *NativeExplorer) saveCurrentFile() {
	exp.mu.Lock()
	targetPath := exp.currentPath
	exp.mu.Unlock()

	if targetPath == "" {
		exp.msgBox("保存エラー", "編集中のファイルがありません。左のツリーから選択してください。", MB_ICONERROR)
		return
	}

	c := exp.getClient()
	if c == nil {
		exp.msgBox("保存エラー", "サーバーに未接続です。", MB_ICONERROR)
		return
	}

	length, _, _ := procGetWindowTextLengthW.Call(uintptr(exp.hEdit))
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(uintptr(exp.hEdit), uintptr(unsafe.Pointer(&buf[0])), length+1)
	text := windows.UTF16ToString(buf)
	cleanText := strings.ReplaceAll(text, "\r\n", "\n")

	cleanPath := strings.TrimPrefix(targetPath, "/")
	f, err := c.Open(cleanPath, proto.Owrite)
	if err != nil {
		exp.msgBox("書き込みエラー", fmt.Sprintf("ファイルを書き込みモードで開けませんでした:\n%v", err), MB_ICONERROR)
		return
	}

	dataToSend := []byte(cleanText)
	if cmd, err := extractClientUnifiedCommand([]byte(cleanText)); err == nil && cmd != "" {
		dataToSend = []byte(cmd)
	}

	_, err = f.Write(dataToSend)
	_ = f.Close()
	if err != nil {
		exp.msgBox("書き込みエラー", fmt.Sprintf("書き込みに失敗しました:\n%v", err), MB_ICONERROR)
		return
	}

	exp.setStatus(fmt.Sprintf("✓ %s に書き込み・命令送信しました！ (結果を自動更新中...)", targetPath))

	go func(savedPath string) {
		delays := []time.Duration{
			250 * time.Millisecond,
			1200 * time.Millisecond,
			2800 * time.Millisecond,
			5500 * time.Millisecond,
		}
		for _, d := range delays {
			time.Sleep(d)
			exp.mu.Lock()
			curr := exp.currentPath
			exp.mu.Unlock()
			if curr != savedPath {
				return
			}
			exp.loadFileContent(savedPath)
		}
	}(targetPath)
}

func setControlText(hWnd windows.Handle, text string) {
	ptr, _ := windows.UTF16PtrFromString(text)
	procSetWindowTextW.Call(uintptr(hWnd), uintptr(unsafe.Pointer(ptr)))
}

func createControl(className, text string, style uint32, x, y, w, h int32, hParent windows.Handle, id uintptr) windows.Handle {
	clsPtr, _ := windows.UTF16PtrFromString(className)
	txtPtr, _ := windows.UTF16PtrFromString(text)
	hInstance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)

	hWnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(clsPtr)),
		uintptr(unsafe.Pointer(txtPtr)),
		uintptr(style|WS_CHILD|WS_VISIBLE),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(hParent), id, hInstance, 0,
	)
	return windows.Handle(hWnd)
}

func appendMenuItem(hMenu uintptr, id uintptr, title string) {
	ptr, _ := windows.UTF16PtrFromString(title)
	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), id, uintptr(unsafe.Pointer(ptr)))
}

func attachToDefaultDesktop() {
	procOpenDesktopW := user32.NewProc("OpenDesktopW")
	procSetThreadDesktop := user32.NewProc("SetThreadDesktop")
	namePtr, _ := windows.UTF16PtrFromString("Default")
	hDefault, _, _ := procOpenDesktopW.Call(uintptr(unsafe.Pointer(namePtr)), 0, 0, 0x10000000)
	if hDefault != 0 {
		procSetThreadDesktop.Call(hDefault)
	}
}

func RunNativeApplication(c *client.Client, cfg *ClientConfig) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	attachToDefaultDesktop()

	className, _ := windows.UTF16PtrFromString("9PNativeExplorerWindow")
	titleName, _ := windows.UTF16PtrFromString(fmt.Sprintf("9P Virtual Resource Explorer (v%d)", CurrentClientVersion))
	hInstance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	hCursor, _, _ := procLoadCursorW.Call(0, uintptr(IDC_ARROW))
	hIcon := loadAppIcon()

	var wc WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = syscall.NewCallback(explorerWndProc)
	wc.HInstance = windows.Handle(hInstance)
	wc.HCursor = windows.Handle(hCursor)
	wc.HIcon = windows.Handle(hIcon)
	wc.HIconSm = windows.Handle(hIcon)
	wc.LpszClassName = className
	wc.HbrBackground = windows.Handle(16) // COLOR_BTNFACE + 1

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hWnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titleName)),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE,
		100, 100, 960, 640,
		0, 0, hInstance, 0,
	)

	if hWnd == 0 {
		log.Fatalf("[ERROR] メインウィンドウ作成失敗: %v\n", err)
	}

	exp := &NativeExplorer{
		hWndMain: windows.Handle(hWnd),
		client:   c,
		cfg:      cfg,
		pathMap:  make(map[uintptr]string),
	}
	globalExplorer = exp

	setAutoReconnectHook(func(newC *client.Client, serverAddr string) {
		exp.setClient(newC)
		exp.setStatus(fmt.Sprintf("✓ サーバー (%s) に自動接続しました", serverAddr))
		if exp.cfg != nil && exp.cfg.Client.AutoMountOnTray && !globalMountManager.IsMounted() {
			_ = globalMountManager.MountAsync(
				newC,
				exp.cfg.Client.MountDrive,
				exp.cfg.Server.Disk.TotalBytes,
				exp.cfg.Server.Disk.FreeBytes,
				exp.cfg.Client.VolumeLabel,
				exp.cfg.Client.MountAsNetwork,
			)
			exp.updateMountBtnText()
		}
		go exp.refreshTree()
	})

	createControl("STATIC", "サーバー:", 0, 15, 15, 55, 20, exp.hWndMain, 0)
	exp.hEditAddr = createControl("EDIT", cfg.Client.ServerAddr, WS_BORDER, 75, 12, 160, 24, exp.hWndMain, ID_EDIT_ADDR)
	createControl("BUTTON", "再接続", 0, 242, 12, 60, 25, exp.hWndMain, ID_BTN_RECONNECT)
	exp.hBtnMount = createControl("BUTTON", fmt.Sprintf("🔌 %s マウント", cfg.Client.MountDrive), 0, 308, 12, 98, 25, exp.hWndMain, ID_BTN_MOUNT)
	createControl("BUTTON", "🔄 ツリー更新", 0, 412, 12, 95, 25, exp.hWndMain, ID_BTN_REFRESH)
	createControl("BUTTON", "⚙️ 設定", 0, 513, 12, 75, 25, exp.hWndMain, ID_BTN_CONFIG)

	exp.hTree = createControl("SysTreeView32", "", WS_BORDER|TVS_HASLINES|TVS_LINESATROOT|TVS_HASBUTTONS|TVS_SHOWSELALWAYS, 15, 48, 260, 500, exp.hWndMain, ID_TREEVIEW)
	exp.hEdit = createControl("EDIT", "", WS_BORDER|WS_VSCROLL|WS_HSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_AUTOHSCROLL|ES_WANTRETURN, 285, 48, 640, 500, exp.hWndMain, ID_EDIT_CONTENT)

	editProcCallback := syscall.NewCallback(editSubclassWndProc)
	oldProc, _, _ := procSetWindowLongPtrW.Call(uintptr(exp.hEdit), uintptr(gwlWndProc), editProcCallback)
	origEditWndProc = oldProc

	createControl("BUTTON", "💾 保存・実行 (Ctrl+S)", 0, 285, 558, 165, 30, exp.hWndMain, ID_BTN_SAVE)
	createControl("BUTTON", "🔃 再読込 (F5)", 0, 456, 558, 110, 30, exp.hWndMain, ID_BTN_RELOAD_FILE)
	exp.hStatusLbl = createControl("STATIC", fmt.Sprintf("準備完了 (v%d)", CurrentClientVersion), 0, 575, 565, 350, 20, exp.hWndMain, 0)

	exp.updateMountBtnText()
	exp.setupTrayIcon(fmt.Sprintf("9P Client (%s)", cfg.Client.MountDrive))

	procShowWindow.Call(uintptr(exp.hWndMain), SW_SHOW)
	procUpdateWindow.Call(uintptr(exp.hWndMain))
	procSetForegroundWindow.Call(uintptr(exp.hWndMain))

	startClientTelemetryReporter(c, cfg.Client.ServerAddr)

	if c != nil {
		go exp.refreshTree()
		if cfg.Client.AutoMountOnTray {
			_ = globalMountManager.MountAsync(
				c,
				cfg.Client.MountDrive,
				cfg.Server.Disk.TotalBytes,
				cfg.Server.Disk.FreeBytes,
				cfg.Client.VolumeLabel,
				cfg.Client.MountAsNetwork,
			)
			exp.updateMountBtnText()
		}
	} else {
		exp.setStatus("サーバー待機中... 起動次第バックグラウンドで自動接続します。")
	}

	var msg MSG
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
