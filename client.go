package main

import (
	"fmt"
	"io"
	"log"

	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/proto"
)

// ファイルを読み取って画面に出す
func readFile(client *client.Client, path string) {
	// 9Pサーバー上のファイルを開く (Oread を使用)
	file, err := client.Open(path, proto.Oread)
	if err != nil {
		log.Printf("[ERROR] %s を開けませんでした: %v\n", path, err)
		return
	}
	defer file.Close()

	// ファイルの中身を全部読み取る
	data, err := io.ReadAll(file)
	if err != nil {
		log.Printf("[ERROR] %s の読み取りに失敗: %v\n", path, err)
		return
	}
	// 取得した文字列を標準出力に表示
	fmt.Printf("[%s の内容]: %s\n", path, string(data))
}

func main() {
	// サーバーPCのIP指定
	serverAddr := "winserver:5640"
	// 9Pサーバーに接続 (client.DialNet を使用)	
	cliant, err := client.DialNet("tcp", serverAddr, "user",nil)
	if err != nil {
		log.Fatalf("[ERROR] サーバーへの接続失敗: %v", err)
	}
	defer cliant.Close()

	// /hello ファイルを読み取る
	readFile(cliant, "hello")

	// /sys/cpu ファイルを読み取る
	readFile(cliant, "sys/cpu")
}