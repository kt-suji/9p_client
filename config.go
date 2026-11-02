package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type SmartConfig struct {
	DeviceModel        string  `yaml:"device_model"`
	SerialNumber       string  `yaml:"serial_number"`
	FirmwareVersion    string  `yaml:"firmware_version"`
	Interface          string  `yaml:"interface"`
	TransferMode       string  `yaml:"transfer_mode"`
	HealthStatus       string  `yaml:"health_status"`
	HealthPercent      int     `yaml:"health_percent"`
	TemperatureC       int     `yaml:"temperature_c"`
	PowerOnHours       int     `yaml:"power_on_hours"`
	PowerOnCount       int     `yaml:"power_on_count"`
	UnsafeShutdowns    int     `yaml:"unsafe_shutdowns"`
	TotalReadsTB       float64 `yaml:"total_reads_tb"`
	TotalWritesTB      float64 `yaml:"total_writes_tb"`
	AvailableSpare     int     `yaml:"available_spare"`
	ReallocatedSectors int     `yaml:"reallocated_sectors"`
}

type MemoryConfig struct {
	TotalDisplay string  `yaml:"total_display"`
	UsedPercent  float64 `yaml:"used_percent"`
	VirtualMode  bool    `yaml:"virtual_mode"`
}

type ClientConfig struct {
	Client struct {
		ServerAddr      string `yaml:"server_addr"`
		MountDrive      string `yaml:"mount_drive"`
		VolumeLabel     string `yaml:"volume_label"`
		MountAsNetwork  bool   `yaml:"mount_as_network"`
		AutoMountOnTray bool   `yaml:"auto_mount_on_tray"`
		GUIPort         int    `yaml:"gui_port"`
	} `yaml:"client"`
	Server struct {
		Disk struct {
			CapacityDisplay string      `yaml:"capacity_display"`
			UsedDisplay     string      `yaml:"used_display"` // 使用量 (例: 500TB, 92%) 空なら free_bytes をそのまま使う
			TotalBytes      uint64      `yaml:"total_bytes"`
			FreeBytes       uint64      `yaml:"free_bytes"`
			Smart           SmartConfig `yaml:"smart"`
		} `yaml:"disk"`
		Memory MemoryConfig `yaml:"memory"`
	} `yaml:"server"`
}

func ParseCapacityToBytes(str string) uint64 {
	str = strings.TrimSpace(str)
	if str == "" {
		return 1000 * 1024 * 1024 * 1024 // 1000GB (1TB) default
	}
	upper := strings.ToUpper(str)
	// デフォルト単位はGB (1000 = 1000GB = 1TB, 0.1 = 100MB, 2000000 = 2PB)
	var multiplier uint64 = 1024 * 1024 * 1024
	var numStr string

	if strings.HasSuffix(upper, "PB") || strings.HasSuffix(upper, "PIB") {
		multiplier = 1024 * 1024 * 1024 * 1024 * 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(upper, "PIB"), "PB"))
	} else if strings.HasSuffix(upper, "P") {
		multiplier = 1024 * 1024 * 1024 * 1024 * 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(upper, "P"))
	} else if strings.HasSuffix(upper, "TB") || strings.HasSuffix(upper, "TIB") {
		multiplier = 1024 * 1024 * 1024 * 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(upper, "TIB"), "TB"))
	} else if strings.HasSuffix(upper, "T") {
		multiplier = 1024 * 1024 * 1024 * 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(upper, "T"))
	} else if strings.HasSuffix(upper, "GB") || strings.HasSuffix(upper, "GIB") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(upper, "GIB"), "GB"))
	} else if strings.HasSuffix(upper, "G") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(upper, "G"))
	} else if strings.HasSuffix(upper, "MB") || strings.HasSuffix(upper, "MIB") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(upper, "MIB"), "MB"))
	} else if strings.HasSuffix(upper, "M") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(upper, "M"))
	} else if strings.HasSuffix(upper, "KB") || strings.HasSuffix(upper, "KIB") {
		multiplier = 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(upper, "KIB"), "KB"))
	} else if strings.HasSuffix(upper, "K") {
		multiplier = 1024
		numStr = strings.TrimSpace(strings.TrimSuffix(upper, "K"))
	} else {
		numStr = upper
	}

	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil || val <= 0 {
		return 1000 * 1024 * 1024 * 1024 // 1000GB default
	}
	return uint64(val * float64(multiplier))
}

func FormatBytesHuman(b uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
		PB = 1024 * TB
	)
	switch {
	case b >= PB:
		return fmt.Sprintf("%.2f PB", float64(b)/float64(PB))
	case b >= TB:
		return fmt.Sprintf("%.2f TB", float64(b)/float64(TB))
	case b >= GB:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(MB))
	default:
		return fmt.Sprintf("%d bytes", b)
	}
}

func loadClientConfig(defaultFileName string) *ClientConfig {
	cfg := &ClientConfig{}
	cfg.Client.ServerAddr = "winserver:5640"
	cfg.Client.MountDrive = "P:"
	cfg.Client.VolumeLabel = "9P Virtual SSD"
	cfg.Client.MountAsNetwork = false
	cfg.Client.AutoMountOnTray = false
	cfg.Server.Disk.CapacityDisplay = "1PB"
	cfg.Server.Disk.TotalBytes = 1125899906842624 // 1PB
	cfg.Server.Disk.FreeBytes = 1099511627776000
	cfg.Server.Disk.Smart = SmartConfig{
		DeviceModel:        "Samsung SSD 980 PRO 1TB (Virtual 9P Volume)",
		SerialNumber:       "S5GXNF0R123456Z",
		FirmwareVersion:    "5B2QGXA7",
		Interface:          "NVM Express (9P2000.L Remote)",
		TransferMode:       "PCIe 4.0 x4 | PCIe 4.0 x4",
		HealthStatus:       "GOOD",
		HealthPercent:      98,
		TemperatureC:       36,
		PowerOnHours:       2450,
		PowerOnCount:       318,
		UnsafeShutdowns:    12,
		TotalReadsTB:       18.5,
		TotalWritesTB:      14.2,
		AvailableSpare:     100,
		ReallocatedSectors: 0,
	}
	cfg.Server.Memory = MemoryConfig{
		TotalDisplay: "64GB",
		UsedPercent:  28.5,
		VirtualMode:  true,
	}

	candidates := []string{
		defaultFileName,
		"../" + defaultFileName,
	}

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append([]string{filepath.Join(exeDir, defaultFileName), filepath.Join(exeDir, "..", defaultFileName)}, candidates...)
	}

	var loadedData []byte
	var loadedPath string
	for _, p := range candidates {
		data, err := os.ReadFile(p)
		if err == nil {
			loadedData = data
			loadedPath = p
			break
		}
	}

	if len(loadedData) > 0 {
		if err := yaml.Unmarshal(loadedData, cfg); err != nil {
			log.Printf("[WARN] %s のパース失敗: %v (デフォルト値を使用)", loadedPath, err)
		} else {
			log.Printf("[CONFIG] 設定ファイルを読み込みました: %s (接続先: %s, マウント先: %s)", loadedPath, cfg.Client.ServerAddr, cfg.Client.MountDrive)
		}
	} else {
		log.Println("[INFO] config.yaml が見つからないためデフォルト設定で起動します (接続先: winserver:5640)")
	}

	// capacity_display から total_bytes を自動補完
	if cfg.Server.Disk.CapacityDisplay != "" {
		calcBytes := ParseCapacityToBytes(cfg.Server.Disk.CapacityDisplay)
		if calcBytes > 0 {
			cfg.Server.Disk.TotalBytes = calcBytes
			if cfg.Server.Disk.UsedDisplay == "" && (cfg.Server.Disk.FreeBytes == 0 || cfg.Server.Disk.FreeBytes == 1099511627776000) {
				cfg.Server.Disk.FreeBytes = calcBytes * 98 / 100
			}
		}
	}

	// used_display (使用量) が指定されていれば空き容量を逆算
	if cfg.Server.Disk.UsedDisplay != "" {
		used := ParseUsedToBytes(cfg.Server.Disk.UsedDisplay, cfg.Server.Disk.TotalBytes)
		cfg.Server.Disk.FreeBytes = cfg.Server.Disk.TotalBytes - used
	}
	if cfg.Server.Disk.FreeBytes > cfg.Server.Disk.TotalBytes {
		cfg.Server.Disk.FreeBytes = cfg.Server.Disk.TotalBytes
	}

	return cfg
}

// ParseUsedToBytes は使用量の文字列を最大容量に対するバイト数に変換します。
// "92%" のような割合指定と、"500TB" / "1000"(GB) のような容量指定の両方に対応し、最大容量を超えないよう丸めます。
func ParseUsedToBytes(str string, total uint64) uint64 {
	str = strings.TrimSpace(str)
	if str == "" {
		return 0
	}
	var used uint64
	if strings.HasSuffix(str, "%") {
		pct, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(str, "%")), 64)
		if err != nil || pct < 0 {
			return 0
		}
		if pct > 100 {
			pct = 100
		}
		used = uint64(float64(total) * pct / 100)
	} else if v, err := strconv.ParseFloat(str, 64); err == nil && v == 0 {
		used = 0
	} else {
		used = ParseCapacityToBytes(str)
	}
	if used > total {
		used = total
	}
	return used
}
