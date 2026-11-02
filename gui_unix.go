//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/proto"
)

type unixTreeEntry struct {
	Name     string          `json:"name"`
	Path     string          `json:"path"`
	IsDir    bool            `json:"isDir"`
	Children []unixTreeEntry `json:"children,omitempty"`
}

func buildUnixTree(c *client.Client, dirPath string) []unixTreeEntry {
	cleanPath := strings.TrimPrefix(dirPath, "/")
	entries, err := c.Readdir(cleanPath)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		iDir := (entries[i].Mode & proto.DMDIR) != 0
		jDir := (entries[j].Mode & proto.DMDIR) != 0
		if iDir != jDir {
			return iDir
		}
		return entries[i].Name < entries[j].Name
	})

	var out []unixTreeEntry
	for _, e := range entries {
		sub := e.Name
		if cleanPath != "" {
			sub = cleanPath + "/" + e.Name
		}
		isDir := (e.Mode & proto.DMDIR) != 0
		item := unixTreeEntry{
			Name:  e.Name,
			Path:  sub,
			IsDir: isDir,
		}
		if isDir {
			item.Children = buildUnixTree(c, sub)
		}
		out = append(out, item)
	}
	return out
}

func launchLinuxDesktopWindow(url string) bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	// スタンドアロンウィンドウ (--app=...) 対応ブラウザを優先検索
	appBrowsers := []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"}
	for _, bin := range appBrowsers {
		if path, err := exec.LookPath(bin); err == nil {
			cmd := exec.Command(path, "--app="+url, "--window-size=1020,680")
			if err := cmd.Start(); err == nil {
				return true
			}
		}
	}
	if path, err := exec.LookPath("xdg-open"); err == nil {
		cmd := exec.Command(path, url)
		if err := cmd.Start(); err == nil {
			return true
		}
	}
	return false
}

// RunNativeApplication は Linux デスクトップ環境向けに 9P Virtual Resource Explorer GUI を起動します。
// X11 / Wayland が無いヘッドレス環境の場合は自動的に CUI コントロールセンターへフォールバックします。
func RunNativeApplication(c *client.Client, cfg *ClientConfig) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		log.Println("[INFO] ディスプレイ環境 (X11/Wayland) が検出されないため、CUI コントロールセンターを起動します。")
		runCUIControlCenter(c, cfg)
		return
	}

	var connMu sync.Mutex
	currClient := c

	setAutoReconnectHook(func(newC *client.Client, serverAddr string) {
		connMu.Lock()
		currClient = newC
		connMu.Unlock()
	})

	startClientTelemetryReporter(c, cfg.Client.ServerAddr)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/tree", func(w http.ResponseWriter, r *http.Request) {
		connMu.Lock()
		cl := currClient
		addr := cfg.Client.ServerAddr
		connMu.Unlock()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if cl == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"connected": false,
				"addr":      addr,
				"tree":      []unixTreeEntry{},
			})
			return
		}
		tree := buildUnixTree(cl, "")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connected": true,
			"addr":      addr,
			"tree":      tree,
		})
	})

	mux.HandleFunc("/api/file", func(w http.ResponseWriter, r *http.Request) {
		connMu.Lock()
		cl := currClient
		connMu.Unlock()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if cl == nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "サーバー未接続です"})
			return
		}

		if r.Method == http.MethodGet {
			p := strings.TrimPrefix(r.URL.Query().Get("path"), "/")
			f, err := cl.Open(p, proto.Oread)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"path": p, "content": string(data)})
			return
		}

		if r.Method == http.MethodPost {
			var req struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			cleanPath := strings.TrimPrefix(req.Path, "/")
			f, err := cl.Open(cleanPath, proto.Owrite)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			_, wErr := f.Write([]byte(req.Content))
			_ = f.Close()
			if wErr != nil {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": wErr.Error()})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}
	})

	mux.HandleFunc("/api/reconnect", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Addr string `json:"addr"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		newAddr := strings.TrimSpace(req.Addr)
		if newAddr == "" {
			newAddr = cfg.Client.ServerAddr
		}
		newC, err := dial9PClient(newAddr, 3*time.Second)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		connMu.Lock()
		currClient = newC
		cfg.Client.ServerAddr = newAddr
		connMu.Unlock()
		startClientTelemetryReporter(newC, newAddr)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "addr": newAddr})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(linuxExplorerHTML))
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("[WARN] GUIポートバインド失敗 (%v)。CUIモードへ切り替えます。\n", err)
		runCUIControlCenter(c, cfg)
		return
	}
	url := fmt.Sprintf("http://%s", ln.Addr().String())
	log.Printf("[GUI] Linux 9P Virtual Resource Explorer 起動中: %s\n", url)
	if !launchLinuxDesktopWindow(url) {
		fmt.Printf("\n🌐 ブラウザで以下のURLを開くと GUI エクスプローラーが使えます: %s\n", url)
	}
	_ = http.Serve(ln, mux)
}

const linuxExplorerHTML = `<!DOCTYPE html>
<html lang="ja">
<head>
<meta charset="utf-8">
<title>9P Virtual Resource Explorer (Linux GUI)</title>
<style>
  * { box-sizing: border-box; }
  body { margin: 0; font-family: monospace, sans-serif; background: #1e1e2e; color: #cdd6f4; display: flex; flex-direction: column; height: 100vh; }
  .toolbar { display: flex; gap: 8px; align-items: center; padding: 10px 14px; background: #181825; border-bottom: 1px solid #313244; }
  .toolbar input { background: #313244; color: #cdd6f4; border: 1px solid #45475a; padding: 6px 10px; border-radius: 4px; width: 200px; font-family: monospace; }
  .toolbar button, .bottombar button { background: #45475a; color: #cdd6f4; border: none; padding: 6px 12px; border-radius: 4px; cursor: pointer; font-weight: bold; }
  .toolbar button:hover, .bottombar button:hover { background: #585b70; }
  .main { display: flex; flex: 1; overflow: hidden; }
  .tree { width: 280px; background: #181825; border-right: 1px solid #313244; overflow-y: auto; padding: 10px; user-select: none; }
  .tree-item { padding: 4px 8px; cursor: pointer; border-radius: 4px; white-space: nowrap; }
  .tree-item:hover { background: #313244; }
  .tree-item.active { background: #89b4fa; color: #11111b; font-weight: bold; }
  .tree-children { padding-left: 16px; }
  .editor-wrap { flex: 1; display: flex; flex-direction: column; }
  textarea { flex: 1; background: #1e1e2e; color: #a6e3a1; border: none; padding: 14px; font-family: monospace; font-size: 13px; line-height: 1.45; resize: none; outline: none; white-space: pre; overflow: auto; }
  .bottombar { display: flex; gap: 12px; align-items: center; padding: 8px 14px; background: #181825; border-top: 1px solid #313244; }
  .status { font-size: 12px; color: #a6adc8; }
</style>
</head>
<body>
  <div class="toolbar">
    <span>サーバー:</span>
    <input id="addr" value="" placeholder="winserver:5640">
    <button onclick="reconnect()">🔌 再接続</button>
    <button onclick="loadTree()">🔄 ツリー更新</button>
  </div>
  <div class="main">
    <div id="tree" class="tree"></div>
    <div class="editor-wrap">
      <textarea id="editor" spellcheck="false" placeholder="左のツリーから仮想ファイルを選択してください (cluster/status 等)"></textarea>
    </div>
  </div>
  <div class="bottombar">
    <button onclick="saveFile()">💾 保存・実行 (Ctrl+S)</button>
    <button onclick="reloadCurrent()">🔃 再読込 (F5)</button>
    <span id="status" class="status">準備完了</span>
  </div>
<script>
let currentPath = "";
function setStatus(msg) { document.getElementById("status").textContent = msg; }

function renderNodes(nodes, container) {
  nodes.forEach(n => {
    const div = document.createElement("div");
    div.className = "tree-item" + (n.path === currentPath ? " active" : "");
    div.textContent = (n.isDir ? "📁 " : "📄 ") + n.name;
    if (!n.isDir) {
      div.onclick = () => openFile(n.path);
    }
    container.appendChild(div);
    if (n.isDir && n.children && n.children.length > 0) {
      const sub = document.createElement("div");
      sub.className = "tree-children";
      renderNodes(n.children, sub);
      container.appendChild(sub);
    }
  });
}

async function loadTree() {
  const res = await fetch("/api/tree");
  const data = await res.json();
  if (data.addr && !document.getElementById("addr").value) {
    document.getElementById("addr").value = data.addr;
  }
  const treeEl = document.getElementById("tree");
  treeEl.innerHTML = "";
  if (!data.connected) {
    setStatus("未接続 (バックグラウンドで自動再接続試行中...)");
    return;
  }
  renderNodes(data.tree || [], treeEl);
  if (currentPath) {
    await openFile(currentPath, true);
  } else {
    openFile("cluster/status");
  }
}

async function openFile(path, silent) {
  currentPath = path;
  const res = await fetch("/api/file?path=" + encodeURIComponent(path));
  const data = await res.json();
  if (data.error) {
    setStatus("エラー: " + data.error);
    return;
  }
  document.getElementById("editor").value = data.content;
  if (!silent) setStatus("表示中: " + path + "  [Ctrl+S: 保存・実行 / F5: 最新再読込]");
  document.querySelectorAll(".tree-item").forEach(el => {
    el.classList.toggle("active", el.textContent === "📄 " + path.split("/").pop() && currentPath === path);
  });
}

async function reloadCurrent() {
  if (currentPath) await openFile(currentPath);
  else await loadTree();
}

async function saveFile() {
  if (!currentPath) return;
  const content = document.getElementById("editor").value;
  const res = await fetch("/api/file", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({path: currentPath, content})
  });
  const data = await res.json();
  if (data.error) {
    alert("書き込みエラー:\n" + data.error);
    return;
  }
  setStatus("✓ " + currentPath + " に送信しました！結果を自動更新中...");
  [250, 1200, 2800, 5500].forEach(ms => setTimeout(() => { if (currentPath) openFile(currentPath, true); }, ms));
}

async function reconnect() {
  const addr = document.getElementById("addr").value;
  setStatus("接続試行中: " + addr + " ...");
  const res = await fetch("/api/reconnect", {
    method: "POST",
    headers: {"Content-Type": "application/json"},
    body: JSON.stringify({addr})
  });
  const data = await res.json();
  if (data.error) {
    alert("接続失敗:\n" + data.error);
    return;
  }
  setStatus("✓ 接続成功: " + data.addr);
  loadTree();
}

window.addEventListener("keydown", e => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") {
    e.preventDefault();
    saveFile();
  } else if (e.key === "F5") {
    e.preventDefault();
    reloadCurrent();
  }
});

loadTree();
</script>
</body>
</html>
`
