package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"math"
	"math/bits"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/knusbaum/go9p/client"
	"github.com/knusbaum/go9p/proto"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

var (
	telemetryMu       sync.Mutex
	telemetryStop     chan struct{}
	activeTelConn     *client.Client
	activeTelHost     string
	autoReconnectHook func(c *client.Client, serverAddr string)

	clientBenchMu      sync.Mutex
	clientBenchStatus  = "idle"
	clientBenchResult  = ""
	clientBenchRunning = false

	clientExecMu       sync.Mutex
	clientExecStatus   = "idle"
	clientExecCmd      = ""
	clientExecExitCode = 0
	clientExecOutput   = ""

	lastProcessedCmdSeq uint64
)

func dial9PClient(addr string, timeout time.Duration) (*client.Client, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	c, err := client.NewClient(conn, "user", "")
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func getLocalNormalizedHostname() string {
	rawHost, _ := os.Hostname()
	host := strings.ToLower(strings.TrimSpace(rawHost))
	if host == "" {
		host = "client-pc"
	}
	return host
}

func getActiveTelemetryClient() *client.Client {
	telemetryMu.Lock()
	defer telemetryMu.Unlock()
	return activeTelConn
}

func setAutoReconnectHook(fn func(c *client.Client, serverAddr string)) {
	telemetryMu.Lock()
	autoReconnectHook = fn
	telemetryMu.Unlock()
}

// isDangerousClientCommand はクライアント側でもシステム保護のため危険コマンドを二重ブロックします。
func isDangerousClientCommand(cmd string) (bool, string) {
	lower := strings.ToLower(strings.TrimSpace(cmd))
	if strings.Contains(lower, "taskkill") || strings.Contains(lower, "stop-process") || strings.Contains(lower, "tskill") || strings.Contains(lower, "killall") || strings.Contains(lower, "pkill") {
		criticalProcs := []string{"explorer", "svchost", "csrss", "wininit", "winlogon", "lsass", "dwm", "smss", "systemd", "init", "p9_server", "9p_client"}
		for _, proc := range criticalProcs {
			if strings.Contains(lower, proc) {
				return true, fmt.Sprintf("重要プロセス (%s) の停止は禁止されています", proc)
			}
		}
	}
	forbidden := []string{
		"shutdown", "restart-computer", "stop-computer", "logoff", "reboot", "poweroff", "init 0", "init 6",
		"format ", "diskpart", "bcdedit", "rd /s", "rmdir /s", "del /f /s", "rm -rf", "mkfs",
		"start explorer", ":(){", "fork",
	}
	for _, f := range forbidden {
		if strings.Contains(lower, f) {
			return true, fmt.Sprintf("危険なコマンドパターン (%s) はブロックされました", strings.TrimSpace(f))
		}
	}
	return false, ""
}

func startClientTelemetryReporter(c *client.Client, serverAddr string) {
	telemetryMu.Lock()
	if telemetryStop != nil {
		close(telemetryStop)
	}
	stopCh := make(chan struct{})
	telemetryStop = stopCh
	activeTelConn = c
	telemetryMu.Unlock()

	go func() {
		host := getLocalNormalizedHostname()
		telemetryMu.Lock()
		activeTelHost = host
		currClient := activeTelConn
		telemetryMu.Unlock()

		if currClient != nil {
			_ = sendTelemetryReport(currClient, host)
		}

		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				telemetryMu.Lock()
				currClient = activeTelConn
				hook := autoReconnectHook
				telemetryMu.Unlock()

				if currClient == nil {
					newC, err := dial9PClient(serverAddr, 2*time.Second)
					if err == nil {
						log.Printf("[AUTO-RECONNECT] サーバー (%s) に自動接続しました\n", serverAddr)
						telemetryMu.Lock()
						activeTelConn = newC
						currClient = newC
						telemetryMu.Unlock()

						globalMountManager.UpdateClient(newC)
						if hook != nil {
							hook(newC, serverAddr)
						}
					} else {
						continue
					}
				}

				if err := sendTelemetryReport(currClient, host); err != nil {
					log.Printf("[WARN] 心拍通信切断を検知 (%v)。自動再接続へ移行します...\n", err)
					telemetryMu.Lock()
					activeTelConn = nil
					telemetryMu.Unlock()
				}
			}
		}
	}()
}

func computeClientCompositeWorkload(iterations int) uint64 {
	const bufferSize = 4096
	var buffer [bufferSize]uint64
	s0 := uint64(0x123456789abcdef0)
	s1 := uint64(0x0fedcba987654321)
	s2 := uint64(0x5555555555555555)
	s3 := uint64(0xaaaaaaaaaaaaaaaa)
	f0 := 1.0001
	f1 := 0.9999

	outerLoops := iterations / bufferSize
	if outerLoops < 1 {
		outerLoops = 1
	}

	for loopIdx := 0; loopIdx < outerLoops; loopIdx++ {
		offset := (loopIdx * 17) & (bufferSize - 1)
		for i := 0; i < bufferSize; i++ {
			s0 ^= s0 << 23
			s1 ^= s1 >> 17
			s2 = s2*6364136223846793005 + 1
			s3 = bits.RotateLeft64(s3, 19) ^ s0
			mixed := (s0 + s1) ^ (s2 + s3)
			idx := (i + offset) & (bufferSize - 1)
			buffer[idx] += mixed
			f0 = f0*1.0000001 + 0.0000002
			f1 = f1*0.9999999 + 0.0000001
		}
	}
	return buffer[0] ^ buffer[bufferSize-1] ^ uint64(f0) ^ uint64(f1)
}

func runClientBenchmark(sec int) {
	clientBenchMu.Lock()
	if clientBenchRunning {
		clientBenchMu.Unlock()
		return
	}
	clientBenchRunning = true
	clientBenchStatus = "running"
	clientBenchMu.Unlock()

	if sec <= 0 {
		sec = 5
	}
	if sec > 60 {
		sec = 60
	}
	threads := runtime.NumCPU()
	if threads < 1 {
		threads = 1
	}

	log.Printf("[CLIENT BENCH] ベンチマーク開始: %d秒間 (%dスレッド)...", sec, threads)

	go func() {
		stop := make(chan struct{})
		time.AfterFunc(time.Duration(sec)*time.Second, func() {
			close(stop)
		})

		var totalOps uint64
		var wg sync.WaitGroup
		const chunkSize = 4096 * 8
		const opsPerIteration = 7

		start := time.Now()
		for t := 0; t < threads; t++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var localOps uint64
				for {
					select {
					case <-stop:
						atomic.AddUint64(&totalOps, localOps)
						return
					default:
						_ = computeClientCompositeWorkload(chunkSize)
						localOps += uint64(chunkSize * opsPerIteration)
					}
				}
			}()
		}
		wg.Wait()
		elapsed := time.Since(start).Seconds()
		if elapsed < 0.001 {
			elapsed = 0.001
		}

		opsPerSec := float64(totalOps) / elapsed
		mops := opsPerSec / 1_000_000.0
		scorePt := uint32(math.Round(mops))
		if scorePt < 10 {
			scorePt = 10
		}
		res := fmt.Sprintf("Score: %d pt | Cores: %d | Time: %.2fs | Rate: %.1f Mops/s", scorePt, threads, elapsed, mops)
		log.Printf("[CLIENT BENCH] 計測完了: %s", res)

		clientBenchMu.Lock()
		clientBenchRunning = false
		clientBenchStatus = "finished"
		clientBenchResult = res
		clientBenchMu.Unlock()

		telemetryMu.Lock()
		c := activeTelConn
		h := activeTelHost
		telemetryMu.Unlock()
		if c != nil && h != "" {
			_ = sendTelemetryReport(c, h)
		}
	}()
}

func runClientExecCommand(cmdStr string) {
	if blocked, reason := isDangerousClientCommand(cmdStr); blocked {
		clientExecMu.Lock()
		clientExecStatus = "error"
		clientExecCmd = cmdStr
		clientExecExitCode = -1
		clientExecOutput = "[SECURITY BLOCK] " + reason
		clientExecMu.Unlock()
		return
	}

	clientExecMu.Lock()
	clientExecStatus = "running"
	clientExecCmd = cmdStr
	clientExecExitCode = 0
	clientExecOutput = "クライアント端末で実行中..."
	clientExecMu.Unlock()

	log.Printf("[CLIENT EXEC] コマンド実行開始: %q", cmdStr)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		cmd := buildClientShellCommand(ctx, cmdStr)
		rawOut, err := cmd.CombinedOutput()
		decoded := decodeClientConsoleOutput(rawOut)
		if len(decoded) > 32768 {
			decoded = decoded[:32768] + "\n... [Output truncated at 32KB]"
		}

		exitCode := 0
		status := "finished"
		if ctx.Err() == context.DeadlineExceeded {
			status = "error"
			exitCode = 124
			decoded += "\n[TIMEOUT] コマンドが15秒の制限時間を超過したため強制終了しました。"
		} else if err != nil {
			status = "error"
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = 1
			}
			if strings.TrimSpace(decoded) == "" {
				decoded = err.Error()
			}
		}

		clientExecMu.Lock()
		clientExecStatus = status
		clientExecCmd = cmdStr
		clientExecExitCode = exitCode
		clientExecOutput = decoded
		clientExecMu.Unlock()

		log.Printf("[CLIENT EXEC] 完了 (exit=%d, status=%s)", exitCode, status)

		telemetryMu.Lock()
		c := activeTelConn
		h := activeTelHost
		telemetryMu.Unlock()
		if c != nil && h != "" {
			_ = sendTelemetryReport(c, h)
		}
	}()
}

func pollClientCommands(c *client.Client, host string) error {
	f, err := c.Open("join", proto.Oread)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}

	lines := strings.Split(string(data), "\n")
	prefix := fmt.Sprintf("cmd_%s:", host)
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)

		// サーバーからの要求バージョンチェック
		if strings.HasPrefix(strings.ToLower(trimmed), "min_client_version:") {
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) == 2 {
				if reqVer, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64); err == nil {
					if reqVer > CurrentClientVersion {
						triggerClientAutoRestart(reqVer)
					}
				}
			}
			continue
		}

		if strings.HasPrefix(strings.ToLower(trimmed), prefix) {
			cmdPayload := strings.TrimSpace(trimmed[len(prefix):])
			parts := strings.Fields(cmdPayload)
			if len(parts) < 2 {
				continue
			}

			seq, err := strconv.ParseUint(parts[0], 10, 64)
			if err != nil || seq == 0 {
				continue
			}

			lastSeq := atomic.LoadUint64(&lastProcessedCmdSeq)
			if seq <= lastSeq {
				continue
			}
			atomic.StoreUint64(&lastProcessedCmdSeq, seq)

			cmdType := strings.ToLower(parts[1])
			log.Printf("[CLIENT] %s 宛てのリモート命令を受信 (seq=%d, type=%s)", host, seq, cmdType)

			switch cmdType {
			case "bench":
				sec := 5
				if len(parts) >= 3 {
					if s, err := strconv.Atoi(parts[2]); err == nil && s > 0 {
						sec = s
					}
				}
				runClientBenchmark(sec)

			case "exec":
				if len(parts) >= 3 {
					if decoded, err := base64.StdEncoding.DecodeString(parts[2]); err == nil {
						runClientExecCommand(string(decoded))
					}
				}

			case "restart":
				triggerClientAutoRestart(CurrentClientVersion)
			}
		}
	}
	return nil
}

func sendTelemetryReport(c *client.Client, host string) error {
	if c == nil {
		return fmt.Errorf("nil client")
	}

	if err := pollClientCommands(c, host); err != nil {
		return err
	}

	p, err := cpu.Percent(0, false)
	cpuVal := 0.0
	if err == nil && len(p) > 0 {
		cpuVal = p[0]
	}
	v, err2 := mem.VirtualMemory()
	var uMB, tMB uint64
	if err2 == nil {
		uMB = v.Used / 1024 / 1024
		tMB = v.Total / 1024 / 1024
	}

	clientBenchMu.Lock()
	bStatus := clientBenchStatus
	bResult := clientBenchResult
	clientBenchMu.Unlock()

	clientExecMu.Lock()
	eStatus := clientExecStatus
	eCmd := clientExecCmd
	eExit := clientExecExitCode
	eOut := clientExecOutput
	clientExecMu.Unlock()

	ackSeq := atomic.LoadUint64(&lastProcessedCmdSeq)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("hostname: %s\nos: %s (%s)\nversion: %d\ncpu: %.1f\nmem_used_mb: %d\nmem_total_mb: %d\n",
		host, runtime.GOOS, runtime.GOARCH, CurrentClientVersion, cpuVal, uMB, tMB))
	if ackSeq > 0 {
		sb.WriteString(fmt.Sprintf("cmd_ack: %d\n", ackSeq))
	}
	if bStatus != "" {
		sb.WriteString(fmt.Sprintf("bench_status: %s\n", bStatus))
	}
	if bResult != "" {
		sb.WriteString(fmt.Sprintf("bench_result: %s\n", bResult))
	}
	if eStatus != "" {
		sb.WriteString(fmt.Sprintf("exec_status: %s\n", eStatus))
		sb.WriteString(fmt.Sprintf("exec_exit: %d\n", eExit))
	}
	if eCmd != "" {
		sb.WriteString(fmt.Sprintf("exec_cmd_b64: %s\n", base64.StdEncoding.EncodeToString([]byte(eCmd))))
	}
	if eOut != "" {
		sb.WriteString(fmt.Sprintf("exec_out_b64: %s\n", base64.StdEncoding.EncodeToString([]byte(eOut))))
	}

	f, err := c.Open("join", proto.Owrite)
	if err != nil {
		return err
	}
	_, wErr := f.Write([]byte(sb.String()))
	_ = f.Close()
	return wErr
}
