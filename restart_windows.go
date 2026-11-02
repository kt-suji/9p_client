//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
)

func performSelfRestart() {
	exePath, err := os.Executable()
	if err != nil {
		log.Printf("[AUTO-UPDATE] 実行ファイルパス取得失敗: %v\n", err)
		os.Exit(1)
	}

	log.Printf("[AUTO-UPDATE] 新バージョンで自己再起動します: %s\n", exePath)

	var argsStr string
	for _, arg := range os.Args[1:] {
		argsStr += fmt.Sprintf(" \"%s\"", arg)
	}

	// 2秒待機後に最新の exePath をバックグラウンド起動
	// これにより、旧プロセスが完全に終了して排他ミューテックスやソケットが解放されてから新プロセスが立ち上がる
	cmdStr := fmt.Sprintf("timeout /t 2 /nobreak >nul & start \"\" \"%s\"%s", exePath, argsStr)
	cmd := exec.Command("cmd.exe", "/c", cmdStr)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[AUTO-UPDATE] 再起動スクリプト起動失敗: %v\n", err)
	}
	os.Exit(0)
}
