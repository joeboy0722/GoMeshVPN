package tun

// Device represents a network interface device (TUN/TAP)
type Device interface {
	ReadPacket() ([]byte, error)
	WritePacket([]byte) error
	Close()
	Destroy() // 徹底銷毀並從系統中拔除虛擬網卡設備
	SetIP(ip string) error
	InterfaceName() string
}
