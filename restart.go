package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

var (
	restartMu          sync.Mutex
	isRestarting       bool
	lastRestartAttempt time.Time
)

// triggerClientAutoRestart はサーバーからの新バージョン要求を受信した際に、安全にアンマウントして自動再起動します。
func triggerClientAutoRestart(reqVer int64) {
	restartMu.Lock()
	if isRestarting {
		restartMu.Unlock()
		return
	}
	// Google Driveの同期待ちなどを考慮し、15秒以内の連続再起動を防止
	if time.Since(lastRestartAttempt) < 15*time.Second {
		restartMu.Unlock()
		return
	}
	isRestarting = true
	lastRestartAttempt = time.Now()
	restartMu.Unlock()

	log.Printf("[AUTO-UPDATE] ★ サーバーから新バージョン要求 (要求: v%d / 現在: v%d) を検知しました\n", reqVer, CurrentClientVersion)
	log.Printf("[AUTO-UPDATE] 3秒後にドライブを安全にアンマウントし、最新バイナリで自動再起動します...\n")

	if globalExplorer != nil {
		globalExplorer.setStatus(fmt.Sprintf("⚡ サーバーが新バージョン (v%d) を要求中。3秒後に自動更新再起動します...", reqVer))
	}

	go func() {
		time.Sleep(3 * time.Second)

		// 1. ドライブのマウント解除 (WinFspリソースの確実なクリーンアップ)
		if globalMountManager.IsMounted() {
			log.Println("[AUTO-UPDATE] P: ドライブをアンマウント中...")
			_ = globalMountManager.Unmount()
			time.Sleep(600 * time.Millisecond)
		}

		// 2. GUIトレイアイコンの削除
		if globalExplorer != nil {
			globalExplorer.removeTrayIcon()
		}

		// 3. プラットフォーム別の自己再起動
		performSelfRestart()
	}()
}
