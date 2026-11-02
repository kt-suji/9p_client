//go:build !windows

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/knusbaum/go9p/client"
)

type MountManager struct {
	mu         sync.Mutex
	mounted    bool
	mountPoint string
}

var globalMountManager = &MountManager{}

func (m *MountManager) IsMounted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mounted
}

func (m *MountManager) UpdateClient(newClient *client.Client) {
	// Linux では GUI / CUI が直接 9P Client を保持するため no-op
}

func (m *MountManager) MountAsync(c *client.Client, drive string, totalBytes, freeBytes uint64, volumeLabel string, asNetwork bool) error {
	return fmt.Errorf("Linux環境では CUI / GUI から直接全9Pファイルを読み書きできます。OSディレクトリへマウントする場合は 'sudo mount -t 9p -o trans=tcp,port=<port> <ip> /mnt/9p' を使用してください")
}

func (m *MountManager) Unmount() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.mounted {
		return nil
	}
	cmd := exec.Command("umount", m.mountPoint)
	_ = cmd.Run()
	m.mounted = false
	return nil
}

func StartMountBlocking(c *client.Client, mountPoint string, totalBytes, freeBytes uint64, volumeLabel string, asNetwork bool) {
	_ = os.MkdirAll(mountPoint, 0755)
	cfg := loadClientConfig("config.yaml")
	host, port, err := net.SplitHostPort(cfg.Client.ServerAddr)
	if err != nil {
		host = cfg.Client.ServerAddr
		port = "5640"
	}
	log.Printf("[MOUNT] Linux カーネル 9p ドライバ (v9fs) で %s へのマウントを試行します...\n", mountPoint)
	cmd := exec.Command("mount", "-t", "9p", "-o", fmt.Sprintf("trans=tcp,port=%s,version=9p2000", port), host, mountPoint)
	out, mErr := cmd.CombinedOutput()
	if mErr != nil {
		log.Printf("[WARN] カーネル 9p マウント失敗 (%v: %s)\n", mErr, string(out))
		log.Printf("[INFO] Linux では root 権限なしで使える内蔵 CUI / GUI エクスプローラーへ自動で切り替えます。\n")
		runCUIControlCenter(c, cfg)
		return
	}
	globalMountManager.mu.Lock()
	globalMountManager.mounted = true
	globalMountManager.mountPoint = mountPoint
	globalMountManager.mu.Unlock()

	log.Printf("[MOUNT] %s にマウント完了！ (終了は Ctrl+C)\n", mountPoint)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	_ = globalMountManager.Unmount()
}
