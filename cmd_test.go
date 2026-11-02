package main

import (
	"testing"
)

func TestExtractClientUnifiedCommand(t *testing.T) {
	textWithQuickInputAndBottomInput := `=== 9P 遷移対応 Webブラウザ & 検索エンジン (net/web) ===
状態 (Status):     FINISHED (完了 ✓)
現在地 (Target):   https://kakaku.com/item/K0001630330/

--- [クイック入力 (Quick Input)] ---
↓ ここ（'> ' の右側）に 検索語 / リンク番号 / back / more / URL を入力して Ctrl+S (最下部でも入力可) ↓
> 

--- [本文 / 検索結果 (Content)] ---
[33] ショップに問い合わせる
[34] ビックカメラ.com

--- [操作コマンド一覧 (人間 & AI 共通)] ---
・Web検索する : キーワードを書き込む

--- [入力欄 (Input)] ---
↓ ここ ('> ' の右側) に 検索語 / リンク番号(1, 2, ...
>淡路島
`
	cmd, err := extractClientUnifiedCommand([]byte(textWithQuickInputAndBottomInput))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd != "淡路島" {
		t.Fatalf("expected '淡路島', got '%s'", cmd)
	}

	textWithTopInput := `=== 9P 遷移対応 Webブラウザ & 検索エンジン (net/web) ===
状態 (Status):     FINISHED (完了 ✓)
現在地 (Target):   https://kakaku.com/item/K0001630330/

--- [クイック入力 (Quick Input)] ---
↓ ここ（'> ' の右側）に 検索語 / リンク番号 / back / more / URL を入力して Ctrl+S (最下部でも入力可) ↓
> 淡路島

--- [本文 / 検索結果 (Content)] ---
[33] ショップに問い合わせる
[34] ビックカメラ.com

--- [操作コマンド一覧 (人間 & AI 共通)] ---
・Web検索する : キーワードを書き込む

--- [入力欄 (Input)] ---
↓ ここ ('> ' の右側) に 検索語 / リンク番号(1, 2, ...
> 
`
	cmdTop, err := extractClientUnifiedCommand([]byte(textWithTopInput))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmdTop != "淡路島" {
		t.Fatalf("expected '淡路島', got '%s'", cmdTop)
	}
}

func TestClientVersion(t *testing.T) {
	if CurrentClientVersion <= 0 {
		t.Fatalf("expected positive version, got %d", CurrentClientVersion)
	}
}
