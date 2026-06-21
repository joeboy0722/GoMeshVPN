package main

import (
	"GoMeshVPN/pkg/crypto"
	"GoMeshVPN/pkg/protocol"
	"GoMeshVPN/pkg/qos"
	"GoMeshVPN/pkg/tun"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// bufferPool 重用封包緩衝區，減少 GC 壓力
var bufferPool = sync.Pool{
	New: func() interface{} {
		// 預分配 2048 字節，足以容納大部分封包（MTU 1500 + 加密開銷 + VPN header）
		return make([]byte, 0, 2048)
	},
}

type Client struct {
	ServerAddr string
	Username   string
	Password   string

	Conn          net.Conn     // TCP
	UDPConn       *net.UDPConn // UDP
	SessionKey    []byte
	VirtualIP     string
	VirtualIPInt  uint32
	CurrentGroup  string
	ServerUDPPort string // Checkpoint for dynamic UDP port

	Tun tun.Device

	// Channels for UI updates
	LogChan    chan string
	StatusChan chan string
	PeersChan  chan protocol.StatusPayload // Changed to pass full payload (GroupName + Peers)

	// 重連機制
	Reconnecting    bool
	ShouldReconnect bool // 是否應該重連（用戶主動斷線時設為 false）

	// QoS 組件（流量控制與優先級調度）
	packetQueue *qos.PriorityQueue
	rateLimiter *qos.RateLimiter
	classifier  *qos.PacketClassifier

	// Goroutine 生命週期控制
	ctx        context.Context
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup // 等待所有 goroutines 結束

	// 配置參數
	RateLimitMbps     float64 // 流量限制（Mbps）
	KeepAliveInterval int     // KeepAlive 間隔（秒）

	// 統計資訊
	droppedPackets uint64 // 因佇列滿而丟棄的封包數（atomic）

	// [新增] 存活檢查相關
	lastReceivedActivity int64 // 最後一次收到伺服器封包的時間（UnixNano, atomic）
}

func NewClient() *Client {
	ctx, cancel := context.WithCancel(context.Background())

	return &Client{
		LogChan:         make(chan string, 100),
		StatusChan:      make(chan string, 10),
		PeersChan:       make(chan protocol.StatusPayload, 10), // Increase buffer for multiple groups
		ShouldReconnect: true,                                  // 預設開啟自動重連

		// 初始化 QoS 組件
		packetQueue: qos.NewPriorityQueue(5000),
		rateLimiter: qos.NewRateLimiter(50.0, 2.0), // 預設 50 Mbps, 2 秒突發
		classifier:  qos.NewPacketClassifier(),

		// Context 控制
		ctx:        ctx,
		cancelFunc: cancel,

		// 預設配置
		RateLimitMbps:     50.0, // 預設 50 Mbps
		KeepAliveInterval: 15,   // 預設 15 秒

		// 初始化活動時間
		lastReceivedActivity: time.Now().UnixNano(),
	}
}

func (c *Client) log(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	select {
	case c.LogChan <- msg:
	default: // Drop if full
	}
	log.Println(msg)
}

func (c *Client) ConnectAndLogin(addr, user, pass string) error {
	c.ServerAddr = addr
	c.Username = user
	c.Password = pass

	// 1. TCP Connect
	targetAddr := addr
	c.log("[DEBUG] Connecting to: '%s'", addr)

	// Check if port is already present
	host, port, err := net.SplitHostPort(addr)
	if err == nil && host != "" && port != "" {
		// Port exists, use as is
		c.log("[DEBUG] Detected Port: %s", port)
		targetAddr = addr
	} else {
		// Assume missing port, append default
		c.log("[DEBUG] SplitHostPort failed: %v. Appending default port.", err)
		targetAddr = addr + ":8889"
	}

	conn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		return err
	}

	// [新增] 開啟 TCP KeepAlive 讓作業系統層級能在 15 秒收不到 ACK 時發現連線半開 (Half-open)
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(15 * time.Second)
	}

	c.Conn = conn

	// 2. Handshake (Sync)
	if err := c.handshake(); err != nil {
		conn.Close()
		return err
	}
	c.log("Handshake successful.")

	// 3. Login (Sync - 這是唯一需要同步讀取的地方，因為還沒進 Loop)
	if err := c.login(); err != nil {
		conn.Close()
		return err
	}
	c.log("Login successful. Assigned IP: %s", c.VirtualIP)

	return nil
}

func (c *Client) handshake() error {
	priv, pub, err := crypto.GenerateKeyPair()
	if err != nil {
		return err
	}

	payload := protocol.HandshakePayload{PublicKey: pub.Bytes()}
	b, _ := json.Marshal(payload)
	pkt := protocol.ControlPacket{Type: protocol.TypeHandshake, Payload: b}

	enc := json.NewEncoder(c.Conn)
	if err := enc.Encode(pkt); err != nil {
		return err
	}

	dec := json.NewDecoder(c.Conn)
	var respPkt protocol.ControlPacket
	if err := dec.Decode(&respPkt); err != nil {
		return err
	}
	if respPkt.Type != protocol.TypeHandshake {
		return fmt.Errorf("expected handshake response")
	}

	var respPayload protocol.HandshakePayload
	if err := json.Unmarshal(respPkt.Payload, &respPayload); err != nil {
		return err
	}

	shared, err := crypto.ComputeSharedSecret(priv, respPayload.PublicKey)
	if err != nil {
		return err
	}
	c.SessionKey = crypto.DeriveSessionKey(shared)
	return nil
}

func (c *Client) login() error {
	p := protocol.LoginPayload{Username: c.Username, Password: c.Password}
	b, _ := json.Marshal(p)
	pkt := &protocol.ControlPacket{Type: protocol.TypeLogin, Payload: b}

	if err := c.sendTCPEncrypted(pkt); err != nil {
		return err
	}

	resp, err := c.readTCPEncrypted()
	if err != nil {
		return err
	}

	// [更新活動時間] 收到 TCP 控制封包也算活動
	atomic.StoreInt64(&c.lastReceivedActivity, time.Now().UnixNano())

	if resp.Type == protocol.TypeError {
		var sp protocol.StatusPayload
		json.Unmarshal(resp.Payload, &sp)
		return fmt.Errorf("login failed: %s", sp.Message)
	}

	if resp.Type == protocol.TypeStatus {
		var sp protocol.StatusPayload
		json.Unmarshal(resp.Payload, &sp)
		c.VirtualIP = sp.VirtualIP
		c.ServerUDPPort = sp.UdpPort // Save the negotiated UDP port

		ip := net.ParseIP(sp.VirtualIP).To4()
		c.VirtualIPInt = binary.BigEndian.Uint32(ip)

		// Login response might not have peers immediately, or might have them via Broadcast
		// If it has peers, send them
		if len(sp.Peers) > 0 {
			select {
			case c.PeersChan <- sp:
			default:
			}
		}
		return nil
	}
	return fmt.Errorf("unexpected login response")
}

// --- 以下三個函式修改為非阻塞 (Async) ---

func (c *Client) CreateGroup(name, password string) error {
	p := protocol.GroupPayload{GroupName: name, Password: password}
	b, _ := json.Marshal(p)
	pkt := &protocol.ControlPacket{Type: protocol.TypeCreateGroup, Payload: b}

	// 只發送，不讀取！
	if err := c.sendTCPEncrypted(pkt); err != nil {
		return err
	}

	// 樂觀更新狀態 (Front-end will separate lists)
	return nil
}

func (c *Client) JoinGroup(name, password string) error {
	p := protocol.GroupPayload{GroupName: name, Password: password}
	b, _ := json.Marshal(p)
	pkt := &protocol.ControlPacket{Type: protocol.TypeJoinGroup, Payload: b}

	// 只發送，不讀取！
	if err := c.sendTCPEncrypted(pkt); err != nil {
		return err
	}

	return nil
}

func (c *Client) LeaveGroup(groupName string) error {
	p := protocol.GroupPayload{GroupName: groupName}
	b, _ := json.Marshal(p)
	pkt := &protocol.ControlPacket{Type: protocol.TypeLeaveGroup, Payload: b}

	// 只發送，不讀取！
	if err := c.sendTCPEncrypted(pkt); err != nil {
		return err
	}

	return nil
}

// ----------------------------------------

func (c *Client) StartVPN() error {
	// 讓前端知道正在初始化介面卡
	c.StatusChan <- "Initializing"

	c.log("[INFO] Cleaning up previous session resources...")
	// 0. 先發送 Context 取消訊號，通知所有舊協程即將關閉
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	// 1. 隨即關閉舊的 TUN 介面，打破 ReadPacket 的底層 blocking，讓讀取協程安全退出
	if c.Tun != nil {
		c.log("[INFO] Closing existing TUN interface...")
		c.Tun.Close()
		c.Tun = nil
	}

	// 1.5. 同樣關閉舊的 UDP 連線，使 udpReadLoop 能立刻退出（否則要等最多 1 秒超時）
	if c.UDPConn != nil {
		c.UDPConn.Close()
		c.UDPConn = nil
	}

	// 2. 等待所有舊協程安全退出 (此時會瞬間完成)
	c.wg.Wait()
	c.ctx, c.cancelFunc = context.WithCancel(context.Background())

	// 3. 提前重置時間戳並發送心跳續命
	atomic.StoreInt64(&c.lastReceivedActivity, time.Now().UnixNano())
	c.log("[INFO] Sending pre-initialization heartbeat to server...")
	c.sendTCPHeartbeat()

	// 2. 初始化 TUN 介面 (這部分可能很慢，約 10 秒)
	var dev tun.Device
	var err error
	maxAttempts := 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		c.log("[INFO] Starting TUN interface (Attempt %d/%d)...", attempt, maxAttempts)
		dev, err = tun.New("GoMeshVPN", c.VirtualIP, c.sendTCPHeartbeat)
		if err == nil {
			break
		}
		c.log("[ERROR] TUN Initialization failed: %v", err)
		// 初始化期間持續發心跳給伺服器，防止 TCP 斷線
		c.sendTCPHeartbeat()
		if attempt < maxAttempts {
			time.Sleep(2 * time.Second)
		}
	}

	if err != nil {
		c.log("[FATAL] All attempts to start TUN failed.")
		c.StatusChan <- "Initialization Failed"
		return err
	}
	c.Tun = dev
	c.log("Wintun Interface Started.")

	// Parse ServerAddr to get Host
	host, _, err := net.SplitHostPort(c.ServerAddr)
	if err != nil {
		host = c.ServerAddr
	}

	udpPort := "8888"
	if c.ServerUDPPort != "" {
		udpPort = c.ServerUDPPort
	}

	raddr, err := net.ResolveUDPAddr("udp", host+":"+udpPort)
	if err != nil {
		return err
	}

	udpConn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return err
	}

	// 設置 UDP 緩衝區並驗證
	requestedBufSize := 4 * 1024 * 1024 // 4 MB
	if err := udpConn.SetReadBuffer(requestedBufSize); err != nil {
		c.log("[WARN] Failed to set UDP read buffer: %v", err)
	}
	if err := udpConn.SetWriteBuffer(requestedBufSize); err != nil {
		c.log("[WARN] Failed to set UDP write buffer: %v", err)
	}

	// 嘗試讀取實際設置的緩衝區大小（Windows 可能限制）
	// 注意：Go 標準庫沒有直接的 GetReadBuffer API，這裡只能依賴系統行為
	c.log("[INFO] UDP buffer requested: %d bytes (actual size may be limited by system)", requestedBufSize)

	c.UDPConn = udpConn
	c.log("UDP Connected to Server.")

	// [重要] 在啟動迴圈前，最後一次重置活動時間，確保寬限期從現在開始
	atomic.StoreInt64(&c.lastReceivedActivity, time.Now().UnixNano())

	// 發送初始 KeepAlive
	c.sendKeepAlive()

	// 啟動所有 goroutines（受 context 控制）
	c.wg.Add(5)
	go c.tunReadLoop()
	go c.packetSenderLoop() // 新增：專門負責發送的 goroutine
	go c.udpReadLoop()
	go c.keepAliveLoop()
	go c.tcpReadLoop()

	c.StatusChan <- "Connected"
	return nil
}

// tunReadLoop 從 TUN 介面讀取封包並分類加入佇列
func (c *Client) tunReadLoop() {
	defer c.wg.Done()

	for {
		// 檢查 context 是否取消
		select {
		case <-c.ctx.Done():
			c.log("[INFO] tunReadLoop stopped by context")
			return
		default:
		}

		pkt, err := c.Tun.ReadPacket()
		if err != nil {
			// 檢查是否為 context 取消導致的錯誤
			select {
			case <-c.ctx.Done():
				return
			default:
				c.log("Tun Read Error: %v", err)
				continue
			}
		}

		// 基本檢查
		if len(pkt) < 20 {
			continue
		}

		// 檢查 IP 版本（只處理 IPv4）
		version := pkt[0] >> 4
		if version != 4 {
			continue
		}

		// 分類封包優先級（在加密前，處理明文 IP 封包）
		priority := c.classifier.Classify(pkt)

		// 加密封包
		encrypted, err := crypto.Encrypt(pkt, c.SessionKey)
		if err != nil {
			continue
		}

		// 取得目標 IP
		targetIPBytes := pkt[16:20]
		targetIP := binary.BigEndian.Uint32(targetIPBytes)

		// 從 pool 獲取緩衝區
		buf := bufferPool.Get().([]byte)
		requiredSize := 9 + len(encrypted)

		// 防禦性檢查：確保 buffer 容量足夠（防止 panic）
		if cap(buf) < requiredSize {
			// 容量不足（極少發生，可能是 MTU 過大或加密膨脹異常）
			c.log("[WARN] Buffer pool capacity exceeded, allocating new buffer: %d bytes", requiredSize)
			buf = make([]byte, requiredSize)
		} else {
			buf = buf[:requiredSize]
		}

		// 構建 VPN 封包（加上 Header）
		buf[0] = protocol.UDPTypeData
		binary.BigEndian.PutUint32(buf[1:5], c.VirtualIPInt)
		binary.BigEndian.PutUint32(buf[5:9], targetIP)
		copy(buf[9:], encrypted)

		// 將封包加入優先級佇列（不直接發送）
		if !c.packetQueue.Enqueue(qos.Packet{
			Data:     buf,
			Priority: priority,
		}) {
			// 佇列已滿，丟棄封包並記錄統計
			atomic.AddUint64(&c.droppedPackets, 1)
			// 重要：歸還 buffer 到 pool
			bufferPool.Put(buf[:0])
			continue
		}
	}
}

// packetSenderLoop 從優先級佇列取封包並受流量限制器控制發送
func (c *Client) packetSenderLoop() {
	defer c.wg.Done()

	// 移除 Ticker，改用 Signal 驅動，消除 1ms 延遲瓶頸
	for {
		select {
		case <-c.ctx.Done():
			c.log("[INFO] packetSenderLoop stopped by context")
			return

		case <-c.packetQueue.Signal():
			// 隊列有資料，盡可能處理直到空
			// 增加批次限制，避免長時間佔用 CPU
			batchCount := 0
			const maxBatchSize = 100 // 每次最多處理 100 個封包後讓出 CPU

			for {
				// 在緊湊循環中也要檢查 context
				select {
				case <-c.ctx.Done():
					return
				default:
				}

				// 從佇列取出封包
				pkt, ok := c.packetQueue.Dequeue()
				if !ok {
					// 佇列空，跳出內部循環，回去等待 Signal
					break
				}

				// 流量控制：等待直到允許發送
				// Wait 內部會 Sleep，提供足夠的讓出時間
				c.rateLimiter.Wait(len(pkt.Data))

				// 發送封包
				if c.UDPConn != nil {
					c.UDPConn.Write(pkt.Data)
				}

				// 重要：歸還 buffer 到 pool，防止記憶體洩漏
				bufferPool.Put(pkt.Data[:0])

				// 批次計數器檢查與 CPU 讓出
				batchCount++
				if batchCount >= maxBatchSize {
					// 達到批次上限，顯式讓出 CPU 給其他 Goroutine (如讀取、UI、GC)
					// 這樣可以保持高吞吐量，同時不霸佔單核 CPU。
					// 重要的是：不跳出循環，繼續處理剩下封包，避免信號遺失。
					runtime.Gosched()
					batchCount = 0
				}
			}
		}
	}
}

func (c *Client) udpReadLoop() {
	defer c.wg.Done()

	buf := make([]byte, 65535)
	for {
		// 檢查 context
		select {
		case <-c.ctx.Done():
			c.log("[INFO] udpReadLoop stopped by context")
			return
		default:
		}

		// 設置讀取超時以允許週期性檢查 context
		c.UDPConn.SetReadDeadline(time.Now().Add(1 * time.Second))

		n, _, err := c.UDPConn.ReadFromUDP(buf)
		if err != nil {
			// 檢查是否為超時（正常）
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue // 超時後重新檢查 context
			}

			// 其他錯誤或 connection closed
			select {
			case <-c.ctx.Done():
				return
			default:
				c.log("UDP Read Error: %v", err)
				return
			}
		}

		data := buf[:n]
		if len(data) < 9 {
			continue
		}

		// [更新活動時間] 只要收到伺服器的 UDP 封包，就代表伺服器還活著
		atomic.StoreInt64(&c.lastReceivedActivity, time.Now().UnixNano())

		pktType := data[0]
		if pktType == protocol.UDPTypeData || pktType == protocol.UDPTypeBroadcast {
			encrypted := data[9:]
			decrypted, err := crypto.Decrypt(encrypted, c.SessionKey)
			if err != nil {
				continue
			}
			c.Tun.WritePacket(decrypted)
		} else if pktType == protocol.UDPTypeKeepAlive {
			// 伺服器回傳的 KeepAlive 響應，我們已經更新了 activity，這裡不需要額外處理
		}
	}
}

func (c *Client) keepAliveLoop() {
	defer c.wg.Done()

	// 使用可配置的間隔（預設 15 秒）
	interval := time.Duration(c.KeepAliveInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop() // 確保 ticker 被停止

	// [重要] 啟動時立刻發送一次心跳，不要等 interval 結束
	c.sendKeepAlive()
	c.sendTCPHeartbeat()

	for {
		select {
		case <-c.ctx.Done():
			c.log("[INFO] keepAliveLoop stopped by context")
			return

		case <-ticker.C:
			// [存活檢查] 檢查最後接收時間
			last := atomic.LoadInt64(&c.lastReceivedActivity)
			timeout := time.Duration(c.KeepAliveInterval+10) * time.Second
			if time.Since(time.Unix(0, last)) > timeout {
				c.log("[WARN] Connection timeout (no activity for %v). Forcing reconnect...", time.Since(time.Unix(0, last)))
				c.StatusChan <- "Timeout: Reconnecting"

				// 這裡不能直接調用 Reconnect，因為 Reconnect 是阻塞的且會啟動新 goroutine
				// 我們透過關閉連線來讓其他 Loop 報錯並進入重連
				if c.Conn != nil {
					c.Conn.Close()
				}
				if c.UDPConn != nil {
					c.UDPConn.Close()
				}
				// 停止目前循環
				return
			}

			c.sendKeepAlive()
			c.sendTCPHeartbeat()
		}
	}
}

func (c *Client) sendKeepAlive() {
	dummy := []byte("ping")
	enc, _ := crypto.Encrypt(dummy, c.SessionKey)

	msg := make([]byte, 9+len(enc))
	msg[0] = protocol.UDPTypeKeepAlive
	binary.BigEndian.PutUint32(msg[1:5], c.VirtualIPInt)
	binary.BigEndian.PutUint32(msg[5:9], 0)
	copy(msg[9:], enc)

	c.UDPConn.Write(msg)
}

func (c *Client) sendTCPHeartbeat() {
	pkt := &protocol.ControlPacket{Type: protocol.TypeHeartbeat, Payload: []byte("{}")}
	c.sendTCPEncrypted(pkt)
}

func (c *Client) sendTCPEncrypted(pkt *protocol.ControlPacket) error {
	data, err := json.Marshal(pkt)
	if err != nil {
		return err
	}
	encrypted, err := crypto.Encrypt(data, c.SessionKey)
	if err != nil {
		return err
	}

	length := len(encrypted)
	header := []byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)}
	c.Conn.Write(header)
	c.Conn.Write(encrypted)
	return nil
}

func (c *Client) readTCPEncrypted() (*protocol.ControlPacket, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(c.Conn, header); err != nil {
		return nil, err
	}
	length := int(header[0])<<24 | int(header[1])<<16 | int(header[2])<<8 | int(header[3])

	ciphertext := make([]byte, length)
	if _, err := io.ReadFull(c.Conn, ciphertext); err != nil {
		return nil, err
	}

	plaintext, err := crypto.Decrypt(ciphertext, c.SessionKey)
	if err != nil {
		return nil, err
	}

	var pkt protocol.ControlPacket
	if err := json.Unmarshal(plaintext, &pkt); err != nil {
		return nil, err
	}
	return &pkt, nil
}

func (c *Client) tcpReadLoop() {
	defer c.wg.Done()
	defer c.Conn.Close()

	for {
		// 檢查 context
		select {
		case <-c.ctx.Done():
			c.log("[INFO] tcpReadLoop stopped by context")
			return
		default:
		}

		pkt, err := c.readTCPEncrypted()
		if err != nil {
			// 檢查是否為正常關閉
			select {
			case <-c.ctx.Done():
				return
			default:
				c.log("TCP Read Error: %v", err)
				c.StatusChan <- "Disconnected"

				// 觸發重連機制
				if c.ShouldReconnect && !c.Reconnecting {
					c.Reconnecting = true
					go c.Reconnect()
				}
				return
			}
		}

		// [更新活動時間] 收到 TCP 控制封包也算活動
		atomic.StoreInt64(&c.lastReceivedActivity, time.Now().UnixNano())

		switch pkt.Type {
		case protocol.TypeStatus:
			var sp protocol.StatusPayload
			if err := json.Unmarshal(pkt.Payload, &sp); err != nil {
				c.log("JSON Unmarshal Error in TypeStatus: %v", err)
				continue
			}

			// Debug: Log info
			c.log("[DEBUG] Packet TypeStatus. Msg: %s, Peers: %d", sp.Message, len(sp.Peers))

			// 這裡會接收到 Server 廣播的 Peer List
			// Fix: Even if peers is empty, if GroupName is present, we must notify UI to render the group header
			if len(sp.Peers) > 0 || sp.GroupName != "" {
				c.log("Received Peer Update: %d peers for group %s", len(sp.Peers), sp.GroupName)
				select {
				case c.PeersChan <- sp: // 傳送給 UI
					c.log("[DEBUG] Sent peers to UI channel")
				default:
					c.log("[ERROR] PeersChan full! Update dropped.")
				}
			} else if sp.Message != "" {
				c.log("Server Message: %s", sp.Message)
				// 如果是錯誤訊息，顯示在狀態欄
			}

		case protocol.TypeError:
			var sp protocol.StatusPayload
			json.Unmarshal(pkt.Payload, &sp)
			c.log("Error from Server: %s", sp.Message)
			// 可以考慮把 Error 也送到 StatusChan

		case protocol.TypeHeartbeat:
			// Ignore
		}
	}
}

func (c *Client) Disconnect() {
	c.ShouldReconnect = false // 用戶主動斷線，停止自動重連
	c.log("User initiated disconnect...")

	// 1. 取消 context，通知所有 goroutines 停止
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	// 2. 關閉連線（會觸發阻塞的讀取操作返回錯誤）
	if c.Conn != nil {
		c.Conn.Close()
	}
	if c.UDPConn != nil {
		c.UDPConn.Close()
	}

	// 3. 等待所有 goroutines 結束（最多 5 秒）
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.log("All goroutines stopped gracefully")
	case <-time.After(5 * time.Second):
		c.log("[WARN] Timeout waiting for goroutines to stop")
	}

	// 4. 徹底拔除並關閉 TUN 介面，確保使用者系統完全乾淨不殘留虛擬網卡
	if c.Tun != nil {
		c.log("[INFO] 正在銷毀並卸載虛擬網卡，以確保系統完全乾淨不殘留...")
		c.Tun.Destroy()
		c.Tun = nil
	}

	c.log("Disconnected by user.")
}

// GetQoSStats 獲取 QoS 統計資訊供 UI 使用
func (c *Client) GetQoSStats() (queueLen int, droppedPackets uint64, queueHigh int, queueMedium int, queueLow int) {
	queueLen = c.packetQueue.Len()
	droppedPackets = atomic.LoadUint64(&c.droppedPackets)
	queueHigh, queueMedium, queueLow = c.packetQueue.GetStats()
	return
}

// Reconnect 實作自動重連機制（指數退避）
func (c *Client) Reconnect() {
	c.log("Connection lost. Attempting to reconnect...")
	c.StatusChan <- "Reconnecting"

	retryDelay := 2 // 初始延遲 2 秒
	maxDelay := 60  // 最大延遲 60 秒
	attempt := 0

	for c.ShouldReconnect {
		attempt++
		c.log("Reconnect attempt %d, waiting %d seconds...", attempt, retryDelay)
		time.Sleep(time.Duration(retryDelay) * time.Second)

		// 嘗試重新連線
		err := c.ConnectAndLogin(c.ServerAddr, c.Username, c.Password)
		if err == nil {
			c.log("Reconnection successful!")
			c.Reconnecting = false

			// 重新啟動 VPN（不需要重新創建 TUN，只重啟 UDP 和 loops）
			if err := c.restartVPN(); err != nil {
				c.log("Failed to restart VPN: %v", err)
				continue
			}

			c.StatusChan <- "Connected"
			return
		}

		c.log("Reconnect failed: %v", err)

		// 指數退避（最大 60 秒）
		retryDelay *= 2
		if retryDelay > maxDelay {
			retryDelay = maxDelay
		}
	}

	c.log("Reconnect cancelled by user.")
	c.Reconnecting = false
}

// restartVPN 重新啟動 VPN 連線（用於重連場景）
func (c *Client) restartVPN() error {
	c.log("[INFO] Restarting VPN, cleaning up previous session resources...")

	// 1. 發送 Context 取消訊號，通知協程準備關閉
	if c.cancelFunc != nil {
		c.cancelFunc()
	}

	// 2. 關閉舊 TUN 介面以解除 tunReadLoop 讀取阻塞
	if c.Tun != nil {
		c.log("[INFO] Closing existing TUN interface for restart...")
		c.Tun.Close()
		c.Tun = nil
	}

	// 3. 關閉舊 UDP 連線，使舊 udpReadLoop 立即退出
	if c.UDPConn != nil {
		c.UDPConn.Close()
		c.UDPConn = nil
	}

	// 4. 等待所有舊協程安全退出 (消除併發競爭 Race Condition)
	c.wg.Wait()
	c.ctx, c.cancelFunc = context.WithCancel(context.Background())

	// 5. 重新創建並初始化 TUN 介面
	dev, err := tun.New("GoMeshVPN", c.VirtualIP, c.sendTCPHeartbeat)
	if err != nil {
		return err
	}
	c.Tun = dev

	// 6. 解析 Server 地址並建立全新的 UDP 連線
	host, _, err := net.SplitHostPort(c.ServerAddr)
	if err != nil {
		host = c.ServerAddr
	}

	udpPort := "8888"
	if c.ServerUDPPort != "" {
		udpPort = c.ServerUDPPort
	}

	raddr, err := net.ResolveUDPAddr("udp", host+":"+udpPort)
	if err != nil {
		return err
	}

	udpConn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return err
	}

	requestedBufSize := 4 * 1024 * 1024
	if err := udpConn.SetReadBuffer(requestedBufSize); err != nil {
		c.log("[WARN] Failed to set UDP read buffer: %v", err)
	}
	if err := udpConn.SetWriteBuffer(requestedBufSize); err != nil {
		c.log("[WARN] Failed to set UDP write buffer: %v", err)
	}

	c.UDPConn = udpConn

	// 7. 發送初始 KeepAlive
	c.sendKeepAlive()

	// 8. 重啟所有協程 (與 StartVPN 保持一致)
	c.wg.Add(5)
	go c.tunReadLoop()
	go c.packetSenderLoop()
	go c.udpReadLoop()
	go c.keepAliveLoop()
	go c.tcpReadLoop()

	c.log("[INFO] VPN restarted successfully")
	return nil
}
