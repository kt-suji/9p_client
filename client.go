package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/knusbaum/go9p/client"
)

// defaultStartMode はビルド時の -ldflags="-X main.defaultStartMode=gui" で切り替え可能。
// - 9p_client.exe / 9p_client_linux     : "cui" (デフォルトで番号選択式 CUI コントロールセンターを起動)
// - 9p_client_gui.exe / 9p_client_gui_linux : "gui" (デフォルトで GUI エクスプローラーを起動)
var defaultStartMode = "cui"

func main() {
	runtime.LockOSThread()

	// 実行ファイル名に "gui" が含まれている場合は自動的に GUI モードをデフォルトにする
	if len(os.Args) > 0 && strings.Contains(strings.ToLower(os.Args[0]), "gui") {
		defaultStartMode = "gui"
	}

	cfg := loadClientConfig("config.yaml")

	flagAddr := flag.String("addr", "", "サーバーアドレス (例: winserver:5640)")
	flagMount := flag.String("mount", "", "指定ドライブ/パスにフォアグラウンドでマウント (例: P: または /mnt/9p)")
	flagAgent := flag.Bool("agent", false, "ヘッドレスエージェントモードで常駐 (心拍・ベンチ・仮想ターミナル待受)")
	flagGUI := flag.Bool("gui", false, "GUIエクスプローラーを起動")
	flagCUI := flag.Bool("cui", false, "番号選択式 CUI コントロールセンターを起動")
	flagTray := flag.Bool("tray", false, "タスクトレイ常駐モードで起動 (GUI)")
	flag.Parse()

	targetAddr := cfg.Client.ServerAddr
	if *flagAddr != "" {
		targetAddr = *flagAddr
	}
	cfg.Client.ServerAddr = targetAddr

	// CLIサブコマンド判定 (cat, write, ls, shell, bench, exec, status, cui)
	args := flag.Args()
	if len(args) > 0 {
		cmd := strings.ToLower(args[0])

		conn, err := net.DialTimeout("tcp", targetAddr, 3*time.Second)
		if err != nil {
			log.Fatalf("[ERROR] サーバー接続エラー (%s): %v\n", targetAddr, err)
		}
		defer conn.Close()

		c, err := client.NewClient(conn, "user", "")
		if err != nil {
			log.Fatalf("[ERROR] 9P初期化エラー: %v\n", err)
		}

		switch cmd {
		case "cat":
			if len(args) < 2 {
				fmt.Println("使用法: 9p_client cat <path>")
				return
			}
			runCat(c, args[1])
			return

		case "write", "echo":
			if len(args) < 3 {
				fmt.Println("使用法: 9p_client write <path> <content>")
				return
			}
			runWrite(c, args[1], strings.Join(args[2:], " ")+"\n")
			return

		case "ls":
			path := ""
			if len(args) > 1 {
				path = args[1]
			}
			runLs(c, path)
			return

		case "status":
			runCat(c, "cluster/status")
			return

		case "exec":
			if len(args) < 3 {
				fmt.Println("使用法: 9p_client exec <server|client_hostname> <command>")
				return
			}
			target := strings.ToLower(strings.TrimSpace(args[1]))
			execPath := "server/ctl/exec"
			if target != "server" {
				execPath = fmt.Sprintf("client/%s/ctl/exec", strings.TrimPrefix(target, "client/"))
			}
			cmdStr := strings.Join(args[2:], " ")
			runWrite(c, execPath, cmdStr+"\n")
			time.Sleep(1500 * time.Millisecond)
			runCat(c, execPath)
			return

		case "shell":
			runInteractiveShell(c)
			return

		case "cui":
			runCUIControlCenter(c, cfg)
			return

		case "bench":
			runDefaultBenchTest(c)
			return

		default:
			fmt.Printf("未知のサブコマンド: %s\n", cmd)
			fmt.Println("利用可能: status, bench, exec, cui, shell, cat, write, ls")
			return
		}
	}

	// ヘッドレスエージェント指定時
	if *flagAgent {
		c, _ := dial9PClient(targetAddr, 2*time.Second)
		startClientTelemetryReporter(c, targetAddr)
		log.Printf("[AGENT] ヘッドレスエージェント稼働中 (接続先: %s)... (終了は Ctrl+C)\n", targetAddr)
		select {}
	}

	// フォアグラウンドマウント指定時
	if *flagMount != "" {
		c, err := dial9PClient(targetAddr, 3*time.Second)
		if err != nil {
			log.Fatalf("[ERROR] サーバー接続エラー: %v\n", err)
		}
		startClientTelemetryReporter(c, targetAddr)
		StartMountBlocking(
			c,
			*flagMount,
			cfg.Server.Disk.TotalBytes,
			cfg.Server.Disk.FreeBytes,
			cfg.Client.VolumeLabel,
			cfg.Client.MountAsNetwork,
		)
		return
	}

	// 起動モード決定 (CUI vs GUI)
	mode := defaultStartMode
	if *flagGUI || *flagTray {
		mode = "gui"
	}
	if *flagCUI {
		mode = "cui"
	}

	c, err := dial9PClient(targetAddr, 2*time.Second)
	if err != nil {
		log.Printf("[WARN] サーバー (%s) への初回接続保留: %v (バックグラウンドで自動再接続します)\n", targetAddr, err)
	} else {
		log.Printf("[INFO] サーバー (%s) に正常接続しました\n", targetAddr)
	}

	if mode == "gui" {
		ensureSingleInstance("Local\\9P_Client_Exclusive_Instance_2240161")
		RunNativeApplication(c, cfg)
		return
	}

	// CUI版 (9p_client.exe / 9p_client_linux) は番号選択式 CUI コントロールセンターを起動
	runCUIControlCenter(c, cfg)
}
