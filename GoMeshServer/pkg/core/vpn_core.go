package core

import (
	"GoMeshServer/pkg/database"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// Global Buffer Pool to avoid allocation overhead
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 2048)
	},
}

type ServerConfig struct {
	TcpPort      string
	UdpPort      string
	AutoRegister bool
}

type Server struct {
	db          *database.Database
	sessions    *SessionManager
	tcpListener net.Listener
	udpConn     *net.UDPConn
	Config      ServerConfig
	Running     bool
	mu          sync.Mutex
	stopChan    chan struct{}
}

func NewServer(dbPath string) (*Server, error) {
	db, err := database.NewDatabase(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to init db: %v", err)
	}

	sm := NewSessionManager(db)

	return &Server{
		db:       db,
		sessions: sm,
		stopChan: make(chan struct{}),
	}, nil
}

func (s *Server) Start(config ServerConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Running {
		return fmt.Errorf("server already running")
	}

	s.Config = config
	s.Running = true
	s.stopChan = make(chan struct{})

	// Start UDP
	addr, err := net.ResolveUDPAddr("udp", ":"+config.UdpPort)
	if err != nil {
		return err
	}
	udpConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	s.udpConn = udpConn

	// 設置 UDP 緩衝區並驗證
	requestedBufSize := 4 * 1024 * 1024 // 4 MB
	if err := s.udpConn.SetReadBuffer(requestedBufSize); err != nil {
		log.Printf("[WARN] Failed to set UDP read buffer: %v", err)
	}
	if err := s.udpConn.SetWriteBuffer(requestedBufSize); err != nil {
		log.Printf("[WARN] Failed to set UDP write buffer: %v", err)
	}
	log.Printf("[INFO] UDP buffer requested: %d bytes (actual size may be limited by system)", requestedBufSize)

	go s.StartUDPLoop()

	// Start TCP
	ln, err := net.Listen("tcp", ":"+config.TcpPort)
	if err != nil {
		udpConn.Close()
		return err
	}
	s.tcpListener = ln
	go s.StartTCPLoop()

	// Start Session Cleanup Loop (Timeout: 60s)
	s.sessions.StartCleanupLoop(60*time.Second, s.stopChan, s.BroadcastPeers)

	log.Printf("Server started on TCP:%s / UDP:%s (AutoReg:%v)", config.TcpPort, config.UdpPort, config.AutoRegister)
	return nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.Running {
		return
	}

	s.Running = false
	close(s.stopChan)

	if s.tcpListener != nil {
		s.tcpListener.Close()
	}
	if s.udpConn != nil {
		s.udpConn.Close()
	}

	log.Println("Server stopped.")
}

func (s *Server) StartTCPLoop() {
	for {
		conn, err := s.tcpListener.Accept()
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
				log.Println("Accept error:", err)
				continue
			}
		}
		go s.HandleTCP(conn)
	}
}

// --- DB Proxy Methods ---

func (s *Server) GetAllUsers() ([]database.User, error) {
	return s.db.GetAllUsers()
}

func (s *Server) CreateUser(username, password string) error {
	newIP, err := s.sessions.AssignIP()
	if err != nil {
		return err
	}
	return s.db.CreateUser(username, password, newIP)
}

func (s *Server) DeleteUser(id int) error {
	s.sessions.KickUser(id)
	return s.db.DeleteUser(id)
}

func (s *Server) GetAllGroups() ([]database.Group, error) {
	return s.db.GetAllGroups()
}

func (s *Server) CreateGroup(name, password string) error {
	return s.db.CreateGroup(name, password)
}

func (s *Server) DeleteGroup(id int) error {
	return s.db.DeleteGroup(id)
}
