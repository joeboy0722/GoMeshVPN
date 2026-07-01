package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"GoMeshServer/pkg/core"
)

const pidFile = "server.pid"

func main() {
	// 如果有帶參數，則進入命令行模式
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		switch cmd {
		case "stop", "shutdown":
			handleStop()
		case "start":
			handleStart(os.Args[2:])
			return
		case "-h", "--help", "help":
			printUsage()
			return
		default:
			// 如果第一個參數是以 '-' 開頭，代表直接帶參數啟動
			if cmd[0] == '-' {
				handleStart(os.Args[1:])
				return
			}
			fmt.Printf("未知的命令或參數: %s\n", cmd)
			printUsage()
			os.Exit(1)
		}
	}

	// 沒有任何參數時，啟動 GUI 介面
	runGUI()
}


// handleStart 啟動伺服器核心，以命令列模式運行
func handleStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	tcpPort := fs.String("tcp_port", "8889", "TCP 監聽埠")
	udpPort := fs.String("udp_port", "8888", "UDP 監聽埠")
	autoReg := fs.Bool("auto_registration", false, "是否開啟自動註冊")

	err := fs.Parse(args)
	if err != nil {
		fmt.Printf("解析參數失敗: %v\n", err)
		os.Exit(1)
	}

	// 寫入 PID 檔案
	pid := os.Getpid()
	err = os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644)
	if err != nil {
		fmt.Printf("無法寫入 PID 檔案: %v\n", err)
		os.Exit(1)
	}
	// 確保程式結束時會清理 PID 檔案
	defer os.Remove(pidFile)

	// 初始化伺服器核心
	srv, err := core.NewServer("vpn_data.db")
	if err != nil {
		log.Fatalf("無法初始化伺服器核心: %v", err)
	}

	config := core.ServerConfig{
		TcpPort:      *tcpPort,
		UdpPort:      *udpPort,
		AutoRegister: *autoReg,
	}

	log.Printf("正在啟動 GoMeshServer 命令行模式...")
	if err := srv.Start(config); err != nil {
		log.Fatalf("啟動伺服器失敗: %v", err)
	}

	log.Printf("伺服器已成功啟動！PID: %d。監聽 TCP:%s, UDP:%s (自動註冊:%v)", pid, config.TcpPort, config.UdpPort, config.AutoRegister)
	log.Println("您可以在此 CMD 視窗中輸入以下指令來控制伺服器：")
	log.Println("  start           - 啟動 VPN 服務")
	log.Println("  stop            - 停止 VPN 服務")
	log.Println("  status          - 查看當前運行狀態")
	log.Println("  shutdown / exit - 關閉伺服器並結束程式")
	log.Println("  help            - 顯示此指令說明")

	// 監聽結束信號以進行優雅關閉 (如 Ctrl+C)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 用於控制終端機互動迴圈結束的通道
	exitChan := make(chan struct{})

	// 啟動互動式命令列協程
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for {
			fmt.Print("> ")
			if !scanner.Scan() {
				break // Stdin 關閉或讀取失敗
			}
			input := strings.TrimSpace(scanner.Text())
			if input == "" {
				continue
			}

			switch strings.ToLower(input) {
			case "start":
				if srv.Running {
					log.Println("[提示] 伺服器已經在運行中。")
				} else {
					log.Println("正在啟動伺服器...")
					if err := srv.Start(config); err != nil {
						log.Printf("啟動伺服器失敗: %v\n", err)
					} else {
						log.Printf("伺服器已成功啟動！監聽 TCP:%s, UDP:%s", config.TcpPort, config.UdpPort)
					}
				}
			case "stop":
				if !srv.Running {
					log.Println("[提示] 伺服器目前未運行。")
				} else {
					log.Println("正在停止伺服器...")
					srv.Stop()
					log.Println("伺服器已停止。")
				}
			case "status":
				if srv.Running {
					log.Printf("狀態: 運行中 (TCP:%s / UDP:%s)\n", config.TcpPort, config.UdpPort)
				} else {
					log.Println("狀態: 已停止")
				}
			case "shutdown", "exit":
				log.Println("正在關閉伺服器並退出程式...")
				close(exitChan)
				return
			case "help":
				fmt.Println("可用指令:")
				fmt.Println("  start           - 啟動 VPN 服務")
				fmt.Println("  stop            - 停止 VPN 服務")
				fmt.Println("  status          - 查看當前運行狀態")
				fmt.Println("  shutdown / exit - 關閉伺服器並結束程式")
				fmt.Println("  help            - 顯示此說明")
			default:
				fmt.Printf("未知指令: %s，輸入 'help' 查看可用指令。\n", input)
			}
		}
	}()

	// 等待退出信號或互動式命令列的退出請求
	select {
	case <-sigChan:
		log.Println("\n接收到系統中斷訊號，正在關閉伺服器...")
	case <-exitChan:
	}

	if srv.Running {
		srv.Stop()
	}
	log.Println("伺服器已安全關閉。")
}

// handleStop 讀取 PID 檔案並關閉運行中的伺服器進程
func handleStop() {
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("伺服器未運行 (找不到 server.pid)")
			os.Exit(0)
		}
		fmt.Printf("讀取 server.pid 失敗: %v\n", err)
		os.Exit(1)
	}

	pid, err := strconv.Atoi(string(pidData))
	if err != nil {
		fmt.Printf("無效的 PID 格式: %v\n", err)
		os.Exit(1)
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Printf("找不到 PID 為 %d 的進程: %v\n", pid, err)
		os.Exit(1)
	}

	// 結束進程
	err = killProcess(process)
	if err != nil {
		// 如果進程已經不存在，則直接清理 PID 檔案
		if err == os.ErrProcessDone || err.Error() == "os: process already finished" {
			_ = os.Remove(pidFile)
			fmt.Printf("伺服器進程 (PID: %d) 已經不存在，已清理無效的 PID 檔案。\n", pid)
			os.Exit(0)
		}
		fmt.Printf("停止伺服器進程失敗: %v\n", err)
		os.Exit(1)
	}

	// 刪除 PID 檔案
	_ = os.Remove(pidFile)
	fmt.Printf("成功停止 PID 為 %d 的伺服器進程。\n", pid)
	os.Exit(0)
}

// killProcess 跨平台結束進程的輔助函式
func killProcess(p *os.Process) error {
	if runtime.GOOS == "windows" {
		// Windows 不支援 SIGTERM/SIGINT 信號發送給其他進程，直接 Kill
		return p.Kill()
	}

	// Unix 系統發送 SIGTERM 進行優雅關閉
	if err := p.Signal(syscall.SIGTERM); err != nil {
		return p.Kill()
	}

	// 等待進程退出
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		// 在 Unix 上，向已退出的進程發送信號 0 會返回錯誤
		if err := p.Signal(syscall.Signal(0)); err != nil {
			return nil
		}
	}

	// 超時未退出，強制 Kill
	return p.Kill()
}

// printUsage 印出命令列使用說明
func printUsage() {
	fmt.Println("用法:")
	fmt.Println("  GoMeshServer.exe                      (啟動 GUI 介面)")
	fmt.Println("  GoMeshServer.exe start [flags]        (以命令列模式啟動伺服器)")
	fmt.Println("  GoMeshServer.exe stop                 (停止正在運行的命令列伺服器)")
	fmt.Println("  GoMeshServer.exe shutdown             (停止正在運行的命令列伺服器)")
	fmt.Println("\n啟動參數 (flags):")
	fmt.Println("  -tcp_port string")
	fmt.Println("    	TCP 監聽埠 (預設 \"8889\")")
	fmt.Println("  -udp_port string")
	fmt.Println("    	UDP 監聽埠 (預設 \"8888\")")
	fmt.Println("  -auto_registration bool")
	fmt.Println("    	是否開啟自動註冊 (預設 false)")
}
