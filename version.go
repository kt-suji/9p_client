package main

// CurrentClientVersion はクライアントの現在バージョンです。
// ビルド時に -ldflags "-X main.ClientVersionStr=..." で上書きも可能です。
var CurrentClientVersion int64 = 2026110201
