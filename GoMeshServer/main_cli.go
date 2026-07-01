//go:build headless

package main

import (
	"fmt"
	"os"
)

// runGUI 啟動 Wails GUI 介面 (純命令行版本下，此函式僅輸出錯誤提示)
func runGUI() {
	fmt.Println("錯誤：此執行檔為純命令行 (Headless) 版本，不支援啟動 GUI 介面。")
	fmt.Println("請使用命令列參數啟動伺服器，例如：")
	fmt.Println("  GoMeshServer-cli.exe -tcp_port 8889 -udp_port 8888")
	fmt.Println()
	printUsage()
	os.Exit(1)
}
