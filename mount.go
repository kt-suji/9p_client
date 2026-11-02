//go:build windows

package main

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/proto"
	"github.com/winfsp/cgofuse/fuse"
)

type P9FS struct {
	fuse.FileSystemBase
	client       *client.Client
	totalBytes   uint64
	freeBytes    uint64
	mu           sync.Mutex
	nextFh       uint64
	openFiles    map[uint64]*client.File
	writeBuffers map[uint64][]byte
	openPaths    map[uint64]string
}

func NewP9FS(c *client.Client, totalBytes, freeBytes uint64) *P9FS {
	if totalBytes == 0 {
		totalBytes = uint64(256) * 1024 * 1024 * 1024 * 4096 // 1PB
	}
	if freeBytes == 0 {
		freeBytes = totalBytes
	}
	return &P9FS{
		client:       c,
		totalBytes:   totalBytes,
		freeBytes:    freeBytes,
		nextFh:       1,
		openFiles:    make(map[uint64]*client.File),
		writeBuffers: make(map[uint64][]byte),
		openPaths:    make(map[uint64]string),
	}
}

func (fs *P9FS) getClient() *client.Client {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return fs.client
}

func (fs *P9FS) setClient(c *client.Client) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	fs.client = c
}

func (fs *P9FS) toP9Path(path string) string {
	return strings.TrimPrefix(path, "/")
}

func (fs *P9FS) Statfs(path string, stat *fuse.Statfs_t) int {
	stat.Bsize = 4096
	stat.Frsize = 4096
	stat.Blocks = fs.totalBytes / 4096
	stat.Bfree = fs.freeBytes / 4096
	stat.Bavail = fs.freeBytes / 4096
	return 0
}

func (fs *P9FS) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	if path == "/" {
		stat.Mode = fuse.S_IFDIR | 0777
		return 0
	}

	c := fs.getClient()
	if c == nil {
		return -fuse.EIO
	}

	p9Path := fs.toP9Path(path)
	st, err := c.Stat(p9Path)
	if err != nil {
		return -fuse.ENOENT
	}

	if st.Mode&proto.DMDIR != 0 {
		stat.Mode = fuse.S_IFDIR | 0777
	} else {
		stat.Mode = fuse.S_IFREG | 0666
		if st.Length == 0 {
			stat.Size = 4096 // 0バイトだとWindowsが読み込みをスキップするため
		} else {
			stat.Size = int64(st.Length)
		}
	}
	return 0
}

func (fs *P9FS) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	fill(".", nil, 0)
	fill("..", nil, 0)

	c := fs.getClient()
	if c == nil {
		return -fuse.EIO
	}

	p9Path := fs.toP9Path(path)
	entries, err := c.Readdir(p9Path)
	if err != nil {
		return -fuse.ENOENT
	}

	for _, e := range entries {
		fill(e.Name, nil, 0)
	}
	return 0
}

func (fs *P9FS) Access(path string, mask uint32) int {
	// すべてのアクセス要求を許可（AIエージェントやスクリプトからのアクセス拒否を回避）
	return 0
}

func (fs *P9FS) Chmod(path string, mode uint32) int {
	return 0
}

func (fs *P9FS) Chown(path string, uid uint32, gid uint32) int {
	return 0
}

func (fs *P9FS) Utimens(path string, tmsp []fuse.Timespec) int {
	return 0
}

func (fs *P9FS) Create(path string, flags int, mode uint32) (int, uint64) {
	c := fs.getClient()
	if c == nil {
		return -fuse.EIO, ^uint64(0)
	}

	p9Path := fs.toP9Path(path)

	var p9Mode proto.Mode
	acc := flags & fuse.O_ACCMODE
	switch acc {
	case fuse.O_WRONLY:
		p9Mode = proto.Owrite
	case fuse.O_RDWR:
		p9Mode = proto.Ordwr
	default:
		p9Mode = proto.Oread
	}
	if (flags & fuse.O_TRUNC) != 0 {
		p9Mode |= proto.Otrunc
	}

	f, err := c.Open(p9Path, p9Mode)
	if err != nil && (p9Mode&proto.Otrunc) != 0 {
		f, err = c.Open(p9Path, p9Mode&^proto.Otrunc)
	}
	if err != nil {
		return -fuse.EACCES, ^uint64(0)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	fh := fs.nextFh
	fs.nextFh++
	fs.openFiles[fh] = f
	fs.openPaths[fh] = path
	delete(fs.writeBuffers, fh)

	return 0, fh
}

func (fs *P9FS) Open(path string, flags int) (int, uint64) {
	c := fs.getClient()
	if c == nil {
		return -fuse.EIO, ^uint64(0)
	}

	p9Path := fs.toP9Path(path)

	var mode proto.Mode
	acc := flags & fuse.O_ACCMODE
	switch acc {
	case fuse.O_WRONLY:
		mode = proto.Owrite
	case fuse.O_RDWR:
		mode = proto.Ordwr
	default:
		mode = proto.Oread
	}
	if (flags & fuse.O_TRUNC) != 0 {
		mode |= proto.Otrunc
	}

	f, err := c.Open(p9Path, mode)
	if err != nil && (mode&proto.Otrunc) != 0 {
		f, err = c.Open(p9Path, mode&^proto.Otrunc)
	}
	if err != nil {
		return -fuse.EACCES, ^uint64(0)
	}

	fs.mu.Lock()
	defer fs.mu.Unlock()
	fh := fs.nextFh
	fs.nextFh++
	fs.openFiles[fh] = f
	fs.openPaths[fh] = path
	delete(fs.writeBuffers, fh)

	return 0, fh
}

func (fs *P9FS) Read(path string, buff []byte, ofst int64, fh uint64) int {
	fs.mu.Lock()
	f := fs.openFiles[fh]
	fs.mu.Unlock()

	if f == nil {
		return -fuse.EBADF
	}

	n, err := f.ReadAt(buff, ofst)
	if err != nil && n == 0 {
		return 0
	}
	return n
}

func (fs *P9FS) Write(path string, buff []byte, ofst int64, fh uint64) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	f := fs.openFiles[fh]
	if f == nil {
		return -fuse.EBADF
	}

	// クライアント側で書き込みチャンクを蓄積（メモ帳などの4KB分割保存を安全に吸収）
	buf := fs.writeBuffers[fh]
	reqLen := int(ofst) + len(buff)
	if reqLen > len(buf) {
		newBuf := make([]byte, reqLen)
		copy(newBuf, buf)
		buf = newBuf
	}
	copy(buf[ofst:], buff)
	fs.writeBuffers[fh] = buf

	return len(buff)
}

func (fs *P9FS) Truncate(path string, size int64, fh uint64) int {
	return 0
}

func (fs *P9FS) Flush(path string, fh uint64) int {
	return 0
}

func (fs *P9FS) Release(path string, fh uint64) int {
	fs.mu.Lock()
	f, ok := fs.openFiles[fh]
	wbuf, hasWrite := fs.writeBuffers[fh]
	delete(fs.openFiles, fh)
	delete(fs.writeBuffers, fh)
	delete(fs.openPaths, fh)
	fs.mu.Unlock()

	if !ok || f == nil {
		return 0
	}
	defer f.Close()

	if hasWrite && len(wbuf) > 0 {
		dataToSend := wbuf
		// テンプレートヘッダ ("===") が含まれる場合、クライアント側でスマートにコマンドを抽出して送信
		// これにより、古いサーバー相手でも 4KB 分割送信で弾かれずに 1発のクリーンなコマンドとして通る！
		if cmd, err := extractClientUnifiedCommand(wbuf); err == nil && cmd != "" {
			dataToSend = []byte(cmd)
		}
		_, _ = f.WriteAt(dataToSend, 0)
	}

	return 0
}

// extractClientUnifiedCommand はGUIエディタ保存時にクライアント側でコマンドを抽出するスマートプロキシです。
func extractClientUnifiedCommand(data []byte) (string, error) {
	rawStr := strings.TrimPrefix(string(data), "\ufeff")
	raw := strings.ReplaceAll(rawStr, "\r\n", "\n")
	trimmedAll := strings.TrimSpace(raw)
	if trimmedAll == "" {
		return "", fmt.Errorf("empty")
	}

	lines := strings.Split(raw, "\n")
	firstLine := strings.TrimSpace(lines[0])

	if !strings.HasPrefix(firstLine, "===") {
		for i := len(lines) - 1; i >= 0; i-- {
			t := strings.TrimSpace(lines[i])
			if t != "" {
				if strings.HasPrefix(t, ">") {
					t = strings.TrimSpace(strings.TrimPrefix(t, ">"))
				}
				if t != "" && !strings.HasPrefix(t, "↓") && !strings.HasPrefix(t, "※") {
					return t, nil
				}
			}
		}
		return "", fmt.Errorf("empty")
	}

	inInputSection := false
	var promptCandidates []string
	var inputSectionCandidates []string

	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "--- [入力欄") || strings.HasPrefix(t, "--- [クイック入力") || strings.HasPrefix(t, "--- [操作・入力") {
			inInputSection = true
			continue
		}
		if strings.HasPrefix(t, "--- [") {
			inInputSection = false
			continue
		}
		if strings.HasPrefix(t, ">") {
			cmd := strings.TrimSpace(strings.TrimPrefix(t, ">"))
			if cmd != "" {
				promptCandidates = append(promptCandidates, cmd)
			}
			continue
		}
		if inInputSection {
			if t != "" && !strings.HasPrefix(t, "↓") && !strings.HasPrefix(t, "※") {
				inputSectionCandidates = append(inputSectionCandidates, t)
			}
		}
	}

	if len(promptCandidates) > 0 {
		return promptCandidates[len(promptCandidates)-1], nil
	}
	if len(inputSectionCandidates) > 0 {
		return inputSectionCandidates[len(inputSectionCandidates)-1], nil
	}

	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, ">") {
			cmd := strings.TrimSpace(strings.TrimPrefix(t, ">"))
			if cmd != "" {
				return cmd, nil
			}
			break
		}
		if !strings.HasPrefix(t, "↓") && !strings.HasPrefix(t, "※") && !strings.HasPrefix(t, "---") {
			return t, nil
		}
	}

	return "", fmt.Errorf("no command")
}

// MountManager はマウントの開始・停止・状態を管理する
type MountManager struct {
	mu         sync.Mutex
	host       *fuse.FileSystemHost
	mounted    bool
	mountPoint string
	fs         *P9FS
}

var globalMountManager = &MountManager{}

func (mm *MountManager) IsMounted() bool {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return mm.mounted
}

func (mm *MountManager) GetMountPoint() string {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	return mm.mountPoint
}

func (mm *MountManager) UpdateClient(c *client.Client) {
	mm.mu.Lock()
	fs := mm.fs
	mm.mu.Unlock()
	if fs != nil {
		fs.setClient(c)
	}
}

func (mm *MountManager) UpdateCapacity(totalBytes, freeBytes uint64) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	if mm.fs != nil {
		mm.fs.totalBytes = totalBytes
		mm.fs.freeBytes = freeBytes
		log.Printf("[MOUNT] マウント済みドライブの容量を動的更新しました: Total=%d bytes, Free=%d bytes\n", totalBytes, freeBytes)
	}
}

func (mm *MountManager) MountAsync(c *client.Client, mountPoint string, totalBytes, freeBytes uint64, volName string, asNetwork bool) error {
	mm.mu.Lock()
	if mm.mounted {
		mm.mu.Unlock()
		return fmt.Errorf("already mounted on %s", mm.mountPoint)
	}

	p9fs := NewP9FS(c, totalBytes, freeBytes)
	mm.fs = p9fs
	mm.host = fuse.NewFileSystemHost(p9fs)
	mm.mountPoint = mountPoint
	mm.mounted = true
	mm.mu.Unlock()

	var options []string
	if volName == "" {
		volName = "9P Virtual SSD"
	}
	options = append(options, "-o", "volname="+volName)
	options = append(options, "-o", "direct_io")
	options = append(options, "-o", "FileInfoTimeout=0")
	options = append(options, "-o", "FileSecurity=D:P(A;;FA;;;WD)")
	options = append(options, "-o", "uid=-1")
	options = append(options, "-o", "gid=-1")
	if asNetwork {
		options = append(options, "--VolumePrefix=\\9p\\drive")
	}

	go func() {
		log.Printf("[MOUNT] %s にマウントを開始します (ラベル: '%s', ネットワーク表示: %v)...\n", mountPoint, volName, asNetwork)
		if !mm.host.Mount(mountPoint, options) {
			log.Printf("[ERROR] %s へのマウントが終了または失敗しました\n", mountPoint)
		}
		mm.mu.Lock()
		mm.mounted = false
		mm.mu.Unlock()
	}()

	return nil
}

func (mm *MountManager) Unmount() error {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	if !mm.mounted || mm.host == nil {
		return fmt.Errorf("not mounted")
	}

	log.Printf("[MOUNT] %s のアンマウントを実行中...\n", mm.mountPoint)
	ok := mm.host.Unmount()
	if !ok {
		return fmt.Errorf("unmount failed")
	}
	mm.mounted = false
	return nil
}

func StartMountBlocking(c *client.Client, mountPoint string, totalBytes, freeBytes uint64, volName string, asNetwork bool) {
	p9fs := NewP9FS(c, totalBytes, freeBytes)
	host := fuse.NewFileSystemHost(p9fs)
	if volName == "" {
		volName = "9P Virtual SSD"
	}
	options := []string{
		"-o", "volname=" + volName,
		"-o", "direct_io",
		"-o", "FileInfoTimeout=0",
		"-o", "FileSecurity=D:P(A;;FA;;;WD)",
		"-o", "uid=-1",
		"-o", "gid=-1",
	}
	if asNetwork {
		options = append(options, "--VolumePrefix=\\9p\\drive")
	}
	log.Printf("[MOUNT] %s に 9P ファイルシステムをマウント中 (ラベル: '%s')... (終了は Ctrl+C)\n", mountPoint, volName)
	if !host.Mount(mountPoint, options) {
		log.Fatalf("[ERROR] %s へのマウントに失敗しました\n", mountPoint)
	}
}
