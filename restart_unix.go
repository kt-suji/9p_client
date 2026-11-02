//go:build !windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
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
		argsStr += fmt.Sprintf(" %q", arg)
	}

	cmdStr := fmt.Sprintf("sleep 2 && exec %q%s &", exePath, argsStr)
	cmd := exec.Command("sh", "-c", cmdStr)
	if err := cmd.Start(); err != nil {
		log.Printf("[AUTO-UPDATE] 再起動スクリプト起動失敗: %v\n", err)
	}
	os.Exit(0)
}
