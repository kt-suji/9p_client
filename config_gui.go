//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"gopkg.in/yaml.v3"
)

var (
	hConfigWnd         windows.Handle
	hCfgServerAddr     windows.Handle
	hCfgMountDrive     windows.Handle
	hCfgVolLabel       windows.Handle
	hCfgAutoMount      windows.Handle
	hCfgMountNet       windows.Handle
	hCfgModel          windows.Handle
	hCfgSerial         windows.Handle
	hCfgFirmware       windows.Handle
	hCfgCapacity       windows.Handle
	hCfgUsed           windows.Handle
	hCfgMemTotal       windows.Handle
	hCfgMemPct         windows.Handle
	hCfgHealthStatus   windows.Handle
	hCfgHealthPct      windows.Handle
	hCfgSmartTemp      windows.Handle
	hCfgHours          windows.Handle
	hCfgPowerCycles    windows.Handle
	hCfgUnsafeShuts    windows.Handle
	hCfgReadsTB        windows.Handle
	hCfgWritesTB       windows.Handle
	hCfgAvailableSpare windows.Handle
	hCfgReallocated    windows.Handle
	cfgTargetObj       *ClientConfig
)

const (
	ID_CFG_SAVE   = 3001
	ID_CFG_CANCEL = 3002

	BS_AUTOCHECKBOX = 0x00000003
	BM_GETCHECK     = 0x00F0
	BM_SETCHECK     = 0x00F1
	BST_CHECKED     = 1
	BST_UNCHECKED   = 0
)

func configWndProc(hWnd windows.Handle, msg uintptr, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case WM_COMMAND:
		switch loword(uint32(wParam)) {
		case ID_CFG_SAVE:
			saveConfigFromGUI(hWnd)
			procShowWindow.Call(uintptr(hWnd), 0)
			return 0
		case ID_CFG_CANCEL:
			procShowWindow.Call(uintptr(hWnd), 0)
			return 0
		}
	case 0x0010: // WM_CLOSE
		procShowWindow.Call(uintptr(hWnd), 0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hWnd), uintptr(msg), wParam, lParam)
	return ret
}

func saveConfigFromGUI(hWnd windows.Handle) {
	addr := getWinText(hCfgServerAddr)
	drive := getWinText(hCfgMountDrive)
	volLabel := getWinText(hCfgVolLabel)
	model := getWinText(hCfgModel)
	serial := getWinText(hCfgSerial)
	firmware := getWinText(hCfgFirmware)
	capacity := getWinText(hCfgCapacity)
	memTotalStr := getWinText(hCfgMemTotal)
	memPctStr := getWinText(hCfgMemPct)
	healthStatus := getWinText(hCfgHealthStatus)
	healthPctStr := getWinText(hCfgHealthPct)
	tempStr := getWinText(hCfgSmartTemp)
	hoursStr := getWinText(hCfgHours)
	cyclesStr := getWinText(hCfgPowerCycles)
	unsafeStr := getWinText(hCfgUnsafeShuts)
	readsStr := getWinText(hCfgReadsTB)
	writesStr := getWinText(hCfgWritesTB)
	spareStr := getWinText(hCfgAvailableSpare)
	reallocStr := getWinText(hCfgReallocated)

	checkRet, _, _ := procSendMessageW.Call(uintptr(hCfgAutoMount), BM_GETCHECK, 0, 0)
	autoMount := (checkRet == BST_CHECKED)

	checkNetRet, _, _ := procSendMessageW.Call(uintptr(hCfgMountNet), BM_GETCHECK, 0, 0)
	mountAsNetwork := (checkNetRet == BST_CHECKED)

	if addr != "" {
		cfgTargetObj.Client.ServerAddr = addr
	}
	if drive != "" {
		cfgTargetObj.Client.MountDrive = drive
	}
	if volLabel != "" {
		cfgTargetObj.Client.VolumeLabel = volLabel
	}
	cfgTargetObj.Client.MountAsNetwork = mountAsNetwork
	cfgTargetObj.Client.AutoMountOnTray = autoMount

	// 容量計算 (例: 2PB, 1TB) と使用量 (例: 500TB, 92%)
	usedStr := getWinText(hCfgUsed)
	if capacity != "" {
		cfgTargetObj.Server.Disk.CapacityDisplay = capacity
		cfgTargetObj.Server.Disk.TotalBytes = ParseCapacityToBytes(capacity)
	}
	totalBytes := cfgTargetObj.Server.Disk.TotalBytes
	cfgTargetObj.Server.Disk.UsedDisplay = usedStr
	if usedStr != "" {
		cfgTargetObj.Server.Disk.FreeBytes = totalBytes - ParseUsedToBytes(usedStr, totalBytes)
	} else if capacity != "" {
		cfgTargetObj.Server.Disk.FreeBytes = totalBytes * 98 / 100
	}
	if cfgTargetObj.Server.Disk.FreeBytes > totalBytes {
		cfgTargetObj.Server.Disk.FreeBytes = totalBytes
	}
	globalMountManager.UpdateCapacity(totalBytes, cfgTargetObj.Server.Disk.FreeBytes)

	// 仮想メモリ
	if memTotalStr == "" {
		memTotalStr = "64GB"
	}
	memPct, err := strconv.ParseFloat(memPctStr, 64)
	if err != nil || memPct <= 0 {
		memPct = 28.5
	}
	cfgTargetObj.Server.Memory.TotalDisplay = memTotalStr
	cfgTargetObj.Server.Memory.UsedPercent = memPct
	cfgTargetObj.Server.Memory.VirtualMode = true

	// S.M.A.R.T.
	smart := &cfgTargetObj.Server.Disk.Smart
	if model != "" {
		smart.DeviceModel = model
	}
	if serial != "" {
		smart.SerialNumber = serial
	}
	if firmware != "" {
		smart.FirmwareVersion = firmware
	}
	if healthStatus != "" {
		smart.HealthStatus = strings.ToUpper(healthStatus)
	}
	if v, err := strconv.Atoi(healthPctStr); err == nil {
		smart.HealthPercent = v
	}
	if v, err := strconv.Atoi(tempStr); err == nil {
		smart.TemperatureC = v
	}
	if v, err := strconv.Atoi(hoursStr); err == nil {
		smart.PowerOnHours = v
	}
	if v, err := strconv.Atoi(cyclesStr); err == nil {
		smart.PowerOnCount = v
	}
	if v, err := strconv.Atoi(unsafeStr); err == nil {
		smart.UnsafeShutdowns = v
	}
	if v, err := strconv.ParseFloat(readsStr, 64); err == nil {
		smart.TotalReadsTB = v
	}
	if v, err := strconv.ParseFloat(writesStr, 64); err == nil {
		smart.TotalWritesTB = v
	}
	if v, err := strconv.Atoi(spareStr); err == nil {
		smart.AvailableSpare = v
	}
	if v, err := strconv.Atoi(reallocStr); err == nil {
		smart.ReallocatedSectors = v
	}

	// YAMLファイルを更新保存
	targetFile := "config.yaml"
	if exePath, err := os.Executable(); err == nil {
		targetFile = filepath.Join(filepath.Dir(exePath), "config.yaml")
	}

	// 既存ファイルを読み込んでマージ
	type FullConfig struct {
		Server map[string]any `yaml:"server"`
		Client map[string]any `yaml:"client"`
	}
	var full FullConfig
	if d, err := os.ReadFile(targetFile); err == nil {
		_ = yaml.Unmarshal(d, &full)
	}
	if full.Client == nil {
		full.Client = make(map[string]any)
	}
	full.Client["server_addr"] = cfgTargetObj.Client.ServerAddr
	full.Client["mount_drive"] = cfgTargetObj.Client.MountDrive
	full.Client["volume_label"] = cfgTargetObj.Client.VolumeLabel
	full.Client["mount_as_network"] = cfgTargetObj.Client.MountAsNetwork
	full.Client["auto_mount_on_tray"] = autoMount

	if full.Server == nil {
		full.Server = make(map[string]any)
	}
	diskMap, ok := full.Server["disk"].(map[string]any)
	if !ok {
		diskMap = make(map[string]any)
		full.Server["disk"] = diskMap
	}
	diskMap["capacity_display"] = cfgTargetObj.Server.Disk.CapacityDisplay
	diskMap["used_display"] = cfgTargetObj.Server.Disk.UsedDisplay
	diskMap["total_bytes"] = cfgTargetObj.Server.Disk.TotalBytes
	diskMap["free_bytes"] = cfgTargetObj.Server.Disk.FreeBytes

	// メモリ設定保存
	memMap, ok := full.Server["memory"].(map[string]any)
	if !ok {
		memMap = make(map[string]any)
		full.Server["memory"] = memMap
	}
	memMap["total_display"] = cfgTargetObj.Server.Memory.TotalDisplay
	memMap["used_percent"] = cfgTargetObj.Server.Memory.UsedPercent
	memMap["virtual_mode"] = true

	smartMap, ok := diskMap["smart"].(map[string]any)
	if !ok {
		smartMap = make(map[string]any)
		diskMap["smart"] = smartMap
	}
	smartMap["device_model"] = smart.DeviceModel
	smartMap["serial_number"] = smart.SerialNumber
	smartMap["firmware_version"] = smart.FirmwareVersion
	smartMap["health_status"] = smart.HealthStatus
	smartMap["health_percent"] = smart.HealthPercent
	smartMap["temperature_c"] = smart.TemperatureC
	smartMap["power_on_hours"] = smart.PowerOnHours
	smartMap["power_on_count"] = smart.PowerOnCount
	smartMap["unsafe_shutdowns"] = smart.UnsafeShutdowns
	smartMap["total_reads_tb"] = smart.TotalReadsTB
	smartMap["total_writes_tb"] = smart.TotalWritesTB
	smartMap["available_spare"] = smart.AvailableSpare
	smartMap["reallocated_sectors"] = smart.ReallocatedSectors

	if out, err := yaml.Marshal(&full); err == nil {
		_ = os.WriteFile(targetFile, out, 0644)
		// 親ディレクトリの config.yaml も同期更新
		parentConfig := filepath.Join(filepath.Dir(filepath.Dir(targetFile)), "config.yaml")
		_ = os.WriteFile(parentConfig, out, 0644)
		log.Printf("[CONFIG] 設定を保存しました: 容量=%s (%d bytes, 空き=%d bytes), メモリ=%s (%.1f%%)",
			cfgTargetObj.Server.Disk.CapacityDisplay, cfgTargetObj.Server.Disk.TotalBytes, cfgTargetObj.Server.Disk.FreeBytes,
			cfgTargetObj.Server.Memory.TotalDisplay, cfgTargetObj.Server.Memory.UsedPercent)
	}

	// メインエクスプローラー側のアドレス表示・ステータス更新 & 現在表示中ファイルの即時再読込
	if globalExplorer != nil {
		setControlText(globalExplorer.hEditAddr, cfgTargetObj.Client.ServerAddr)
		globalExplorer.updateMountBtnText()
		globalExplorer.setStatus("✓ 設定を保存し、仮想容量・使用量・メモリ・S.M.A.R.T.に反映しました")
		if globalExplorer.currentPath != "" {
			go globalExplorer.loadFileContent(globalExplorer.currentPath)
		}
	}

	usedBytes := uint64(0)
	if cfgTargetObj.Server.Disk.TotalBytes > cfgTargetObj.Server.Disk.FreeBytes {
		usedBytes = cfgTargetObj.Server.Disk.TotalBytes - cfgTargetObj.Server.Disk.FreeBytes
	}
	tPtr, _ := windows.UTF16PtrFromString("設定保存")
	mPtr, _ := windows.UTF16PtrFromString(fmt.Sprintf("✓ 設定を保存し反映しました！\n\n・仮想ディスク最大容量: %s (%s)\n・仮想ディスク使用中: %s (空き: %s)\n・仮想メモリ: %s (使用率 %.1f%%)\n・ボリューム名: %s",
		cfgTargetObj.Server.Disk.CapacityDisplay,
		FormatBytesHuman(cfgTargetObj.Server.Disk.TotalBytes),
		FormatBytesHuman(usedBytes),
		FormatBytesHuman(cfgTargetObj.Server.Disk.FreeBytes),
		cfgTargetObj.Server.Memory.TotalDisplay,
		cfgTargetObj.Server.Memory.UsedPercent,
		cfgTargetObj.Client.VolumeLabel))
	procMessageBoxW.Call(uintptr(hWnd), uintptr(unsafe.Pointer(mPtr)), uintptr(unsafe.Pointer(tPtr)), uintptr(MB_OK|MB_ICONINFORMATION))
}

func getWinText(hWnd windows.Handle) string {
	length, _, _ := procGetWindowTextLengthW.Call(uintptr(hWnd))
	buf := make([]uint16, length+1)
	procGetWindowTextW.Call(uintptr(hWnd), uintptr(unsafe.Pointer(&buf[0])), length+1)
	return strings.TrimSpace(windows.UTF16ToString(buf))
}

func OpenConfigDialog(cfg *ClientConfig) {
	cfgTargetObj = cfg

	if hConfigWnd != 0 {
		populateConfigFields(cfg)
		procShowWindow.Call(uintptr(hConfigWnd), SW_SHOW)
		procSetForegroundWindow.Call(uintptr(hConfigWnd))
		return
	}

	className, _ := windows.UTF16PtrFromString("9PConfigWindowClass")
	titleName, _ := windows.UTF16PtrFromString("⚙️ 9P 仮想リソース & S.M.A.R.T. 設定 (config.yaml)")
	hInstance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)

	var wc WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = syscall.NewCallback(configWndProc)
	wc.HInstance = windows.Handle(hInstance)
	wc.LpszClassName = className
	wc.HbrBackground = windows.Handle(16) // COLOR_BTNFACE + 1

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hWnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titleName)),
		WS_OVERLAPPEDWINDOW&^0x00040000&^0x00010000, // リサイズ禁止
		150, 100, 580, 535,
		0, 0, hInstance, 0,
	)

	hConfigWnd = windows.Handle(hWnd)

	// --- 1. クライアント基本設定 ---
	createControl("STATIC", "【クライアント & マウント設定】", 0, 20, 10, 520, 18, hConfigWnd, 0)

	createControl("STATIC", "サーバーアドレス:", 0, 20, 32, 120, 20, hConfigWnd, 0)
	hCfgServerAddr = createControl("EDIT", cfg.Client.ServerAddr, WS_BORDER, 150, 30, 200, 22, hConfigWnd, 0)

	createControl("STATIC", "ドライブ:", 0, 370, 32, 60, 20, hConfigWnd, 0)
	hCfgMountDrive = createControl("EDIT", cfg.Client.MountDrive, WS_BORDER, 440, 30, 100, 22, hConfigWnd, 0)

	createControl("STATIC", "エクスプローラー名:", 0, 20, 58, 120, 20, hConfigWnd, 0)
	hCfgVolLabel = createControl("EDIT", cfg.Client.VolumeLabel, WS_BORDER, 150, 56, 200, 22, hConfigWnd, 0)

	hCfgAutoMount = createControl("BUTTON", "起動時自動マウント", BS_AUTOCHECKBOX, 370, 56, 170, 22, hConfigWnd, 0)
	hCfgMountNet = createControl("BUTTON", "ネットワークの場所として表示 (UNC: \\\\9p\\drive)", BS_AUTOCHECKBOX, 150, 82, 380, 20, hConfigWnd, 0)

	// --- 2. 仮想ディスク & メモリ仕様 ---
	createControl("STATIC", "【仮想リソース仕様 (ディスク容量 & メモリ)】", 0, 20, 108, 520, 18, hConfigWnd, 0)

	createControl("STATIC", "ドライブモデル名:", 0, 20, 130, 120, 20, hConfigWnd, 0)
	hCfgModel = createControl("EDIT", cfg.Server.Disk.Smart.DeviceModel, WS_BORDER, 150, 128, 390, 22, hConfigWnd, 0)

	createControl("STATIC", "シリアルナンバー:", 0, 20, 156, 120, 20, hConfigWnd, 0)
	hCfgSerial = createControl("EDIT", cfg.Server.Disk.Smart.SerialNumber, WS_BORDER, 150, 154, 200, 22, hConfigWnd, 0)

	createControl("STATIC", "ファームウェア:", 0, 370, 156, 90, 20, hConfigWnd, 0)
	hCfgFirmware = createControl("EDIT", cfg.Server.Disk.Smart.FirmwareVersion, WS_BORDER, 465, 154, 75, 22, hConfigWnd, 0)

	createControl("STATIC", "ディスク容量 (例:2000PB):", 0, 20, 182, 145, 20, hConfigWnd, 0)
	hCfgCapacity = createControl("EDIT", cfg.Server.Disk.CapacityDisplay, WS_BORDER, 165, 180, 90, 22, hConfigWnd, 0)

	createControl("STATIC", "ディスク使用量 (例:500PB/50%):", 0, 270, 182, 175, 20, hConfigWnd, 0)
	hCfgUsed = createControl("EDIT", cfg.Server.Disk.UsedDisplay, WS_BORDER, 445, 180, 95, 22, hConfigWnd, 0)

	createControl("STATIC", "仮想メモリ (例: 64GB):", 0, 20, 208, 140, 20, hConfigWnd, 0)
	hCfgMemTotal = createControl("EDIT", cfg.Server.Memory.TotalDisplay, WS_BORDER, 165, 206, 90, 22, hConfigWnd, 0)

	createControl("STATIC", "メモリ使用率 (%):", 0, 280, 208, 110, 20, hConfigWnd, 0)
	hCfgMemPct = createControl("EDIT", fmt.Sprintf("%.1f", cfg.Server.Memory.UsedPercent), WS_BORDER, 395, 206, 60, 22, hConfigWnd, 0)
	createControl("STATIC", "%", 0, 460, 208, 20, 20, hConfigWnd, 0)

	// --- 3. S.M.A.R.T. ヘルス・寿命・運用データ ---
	createControl("STATIC", "【S.M.A.R.T. 仮想テレメトリ設定 (CrystalDiskInfo連携)】", 0, 20, 238, 520, 20, hConfigWnd, 0)

	createControl("STATIC", "健康状態 (GOOD/WARNING/CRIT):", 0, 20, 262, 180, 20, hConfigWnd, 0)
	hCfgHealthStatus = createControl("EDIT", cfg.Server.Disk.Smart.HealthStatus, WS_BORDER, 205, 260, 80, 22, hConfigWnd, 0)

	createControl("STATIC", "残り寿命 (%):", 0, 320, 262, 100, 20, hConfigWnd, 0)
	hCfgHealthPct = createControl("EDIT", fmt.Sprintf("%d", cfg.Server.Disk.Smart.HealthPercent), WS_BORDER, 420, 260, 70, 22, hConfigWnd, 0)

	createControl("STATIC", "仮想温度 (℃):", 0, 20, 290, 180, 20, hConfigWnd, 0)
	hCfgSmartTemp = createControl("EDIT", fmt.Sprintf("%d", cfg.Server.Disk.Smart.TemperatureC), WS_BORDER, 205, 288, 80, 22, hConfigWnd, 0)

	createControl("STATIC", "使用時間 (通電時間):", 0, 20, 318, 180, 20, hConfigWnd, 0)
	hCfgHours = createControl("EDIT", fmt.Sprintf("%d", cfg.Server.Disk.Smart.PowerOnHours), WS_BORDER, 205, 316, 80, 22, hConfigWnd, 0)

	createControl("STATIC", "電源投入回数 (回):", 0, 320, 318, 110, 20, hConfigWnd, 0)
	hCfgPowerCycles = createControl("EDIT", fmt.Sprintf("%d", cfg.Server.Disk.Smart.PowerOnCount), WS_BORDER, 435, 316, 70, 22, hConfigWnd, 0)

	createControl("STATIC", "不安全な電源切断回数:", 0, 20, 346, 180, 20, hConfigWnd, 0)
	hCfgUnsafeShuts = createControl("EDIT", fmt.Sprintf("%d", cfg.Server.Disk.Smart.UnsafeShutdowns), WS_BORDER, 205, 344, 80, 22, hConfigWnd, 0)

	createControl("STATIC", "代替処理済セクタ数:", 0, 320, 346, 120, 20, hConfigWnd, 0)
	hCfgReallocated = createControl("EDIT", fmt.Sprintf("%d", cfg.Server.Disk.Smart.ReallocatedSectors), WS_BORDER, 445, 344, 60, 22, hConfigWnd, 0)

	createControl("STATIC", "ホスト総読込量 (TB):", 0, 20, 374, 180, 20, hConfigWnd, 0)
	hCfgReadsTB = createControl("EDIT", fmt.Sprintf("%.1f", cfg.Server.Disk.Smart.TotalReadsTB), WS_BORDER, 205, 372, 80, 22, hConfigWnd, 0)

	createControl("STATIC", "ホスト総書込量 (TB):", 0, 320, 374, 120, 20, hConfigWnd, 0)
	hCfgWritesTB = createControl("EDIT", fmt.Sprintf("%.1f", cfg.Server.Disk.Smart.TotalWritesTB), WS_BORDER, 445, 372, 60, 22, hConfigWnd, 0)

	createControl("STATIC", "予備領域残量 (%):", 0, 20, 402, 180, 20, hConfigWnd, 0)
	hCfgAvailableSpare = createControl("EDIT", fmt.Sprintf("%d", cfg.Server.Disk.Smart.AvailableSpare), WS_BORDER, 205, 400, 80, 22, hConfigWnd, 0)

	// --- アクションボタン ---
	createControl("BUTTON", "💾 設定を保存して適用", 0, 130, 445, 180, 34, hConfigWnd, ID_CFG_SAVE)
	createControl("BUTTON", "キャンセル", 0, 330, 445, 100, 34, hConfigWnd, ID_CFG_CANCEL)

	populateConfigFields(cfg)

	procShowWindow.Call(uintptr(hConfigWnd), SW_SHOW)
	procUpdateWindow.Call(uintptr(hConfigWnd))
	procSetForegroundWindow.Call(uintptr(hConfigWnd))
}

func populateConfigFields(cfg *ClientConfig) {
	setControlText(hCfgServerAddr, cfg.Client.ServerAddr)
	setControlText(hCfgMountDrive, cfg.Client.MountDrive)
	setControlText(hCfgVolLabel, cfg.Client.VolumeLabel)
	setControlText(hCfgModel, cfg.Server.Disk.Smart.DeviceModel)
	setControlText(hCfgSerial, cfg.Server.Disk.Smart.SerialNumber)
	setControlText(hCfgFirmware, cfg.Server.Disk.Smart.FirmwareVersion)
	setControlText(hCfgCapacity, cfg.Server.Disk.CapacityDisplay)
	usedDisp := cfg.Server.Disk.UsedDisplay
	if usedDisp == "" && cfg.Server.Disk.TotalBytes > 0 && cfg.Server.Disk.TotalBytes >= cfg.Server.Disk.FreeBytes {
		usedPct := float64(cfg.Server.Disk.TotalBytes-cfg.Server.Disk.FreeBytes) * 100.0 / float64(cfg.Server.Disk.TotalBytes)
		usedDisp = fmt.Sprintf("%.1f%%", usedPct)
	}
	setControlText(hCfgUsed, usedDisp)
	if cfg.Server.Memory.TotalDisplay == "" {
		cfg.Server.Memory.TotalDisplay = "64GB"
		cfg.Server.Memory.UsedPercent = 28.5
	}
	setControlText(hCfgMemTotal, cfg.Server.Memory.TotalDisplay)
	setControlText(hCfgMemPct, fmt.Sprintf("%.1f", cfg.Server.Memory.UsedPercent))
	setControlText(hCfgHealthStatus, cfg.Server.Disk.Smart.HealthStatus)
	setControlText(hCfgHealthPct, fmt.Sprintf("%d", cfg.Server.Disk.Smart.HealthPercent))
	setControlText(hCfgSmartTemp, fmt.Sprintf("%d", cfg.Server.Disk.Smart.TemperatureC))
	setControlText(hCfgHours, fmt.Sprintf("%d", cfg.Server.Disk.Smart.PowerOnHours))
	setControlText(hCfgPowerCycles, fmt.Sprintf("%d", cfg.Server.Disk.Smart.PowerOnCount))
	setControlText(hCfgUnsafeShuts, fmt.Sprintf("%d", cfg.Server.Disk.Smart.UnsafeShutdowns))
	setControlText(hCfgReadsTB, fmt.Sprintf("%.1f", cfg.Server.Disk.Smart.TotalReadsTB))
	setControlText(hCfgWritesTB, fmt.Sprintf("%.1f", cfg.Server.Disk.Smart.TotalWritesTB))
	setControlText(hCfgAvailableSpare, fmt.Sprintf("%d", cfg.Server.Disk.Smart.AvailableSpare))
	setControlText(hCfgReallocated, fmt.Sprintf("%d", cfg.Server.Disk.Smart.ReallocatedSectors))

	checkVal := BST_UNCHECKED
	if cfg.Client.AutoMountOnTray {
		checkVal = BST_CHECKED
	}
	procSendMessageW.Call(uintptr(hCfgAutoMount), BM_SETCHECK, uintptr(checkVal), 0)

	checkNetVal := BST_UNCHECKED
	if cfg.Client.MountAsNetwork {
		checkNetVal = BST_CHECKED
	}
	procSendMessageW.Call(uintptr(hCfgMountNet), BM_SETCHECK, uintptr(checkNetVal), 0)
}
