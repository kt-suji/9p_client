package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/proto"
)

func read9PFileText(c *client.Client, path string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("サーバー未接続です")
	}
	cleanPath := strings.TrimPrefix(path, "/")
	f, err := c.Open(cleanPath, proto.Oread)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func runCat(c *client.Client, path string) {
	txt, err := read9PFileText(c, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %s を開けません: %v\n", path, err)
		return
	}
	fmt.Print(txt)
}

func runWrite(c *client.Client, path string, content string) {
	if c == nil {
		fmt.Fprintln(os.Stderr, "エラー: サーバー未接続です")
		return
	}
	cleanPath := strings.TrimPrefix(path, "/")
	f, err := c.Open(cleanPath, proto.Owrite)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %s を書き込みモードで開けません: %v\n", path, err)
		return
	}
	defer f.Close()

	n, err := f.Write([]byte(content))
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: 書き込み失敗: %v\n", err)
		return
	}
	log.Printf("[SUCCESS] %d バイトを %s に書き込みました\n", n, path)
}

func runLs(c *client.Client, path string) {
	if c == nil {
		fmt.Fprintln(os.Stderr, "エラー: サーバー未接続です")
		return
	}
	cleanPath := strings.TrimPrefix(path, "/")
	entries, err := c.Readdir(cleanPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %s の一覧取得失敗: %v\n", path, err)
		return
	}

	fmt.Printf("=== Directory: /%s ===\n", cleanPath)
	for _, e := range entries {
		typeChar := "-"
		if e.Mode&proto.DMDIR != 0 {
			typeChar = "d"
		}
		fmt.Printf("%s %10d %s\n", typeChar, e.Length, e.Name)
	}
}

func runDefaultBenchTest(c *client.Client) {
	fmt.Println("==================================================")
	fmt.Println("  9P Multi-Core CPU Benchmark Runner              ")
	fmt.Println("==================================================")
	fmt.Println("\n[1] 現在のクラスタ稼働ステータス:")
	runCat(c, "cluster/status")

	fmt.Println("\n[2] 全ノード一斉ベンチマーク開始要求を送信 (5秒間)...")
	runWrite(c, "cluster/bench", "start all 5\n")

	fmt.Println("\n[3] 実行中の進捗監視:")
	for i := 1; i <= 6; i++ {
		time.Sleep(1 * time.Second)
		fmt.Printf("--- [%d/5 秒] --- ", i)
		runCat(c, "server/sys/cpu")
	}

	fmt.Println("\n[4] ベンチマーク計測完了後のスコア比較表:")
	runCat(c, "cluster/bench")
	fmt.Println("==================================================")
}

// collectOnlineClientNames は /client ディレクトリ配下のホスト名一覧を取得します。
func collectOnlineClientNames(c *client.Client) []string {
	if c == nil {
		return nil
	}
	entries, err := c.Readdir("client")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.Mode&proto.DMDIR != 0 {
			names = append(names, e.Name)
		}
	}
	sort.Strings(names)
	return names
}

// runCUIControlCenter は番号選択式の CUI コントロールセンターを起動します。
func runCUIControlCenter(initialClient *client.Client, cfg *ClientConfig) {
	currClient := initialClient
	setAutoReconnectHook(func(newC *client.Client, serverAddr string) {
		currClient = newC
	})
	startClientTelemetryReporter(currClient, cfg.Client.ServerAddr)

	scanner := bufio.NewScanner(os.Stdin)
	readLine := func(prompt string) string {
		fmt.Print(prompt)
		if !scanner.Scan() {
			return "0"
		}
		return strings.TrimSpace(scanner.Text())
	}

	getClient := func() *client.Client {
		if active := getActiveTelemetryClient(); active != nil {
			currClient = active
		}
		return currClient
	}

	for {
		c := getClient()
		connState := "ONLINE"
		if c == nil {
			connState = "未接続 (自動再接続待機中...)"
		}
		fmt.Println("\n======================================================================")
		fmt.Printf("  9P Cluster CUI Control Center  [接続先: %s | %s]\n", cfg.Client.ServerAddr, connState)
		fmt.Printf("  自ノード名: %s\n", getLocalNormalizedHostname())
		fmt.Println("======================================================================")
		fmt.Println("  [1] 📊 クラスタ稼働ステータス一覧 (/cluster/status)")
		fmt.Println("  [2] 🔥 ベンチマーク比較表・一括起動 (/cluster/bench)")
		fmt.Println("  [3] 💻 仮想ターミナル実行 (ctl/exec で対象PCにコマンド送信)")
		fmt.Println("  [4] 📂 仮想ファイルツリー閲覧・書き込み (番号選択ブラウザ)")
		fmt.Println("  [5] 📜 システムログ・ベンチマーク履歴表示 (/server/log/*)")
		fmt.Println("  [6] ⚙️ 接続先サーバー変更 / 手動再接続")
		fmt.Println("  [7] ⌨️ 対話コマンドシェルモード (9p> )")
		fmt.Println("  [0] 終了 (Exit)")
		fmt.Println("----------------------------------------------------------------------")

		choice := readLine("番号を選択してください (0-7): ")
		switch choice {
		case "0", "exit", "quit", "q":
			fmt.Println("CUI Control Center を終了します。")
			return

		case "1":
			fmt.Println()
			runCat(getClient(), "cluster/status")
			_ = readLine("\n[Enter でメニューに戻る]")

		case "2":
			c := getClient()
			if c == nil {
				fmt.Println("⚠️ サーバーに未接続です。[6] から接続先を確認してください。")
				continue
			}
			fmt.Println()
			runCat(c, "cluster/bench")
			clients := collectOnlineClientNames(c)
			fmt.Println("\n--- [ベンチマーク対象の選択 (Enterのみでスキップして戻る)] ---")
			fmt.Println("  [1] all    (サーバー＋全オンラインクライアントを一斉計測)")
			fmt.Println("  [2] server (サーバー単体を計測)")
			for idx, h := range clients {
				fmt.Printf("  [%d] %s (クライアント単体を計測)\n", idx+3, h)
			}
			tSel := readLine("対象番号または対象名を入力 (0またはEnter=キャンセル): ")
			if tSel == "" || tSel == "0" {
				continue
			}
			target := "all"
			if tSel == "1" {
				target = "all"
			} else if tSel == "2" {
				target = "server"
			} else if n, err := strconv.Atoi(tSel); err == nil && n >= 3 && n-3 < len(clients) {
				target = clients[n-3]
			} else {
				target = tSel
			}

			secStr := readLine("計測秒数を入力 (1〜60秒, 省略時=5): ")
			sec := 5
			if s, err := strconv.Atoi(secStr); err == nil && s > 0 {
				if s > 60 {
					s = 60
				}
				sec = s
			}

			cmd := fmt.Sprintf("start %s %d\n", target, sec)
			fmt.Printf("\n🔥 '/cluster/bench' に %q を送信しています...\n", strings.TrimSpace(cmd))
			runWrite(c, "cluster/bench", cmd)

			for elapsed := 1; elapsed <= sec+1; elapsed++ {
				time.Sleep(1 * time.Second)
				fmt.Printf("  ⏳ 計測中... (%d / %d 秒経過)\n", elapsed, sec)
			}
			time.Sleep(800 * time.Millisecond)
			fmt.Println("\n=== 計測完了後のベンチマーク比較表 (/cluster/bench) ===")
			runCat(c, "cluster/bench")
			_ = readLine("\n[Enter でメニューに戻る]")

		case "3":
			c := getClient()
			if c == nil {
				fmt.Println("⚠️ サーバーに未接続です。")
				continue
			}
			clients := collectOnlineClientNames(c)
			fmt.Println("\n--- [仮想ターミナル (ctl/exec) 対象ノード選択] ---")
			fmt.Println("  [1] server (/server/ctl/exec)")
			for idx, h := range clients {
				fmt.Printf("  [%d] %s (/client/%s/ctl/exec)\n", idx+2, h, h)
			}
			tSel := readLine("対象番号またはホスト名を入力 (省略時=1: server): ")
			execPath := "server/ctl/exec"
			if tSel == "" || tSel == "1" || strings.EqualFold(tSel, "server") {
				execPath = "server/ctl/exec"
			} else if n, err := strconv.Atoi(tSel); err == nil && n >= 2 && n-2 < len(clients) {
				execPath = fmt.Sprintf("client/%s/ctl/exec", clients[n-2])
			} else {
				execPath = fmt.Sprintf("client/%s/ctl/exec", strings.TrimPrefix(tSel, "client/"))
			}

			cmdStr := readLine(fmt.Sprintf("[%s] 実行するコマンドを入力 (例: whoami, ipconfig, uname -a): ", execPath))
			if cmdStr == "" {
				continue
			}
			runWrite(c, execPath, cmdStr+"\n")
			fmt.Println("⏳ コマンド実行結果を待機中...")
			time.Sleep(1500 * time.Millisecond)
			fmt.Println()
			runCat(c, execPath)
			_ = readLine("\n[Enter でメニューに戻る]")

		case "4":
			c := getClient()
			if c == nil {
				fmt.Println("⚠️ サーバーに未接続です。")
				continue
			}
			browseCUITree(c, readLine)

		case "5":
			c := getClient()
			fmt.Println("\n--- [1: システムイベントログ (/server/log/system)] ---")
			runCat(c, "server/log/system")
			fmt.Println("\n--- [2: ベンチマーク履歴 (/server/log/bench)] ---")
			runCat(c, "server/log/bench")
			_ = readLine("\n[Enter でメニューに戻る]")

		case "6":
			newAddr := readLine(fmt.Sprintf("新しいサーバーアドレスを入力 (現在: %s): ", cfg.Client.ServerAddr))
			if newAddr == "" {
				newAddr = cfg.Client.ServerAddr
			}
			fmt.Printf("🔌 %s へ接続試行中...\n", newAddr)
			newC, err := dial9PClient(newAddr, 3*time.Second)
			if err != nil {
				fmt.Printf("❌ 接続失敗: %v\n", err)
			} else {
				currClient = newC
				cfg.Client.ServerAddr = newAddr
				startClientTelemetryReporter(newC, newAddr)
				fmt.Printf("✓ %s に接続しました！\n", newAddr)
			}

		case "7":
			runInteractiveShell(getClient())

		default:
			fmt.Println("0〜7 の番号を選択してください。")
		}
	}
}

func browseCUITree(c *client.Client, readLine func(string) string) {
	currDir := ""
	for {
		entries, err := c.Readdir(currDir)
		if err != nil {
			fmt.Printf("エラー: /%s を読み取れません: %v\n", currDir, err)
			return
		}
		sort.Slice(entries, func(i, j int) bool {
			iDir := (entries[i].Mode & proto.DMDIR) != 0
			jDir := (entries[j].Mode & proto.DMDIR) != 0
			if iDir != jDir {
				return iDir
			}
			return entries[i].Name < entries[j].Name
		})

		fmt.Printf("\n=== 📂 仮想ディレクトリ: /%s ===\n", currDir)
		for idx, e := range entries {
			icon := "📄"
			if e.Mode&proto.DMDIR != 0 {
				icon = "📁"
			}
			fmt.Printf("  [%d] %s %s\n", idx+1, icon, e.Name)
		}
		if currDir != "" {
			fmt.Println("  [u] ⬆️ 親フォルダへ戻る")
		}
		fmt.Println("  [0] メインメニューへ戻る")

		sel := readLine("開く番号を入力: ")
		if sel == "0" || sel == "" {
			return
		}
		if strings.EqualFold(sel, "u") && currDir != "" {
			idx := strings.LastIndex(currDir, "/")
			if idx < 0 {
				currDir = ""
			} else {
				currDir = currDir[:idx]
			}
			continue
		}

		num, err := strconv.Atoi(sel)
		if err != nil || num < 1 || num > len(entries) {
			continue
		}
		chosen := entries[num-1]
		targetPath := chosen.Name
		if currDir != "" {
			targetPath = currDir + "/" + chosen.Name
		}

		if chosen.Mode&proto.DMDIR != 0 {
			currDir = targetPath
			continue
		}

		fmt.Printf("\n--- 📄 /%s の内容 ---\n", targetPath)
		runCat(c, targetPath)
		input := readLine("\nこのファイルへ書き込む場合は内容を入力 (Enterのみで戻る): ")
		if input != "" {
			runWrite(c, targetPath, input+"\n")
			time.Sleep(1000 * time.Millisecond)
			fmt.Printf("\n--- 📄 /%s (更新後) ---\n", targetPath)
			runCat(c, targetPath)
			_ = readLine("\n[Enter で一覧に戻る]")
		}
	}
}

func runInteractiveShell(c *client.Client) {
	fmt.Println("==================================================")
	fmt.Println("  9P Interactive Shell (Type 'exit' to return)    ")
	fmt.Println("  Commands: status, ls [path], cat <path>, write <path> <data>, help")
	fmt.Println("==================================================")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("9p> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 3)
		cmd := parts[0]

		switch cmd {
		case "exit", "quit":
			return
		case "help":
			runCat(c, "help")
		case "status":
			runCat(c, "cluster/status")
		case "ls":
			p := ""
			if len(parts) > 1 {
				p = parts[1]
			}
			runLs(c, p)
		case "cat":
			if len(parts) < 2 {
				fmt.Println("使用法: cat <path>")
				continue
			}
			runCat(c, parts[1])
		case "write", "echo":
			if len(parts) < 3 {
				fmt.Println("使用法: write <path> <data>")
				continue
			}
			runWrite(c, parts[1], parts[2]+"\n")
		default:
			fmt.Printf("未知のコマンド: %s (status, ls, cat, write, help, exit が使えます)\n", cmd)
		}
	}
}
