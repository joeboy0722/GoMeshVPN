package protocol

import (
	"encoding/json"
)

type PacketType string

const (
	TypeHandshake   PacketType = "HANDSHAKE"
	TypeLogin       PacketType = "LOGIN"
	TypeRegister    PacketType = "REGISTER"
	TypeCreateGroup PacketType = "CREATE_GROUP"
	TypeJoinGroup   PacketType = "JOIN_GROUP"
	TypeLeaveGroup  PacketType = "LEAVE_GROUP"
	TypeHeartbeat   PacketType = "HEARTBEAT"
	TypeStatus      PacketType = "STATUS" // Server ?�傳?�??
	TypeError       PacketType = "ERROR"
)

// ControlPacket ?�於 TCP ?�制信令
// ?�握?��??��?，整??JSON 字串將被?��??�輸
type ControlPacket struct {
	Type    PacketType      `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// HandshakePayload ?��??�段交�??�鑰 (?��??�輸)
type HandshakePayload struct {
	PublicKey []byte `json:"public_key"`
}

// LoginPayload ?�入請�?
type LoginPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// RegisterPayload 註�?請�?
type RegisterPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// GroupPayload 群�??��?
type GroupPayload struct {
	GroupName string `json:"group_name"`
	Password  string `json:"password"` // ?�於 Join/Create
}

// StatusPayload 伺�??��??��??�?�更??
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
	IsOnline  bool   `json:"is_online"` // 標�??�員?�否?��?
}

// DataPacket UDP ?��?封�?結�? (Logical 概念，實?�是 Binary Stream)
// ?��?: [Type(1 byte)] [TargetIP(4 bytes)] [Encrypted Payload]
type UDPHeader struct {
	PacketType byte   // 0x01: P2P Data, 0x02: Broadcast, 0x03: KeepAlive
	SourceIP   uint32 // 來�??�擬 IP (big endian), ?�於 Server ?�找 SessionKey �??
	TargetIP   uint32 // ?��??�擬 IP (big endian)
}

const (
	UDPTypeData      byte = 0x01
	UDPTypeBroadcast byte = 0x02
	UDPTypeKeepAlive byte = 0x03
)
