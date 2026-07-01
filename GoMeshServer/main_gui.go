//go:build !headless

package main

import (
	"embed"
	"runtime"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// hideConsole 隱藏 Windows 的控制台視窗，避免彈出黑色 CMD 視窗
func hideConsole() {
	if runtime.GOOS == "windows" {
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		freeConsole := kernel32.NewProc("FreeConsole")
		freeConsole.Call()
	}
}

// runGUI 啟動 Wails GUI 介面
func runGUI() {
	// 在 Windows 上隱藏控制台視窗，避免 GUI 模式下彈出黑色 CMD 視窗
	hideConsole()

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "GoMeshServer",
		Width:  1024,
		Height: 768,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
