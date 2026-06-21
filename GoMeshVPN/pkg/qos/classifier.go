package qos

import (
	"encoding/binary"
)

// Priority 定義封包優先級
type Priority int

const (
	PriorityLow    Priority = 1 // 大封包、批次資料
	PriorityMedium Priority = 2 // 一般資料
	PriorityHigh   Priority = 3 // 即時控制、小封包
)

// PacketClassifier 封包分類器（通用化設計）
type PacketClassifier struct {
	// 可擴展的配置參數
	SmallPacketThreshold  int // 小封包門檻（預設 200 bytes）
	MediumPacketThreshold int // 中型封包門檻（預設 800 bytes）
}

// NewPacketClassifier 建立預設分類器
func NewPacketClassifier() *PacketClassifier {
	return &PacketClassifier{
		SmallPacketThreshold:  200,
		MediumPacketThreshold: 800,
	}
}

// Classify 分類封包優先級（在加密前調用，處理明文 IP 封包）
func (pc *PacketClassifier) Classify(pkt []byte) Priority {
	// 基本檢查
	if len(pkt) < 20 {
		return PriorityMedium
	}

	// 檢查 IP 版本（只處理 IPv4）
	ipVersion := pkt[0] >> 4
	if ipVersion != 4 {
		return PriorityMedium
	}

	// 取得 IP 協議類型
	protocol := pkt[9]

	// 1. ICMP (ping, traceroute) - 網路診斷，高優先級
	if protocol == 1 {
		return PriorityHigh
	}

	// 2. UDP 協議處理
	if protocol == 17 {
		return pc.classifyUDP(pkt)
	}

	// 3. TCP 協議處理
	if protocol == 6 {
		if p := pc.classifyTCP(pkt); p != 0 {
			return p
		}
	}

	// 4. 其他協議按大小分類
	return pc.classifyBySize(pkt)
}

// classifyUDP 分類 UDP 封包
func (pc *PacketClassifier) classifyUDP(pkt []byte) Priority {
	if len(pkt) < 28 { // IP(20) + UDP(8)
		return PriorityMedium
	}

	// 檢查是否為 DNS 查詢（Port 53）
	srcPort := binary.BigEndian.Uint16(pkt[20:22])
	dstPort := binary.BigEndian.Uint16(pkt[22:24])

	if srcPort == 53 || dstPort == 53 {
		return PriorityHigh // DNS 查詢必須快速
	}

	// NTP (Port 123) - 時間同步
	if srcPort == 123 || dstPort == 123 {
		return PriorityHigh
	}

	// 其他 UDP 封包按大小分類
	return pc.classifyBySize(pkt)
}

// classifyTCP 分類 TCP 封包
func (pc *PacketClassifier) classifyTCP(pkt []byte) Priority {
	// 計算 IP header 長度
	ihl := int(pkt[0]&0x0F) * 4 // IP Header Length (單位: 4 bytes)

	if len(pkt) < ihl+14 { // 至少要有 TCP header 的前 14 bytes
		return PriorityMedium
	}

	// 取得 TCP Flags (offset 13 from TCP header start)
	tcpFlags := pkt[ihl+13]

	// 解析各個 flag
	flagFIN := (tcpFlags & 0x01) != 0 // 連線結束
	flagSYN := (tcpFlags & 0x02) != 0 // 連線建立
	flagRST := (tcpFlags & 0x04) != 0 // 連線重置
	flagPSH := (tcpFlags & 0x08) != 0 // 推送資料
	flagACK := (tcpFlags & 0x10) != 0 // 確認訊號

	// 1. 連線控制封包（SYN, FIN, RST）- 最高優先級
	if flagSYN || flagFIN || flagRST {
		return PriorityHigh
	}

	// 2. 純 ACK 封包（沒有資料載荷）- 高優先級
	// TCP header 最小 20 bytes，如果封包很小表示只是 ACK
	if flagACK && !flagPSH && len(pkt) < ihl+40 {
		return PriorityHigh
	}

	// 3. 帶 PSH flag 的小封包 - 通常是即時互動資料（如 SSH, Telnet）
	if flagPSH && len(pkt) < pc.SmallPacketThreshold+ihl {
		return PriorityHigh
	}

	// 4. 檢查常見的即時通訊協議 Port
	if len(pkt) >= ihl+4 {
		srcPort := binary.BigEndian.Uint16(pkt[ihl : ihl+2])
		dstPort := binary.BigEndian.Uint16(pkt[ihl+2 : ihl+4])

		// SSH (22), Telnet (23), HTTPS (443)
		if srcPort == 22 || dstPort == 22 ||
			srcPort == 23 || dstPort == 23 {
			return PriorityHigh
		}

		// HTTP/HTTPS 小封包（可能是 API 請求）
		if (srcPort == 80 || dstPort == 80 || srcPort == 443 || dstPort == 443) &&
			len(pkt) < pc.SmallPacketThreshold+ihl {
			return PriorityHigh
		}
	}

	// 5. 其他 TCP 封包按大小分類
	return pc.classifyBySize(pkt)
}

// classifyBySize 基於封包大小分類（通用規則）
func (pc *PacketClassifier) classifyBySize(pkt []byte) Priority {
	size := len(pkt)

	// 小封包 - 通常是控制訊息、即時互動
	if size < pc.SmallPacketThreshold {
		return PriorityHigh
	}

	// 中型封包 - 一般應用資料
	if size < pc.MediumPacketThreshold {
		return PriorityMedium
	}

	// 大封包 - 批次傳輸、檔案下載
	return PriorityLow
}

// String 輸出優先級名稱（用於日誌）
func (p Priority) String() string {
	switch p {
	case PriorityHigh:
		return "High"
	case PriorityMedium:
		return "Medium"
	case PriorityLow:
		return "Low"
	default:
		return "Unknown"
	}
}
