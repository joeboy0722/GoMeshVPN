package core

import (
	"GoMeshServer/pkg/database"
	"GoMeshServer/pkg/qos"
	"encoding/binary"
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"
)

// Session
type Session struct {
	VirtualIP  uint32
	RealAddr   *net.UDPAddr // Protected by AddrMu
	AddrMu     sync.RWMutex // Protects RealAddr
	SessionKey []byte
	User       *database.User
	GroupIDs   []int    // Cache for fast permission check
	Conn       net.Conn // TCP Connection
	WriteMu    sync.Mutex

	LastActive time.Time    // Track last activity time
	ActiveMu   sync.RWMutex // Protects LastActive

	// QoS Components
	PacketQueue *qos.PriorityQueue
	RateLimiter *qos.RateLimiter
	StopChan    chan struct{} // To stop the sender loop
	wg          sync.WaitGroup
}

type SessionManager struct {
	sessions        map[uint32]*Session // VirtualIP -> Session
	mu              sync.RWMutex
	db              *database.Database
	lastAllocatedIP uint32 // Tracks the highest IP assigned to avoid scanning
	Classifier      *qos.PacketClassifier
}

func NewSessionManager(db *database.Database) *SessionManager {
	return &SessionManager{
		sessions:   make(map[uint32]*Session),
		db:         db,
		Classifier: qos.NewPacketClassifier(),
	}
}

func (sm *SessionManager) AddSession(ip uint32, sess *Session) {
	sm.mu.Lock()

	oldSess, exists := sm.sessions[ip]
	if exists {
		oldSess.Close()
	}

	// Initialize QoS for this session
	sess.PacketQueue = qos.NewPriorityQueue(5000)
	sess.RateLimiter = qos.NewRateLimiter(50.0, 2.0) // Default 50Mbps per user
	sess.StopChan = make(chan struct{})
	sess.LastActive = time.Now() // Initialize activity time

	sm.sessions[ip] = sess
	sm.mu.Unlock()

	if exists {
		oldSess.wg.Wait()
		fmt.Printf("[INFO] SessionManager Replacing existing session for IP=%s\n", IPToString(ip))
	} else {
		fmt.Printf("[DEBUG] SessionManager Added: IP=%d, HEX=%x\n", ip, ip)
	}
}

func (sm *SessionManager) RemoveSession(ip uint32) {
	sm.mu.Lock()
	sess, exists := sm.sessions[ip]
	if exists {
		delete(sm.sessions, ip)
	}
	sm.mu.Unlock()

	if exists {
		sess.Close()
		sess.wg.Wait() // Wait for sender loop to finish
	}
}

// RemoveExactSession securely removes the session only if the pointers match
func (sm *SessionManager) RemoveExactSession(ip uint32, targetSess *Session) {
	sm.mu.Lock()
	sess, exists := sm.sessions[ip]
	if exists && sess == targetSess {
		delete(sm.sessions, ip)
	} else {
		exists = false
	}
	sm.mu.Unlock()

	if exists {
		targetSess.Close()
		targetSess.wg.Wait()
	}
}

func (sm *SessionManager) GetSession(ip uint32) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[ip]
}

func (sm *SessionManager) UpdateRealAddr(ip uint32, addr *net.UDPAddr) {
	sess := sm.GetSession(ip)
	if sess != nil {
		sess.AddrMu.Lock()
		sess.RealAddr = addr
		sess.AddrMu.Unlock()
	}
}

// Close safely closes the session connection and stops the sender loop
func (s *Session) Close() {
	if s.Conn != nil {
		s.Conn.Close()
	}
	select {
	case <-s.StopChan:
	default:
		close(s.StopChan)
	}
}

// Thread-safe setter for LastActive
func (s *Session) UpdateActivity() {
	s.ActiveMu.Lock()
	s.LastActive = time.Now()
	s.ActiveMu.Unlock()
}

// Thread-safe getter for LastActive
func (s *Session) GetLastActive() time.Time {
	s.ActiveMu.RLock()
	defer s.ActiveMu.RUnlock()
	return s.LastActive
}

// Thread-safe getter for Session
func (s *Session) GetRealAddr() *net.UDPAddr {
	s.AddrMu.RLock()
	defer s.AddrMu.RUnlock()
	return s.RealAddr
}

func (sm *SessionManager) GetGroupPeers(ip uint32) []*Session {
	sess := sm.GetSession(ip)
	if sess == nil || !sess.User.GroupID.Valid {
		return nil
	}
	groupID := int(sess.User.GroupID.Int64)

	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var peers []*Session
	for _, s := range sm.sessions {
		if s.VirtualIP != ip && s.User.GroupID.Valid && int(s.User.GroupID.Int64) == groupID {
			peers = append(peers, s)
		}
	}
	return peers
}

// IPAM: Scalable Allocation for 100.64.0.0/10 (CGNAT Expanded)
func (sm *SessionManager) AssignIP() (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 100.64.0.0/10 Range:
	// Start: 100.64.0.1
	// End:   100.127.255.254
	startIP := StringToIP("100.64.0.1")
	endIP := StringToIP("100.127.255.254")

	// Initialize if first run
	if sm.lastAllocatedIP == 0 {
		maxIPStr, err := sm.db.GetMaxVirtualIP()
		if err != nil {
			return "", fmt.Errorf("failed to init IPAM: %v", err)
		}

		if maxIPStr == "" {
			// DB Empty. Return startIP
			sm.lastAllocatedIP = startIP
			return IPToString(startIP), nil
		}

		// DB has IPs.
		maxVal := StringToIP(maxIPStr)
		ipBytes := make([]byte, 4)
		binary.BigEndian.PutUint32(ipBytes, maxVal)

		// Validation: Check if existing IP is within 100.64.0.0/10
		// 100 = 0x64. Second byte must be betweeen 64 (0x40) and 127 (0x7F).
		if ipBytes[0] != 100 || ipBytes[1] < 64 || ipBytes[1] > 127 {
			fmt.Printf("[WARN] IP %s out of 100.64.0.0/10 range. Resetting allocator.\n", maxIPStr)
			sm.lastAllocatedIP = startIP
			return IPToString(startIP), nil
		}

		sm.lastAllocatedIP = maxVal
	}

	// Normal Allocation
	nextIP := sm.lastAllocatedIP + 1

	if nextIP > endIP {
		return "", fmt.Errorf("IP POOL EXHAUSTED: system full")
	}

	sm.lastAllocatedIP = nextIP
	return IPToString(nextIP), nil
}

func (sm *SessionManager) KickUser(userID int) {
	var sessionsToKick []*Session

	// 1. Collect and delete within lock
	sm.mu.Lock()
	for ip, sess := range sm.sessions {
		if sess.User.ID == userID {
			sessionsToKick = append(sessionsToKick, sess)
			delete(sm.sessions, ip)
		}
	}
	sm.mu.Unlock()

	// 2. Perform blocking cleanup outside lock
	for _, sess := range sessionsToKick {
		sess.Close()
		sess.wg.Wait()
		fmt.Printf("[INFO] Kicked User ID %d (IP: %s)\n", userID, IPToString(sess.VirtualIP))
	}
}

// StartCleanupLoop routinely checks and removes inactive sessions
func (sm *SessionManager) StartCleanupLoop(timeout time.Duration, stopChan <-chan struct{}, broadcastCb func(int, string)) {
	go func() {
		ticker := time.NewTicker(30 * time.Second) // Check every 30 seconds
		defer ticker.Stop()

		for {
			select {
			case <-stopChan:
				fmt.Println("[INFO] Session cleanup loop stopped.")
				return
			case <-ticker.C:
				now := time.Now()
				var deadSessions []*Session

				sm.mu.RLock()
				for _, sess := range sm.sessions {
					if now.Sub(sess.GetLastActive()) > timeout {
						deadSessions = append(deadSessions, sess)
					}
				}
				sm.mu.RUnlock()

				for _, sess := range deadSessions {
					fmt.Printf("[INFO] Session Timeout: Kicking User %s (IP: %s) after %v inactivity\n",
						sess.User.Username, IPToString(sess.VirtualIP), timeout)

					sm.RemoveExactSession(sess.VirtualIP, sess)

					// Broadcast disconnection to group members
					if broadcastCb != nil {
						groups, err := sm.db.GetUserGroups(sess.User.ID)
						if err == nil {
							for _, g := range groups {
								go broadcastCb(g.ID, g.Name)
							}
						}
					}
				}
			}
		}
	}()
}

// StartSender starts the QoS-aware sender loop for this session
func (s *Session) StartSender(conn *net.UDPConn) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		// Remove Ticker, use Signal driven loop
		batchCount := 0
		const maxBatchSize = 100

		for {
			select {
			case <-s.StopChan:
				return

			// Use Signal from PacketQueue
			case <-s.PacketQueue.Signal():

				// Drain queue
				for {
					// Check concurrency context
					select {
					case <-s.StopChan:
						return
					default:
					}

					pkt, ok := s.PacketQueue.Dequeue()
					if !ok {
						break
					}

					// Rate Limit
					s.RateLimiter.Wait(len(pkt.Data))

					// Send if RealAddr is known (thread-safe read)
					addr := s.GetRealAddr()
					if addr != nil {
						conn.WriteToUDP(pkt.Data, addr)
					}

					// Important: Return buffer to pool
					// Assuming the packet data was allocated from bufferPool
					// We need to access the bufferPool from vpn_core.go
					// Ideally, we should export bufferPool or move it to a shared pkg
					// For now, assume it's accessible via core package variable
					// bufferPool is in vpn_core.go (same package core)
					bufferPool.Put(pkt.Data[:0])

					// Batch Yield - 每處理 100 個封包讓出 CPU
					batchCount++
					if batchCount >= maxBatchSize {
						// 主動讓出 CPU 給其他 Goroutine，防止長時間霸佔
						runtime.Gosched()
						batchCount = 0
					}
				}
			}
		}
	}()
}

// Helper: Uint32IP to String
func IPToString(ip uint32) string {
	bytes := make([]byte, 4)
	binary.BigEndian.PutUint32(bytes, ip)
	return net.IP(bytes).String()
}

// Helper: String to Uint32IP
func StringToIP(ipStr string) uint32 {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return 0
	}
	return binary.BigEndian.Uint32(ip)
}
