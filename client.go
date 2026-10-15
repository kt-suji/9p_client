package main

import (
	"io"
	"log"
	"net"
	"time"

	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/proto"
)

func readFile(c *client.Client, path string) string {
	// proto.Oread を指定
	f, err := c.Open(path, proto.Oread)
	if err != nil {
		log.Fatalf("Open failed (%s): %v", path, err)
	}
	defer f.Close()

	buf, err := io.ReadAll(f)
	if err != nil {
		log.Fatalf("Read failed (%s): %v", path, err)
	}
	return string(buf)
}

func writeFile(c *client.Client, path string, data string) {
	// proto.Owrite を指定
	f, err := c.Open(path, proto.Owrite)
	if err != nil {
		log.Fatalf("Open failed (%s): %v", path, err)
	}
	defer f.Close()

	_, err = f.Write([]byte(data))
	if err != nil {
		log.Fatalf("Write failed (%s): %v", path, err)
	}
}

func main() {
	serverAddr := "winserver:5640"

	// 1. TCP接続を確立
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		log.Fatalf("TCP接続エラー: %v", err)
	}
	defer conn.Close()

	// 2. 9Pクライアントを初期化 (conn, user, aname)
	c, err := client.NewClient(conn, "user", "")
	if err != nil {
		log.Fatalf("9P初期化エラー: %v", err)
	}

	// 3. 状態確認（Read）
	log.Println("--- 1. 初期状態を確認 ---")
	log.Printf("ctl/bench の内容:\n%s", readFile(c, "ctl/bench"))

	// 4. ベンチマーク開始要求（Write）
	log.Println("--- 2. ベンチマークを開始 (Write: 'start') ---")
	writeFile(c, "ctl/bench", "start\n")

	// 5. 実行中のステータス確認
	time.Sleep(500 * time.Millisecond)
	log.Printf("実行中確認:\n%s", readFile(c, "ctl/bench"))

	// 6. 完了待ち（サーバー側は約5秒の計算）
	log.Println("--- 3. 処理完了を待機中... ---")
	time.Sleep(6 * time.Second)

	// 7. 実行結果を取得（Read）
	log.Println("--- 4. 実行結果を取得 ---")
	log.Printf("完了後ステータス:\n%s", readFile(c, "ctl/bench"))
}
