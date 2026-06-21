package main

import (
	"context"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	// 注意這裡的路徑要對應你的 go.mod
)

// App struct
type App struct {
	ctx    context.Context
	client *Client // 把我們的 VPN Client 放進來
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		client: NewClient(), // 初始化 Client
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// 在這裡監聽核心發來的訊號，並轉發給前端 JS
	go func() {
		for peers := range a.client.PeersChan {
			// 當收到 Peer 更新時，發送事件給前端，事件名稱叫 "peer-update"
			runtime.EventsEmit(a.ctx, "peer-update", peers)
			// We can't easily log to UI from here without a method, but the client log handles it.
			// Let's rely on client log for now.
		}
	}()

	go func() {
		for status := range a.client.StatusChan {
			runtime.EventsEmit(a.ctx, "status-update", status)
		}
	}()
}

// --- 以下是給前端 JS 呼叫的函式 ---

// Connect 讓前端呼叫連線
func (a *App) Connect(addr, user, pass string) string {
	err := a.client.ConnectAndLogin(addr, user, pass)
	if err != nil {
		return "Error: " + err.Error()
	}

	// 連線成功後，啟動 VPN 迴圈
	go func() {
		if err := a.client.StartVPN(); err != nil {
			a.client.log("[ERROR] StartVPN failed: %v", err)
		}
	}()

	return "success"
}

// GetMyIP 讓前端取得目前 IP
func (a *App) GetMyIP() string {
	return a.client.VirtualIP
}

// JoinGroup 讓前端加入群組
func (a *App) JoinGroup(name, pass string) string {
	err := a.client.JoinGroup(name, pass)
	if err != nil {
		return err.Error()
	}
	return "success"
}

func (a *App) CreateGroup(name, pass string) string {
	err := a.client.CreateGroup(name, pass)
	if err != nil {
		return err.Error() // 例如: "group already exists"
	}
	return "success"
}

func (a *App) LeaveGroup(groupName string) string {
	err := a.client.LeaveGroup(groupName)
	if err != nil {
		return err.Error()
	}
	return "success"
}
