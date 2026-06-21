package protocol

import (
	"encoding/json"
)

// PacketType 定義控制封包類型
type PacketType string

const (
	TypeHandshake   PacketType = "HANDSHAKE"
	TypeLogin       PacketType = "LOGIN"
	TypeRegister    PacketType = "REGISTER"
	TypeCreateGroup PacketType = "CREATE_GROUP"
	TypeJoinGroup   PacketType = "JOIN_GROUP"
	TypeLeaveGroup  PacketType = "LEAVE_GROUP"
	TypeHeartbeat   PacketType = "HEARTBEAT"
	TypeStatus      PacketType = "STATUS" // Server 回傳狀態
	TypeError       PacketType = "ERROR"
)

// ControlPacket 用於 TCP 控制信令
// 在握手完成後，整個 JSON 字串將被加密傳輸
type ControlPacket struct {
	Type    PacketType      `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// HandshakePayload 握手階段交換公鑰 (明文傳輸)
type HandshakePayload struct {
	PublicKey []byte `json:"public_key"`
}

// LoginPayload 登入請求
type LoginPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// RegisterPayload 註冊請求
type RegisterPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// GroupPayload 群組操作
type GroupPayload struct {
	GroupName string `json:"group_name"`
	Password  string `json:"password"` // 用於 Join/Create
}

// StatusPayload 伺服器回傳的狀態更新
type StatusPayload struct {
	Message   string `json:"message"`
	VirtualIP string `json:"virtual_ip"`
	UdpPort   string `json:"udp_port,omitempty"` // Server UDP Port for VPN Traffic
	GroupName string `json:"group_name,omitempty"`
	Peers     []Peer `json:"peers,omitempty"`
}

type Peer struct {
	Username  string `json:"username"`
	VirtualIP string `json:"virtual_ip"`
	IsOnline  bool   `json:"is_online"`
}

// DataPacket UDP 數據封包結構 (Logical 概念，實際是 Binary Stream)
// 格式: [Type(1 byte)] [TargetIP(4 bytes)] [Encrypted Payload]
type UDPHeader struct {
	PacketType byte   // 0x01: P2P Data, 0x02: Broadcast, 0x03: KeepAlive
	SourceIP   uint32 // 來源虛擬 IP (big endian), 用於 Server 查找 SessionKey 解密
	TargetIP   uint32 // 目標虛擬 IP (big endian)
}

const (
	UDPTypeData      byte = 0x01
	UDPTypeBroadcast byte = 0x02
	UDPTypeKeepAlive byte = 0x03
)
