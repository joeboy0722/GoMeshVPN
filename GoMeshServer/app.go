package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"GoMeshServer/pkg/core"
	"GoMeshServer/pkg/database"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Custom Writer to stream logs to Wails
type LogWriter struct {
	ctx context.Context
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	if w.ctx != nil {
		runtime.EventsEmit(w.ctx, "server-log", string(p))
	}
	return len(p), nil
}

// App struct
type App struct {
	ctx    context.Context
	server *core.Server
}

// NewApp creates a new App application struct
func NewApp() *App {
	// Initialize Server Core (which inits DB)
	// DB is always open so we can manage users even if VPN server is stopped
	srv, err := core.NewServer("vpn_data.db")
	if err != nil {
		log.Fatalf("Failed to initialize server core: %v", err)
	}

	return &App{
		server: srv,
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	// Capture standard logs
	log.SetOutput(io.MultiWriter(os.Stdout, &LogWriter{ctx: ctx}))
	log.Println("Log Streaming Initialized")
}

// --- Server Control ---

func (a *App) StartServer(tcpPort, udpPort string, autoReg bool) string {
	config := core.ServerConfig{
		TcpPort:      tcpPort,
		UdpPort:      udpPort,
		AutoRegister: autoReg,
	}

	if err := a.server.Start(config); err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return "Success"
}

func (a *App) StopServer() string {
	a.server.Stop()
	return "Stopped"
}

func (a *App) IsServerRunning() bool {
	return a.server.Running
}

// --- User Management ---

func (a *App) GetAllUsers() []database.User {
	users, err := a.server.GetAllUsers() // Need to expose this in core/Server or access DB directly
	// server struct has db field but it is unexported 'db'.
	// I should add helper methods in Server or export DB?
	// Helper methods in Server is cleaner.
	if err != nil {
		log.Println("Error getting users:", err)
		return []database.User{}
	}
	return users
}

func (a *App) CreateUser(username, password string) string {
	// We need Helper in Server to access DB
	if err := a.server.CreateUser(username, password); err != nil {
		return err.Error()
	}
	return "Success"
}

func (a *App) DeleteUser(id int) string {
	if err := a.server.DeleteUser(id); err != nil {
		return err.Error()
	}
	return "Success"
}

// --- Group Management ---

func (a *App) GetAllGroups() []database.Group {
	groups, err := a.server.GetAllGroups()
	if err != nil {
		log.Println("Error getting groups:", err)
		return []database.Group{}
	}
	return groups
}

func (a *App) CreateGroup(name, password string) string {
	if err := a.server.CreateGroup(name, password); err != nil {
		return err.Error()
	}
	return "Success"
}

func (a *App) DeleteGroup(id int) string {
	if err := a.server.DeleteGroup(id); err != nil {
		return err.Error()
	}
	return "Success"
}
