package tun

import (
	"fmt"
	"os/exec"
	"sync"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
)

type WintunDevice struct {
	Adapter *wintun.Adapter
	Session wintun.Session
	Name    string
	IP      string
	mu      sync.Mutex
}

func NewWintunDevice(name string, ip string) (*WintunDevice, error) {
	// 1. Create Adapter
	// Prioritize "WireGuard" style GUID or random
	adapter, err := wintun.CreateAdapter(name, "GoMeshVPN", nil)
	if err != nil {
		return nil, fmt.Errorf("error creating adapter: %v", err)
	}

	dev := &WintunDevice{
		Adapter: adapter,
		Name:    name,
		IP:      ip,
	}

	// 2. Start Session
	session, err := adapter.StartSession(0x800000) // Ring capacity
	if err != nil {
		adapter.Close()
		return nil, fmt.Errorf("error starting session: %v", err)
	}
	dev.Session = session

	// 3. Set IP Address using netsh (simplest way)
	if err := dev.setIP(ip); err != nil {
		dev.Close()
		return nil, err
	}

	return dev, nil
}

func (d *WintunDevice) setIP(ip string) error {
	// netsh interface ip set address name="AdapterName" static IP 255.255.0.0
	mask := "255.255.0.0" // Fixed /16 mask as per plan
	cmd := exec.Command("netsh", "interface", "ip", "set", "address",
		fmt.Sprintf("name=%s", d.Name), "static", ip, mask)

	// Need to run as admin, assume app is run as admin
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netsh error: %v, output: %s", err, string(output))
	}
	return nil
}

func (d *WintunDevice) ReadPacket() ([]byte, error) {
	// Wintun ReadPacket returns a packet from the ring buffer.
	// We allocate a buffer and copy it because the packet buffer is reused/release by wintun?
	// Actually ReceivePacket returns a pointer to the packet in the ring.
	// We must ReleaseReceivePacket after processing.
	pkt, err := d.Session.ReceivePacket()
	switch err {
	case nil:
		// Copy packet ensures we own the data and can release the ring slot immediately
		buf := make([]byte, len(pkt))
		copy(buf, pkt)
		d.Session.ReleaseReceivePacket(pkt)
		return buf, nil
	case windows.ERROR_NO_MORE_ITEMS:
		// No packets, wait a bit to avoid busy loop?
		// Wintun uses events, but the go wrapper might be polling or blocking?
		// ReceivePacket is non-blocking if no items?
		// "ReceivePacket returns a packet... If no packet... returns ERROR_NO_MORE_ITEMS"
		// The recommended way is to wait for the ReadWaitEvent.
		windows.WaitForSingleObject(d.Session.ReadWaitEvent(), windows.INFINITE)
		return d.ReadPacket() // Retry
	default:
		return nil, err
	}
}

func (d *WintunDevice) WritePacket(data []byte) error {
	// Allocate space in ring
	pkt, err := d.Session.AllocateSendPacket(len(data))
	if err == nil {
		copy(pkt, data)
		d.Session.SendPacket(pkt)
		return nil
	}

	switch err {
	case windows.ERROR_HANDLE_EOF:
		return fmt.Errorf("session closed")
	case windows.ERROR_BUFFER_OVERFLOW:
		// Ring full, drop packet
		return nil
	default:
		return err
	}
}

func (d *WintunDevice) Close() {
	// Session is a struct, so we can't check nil. Assuming open if Close called.
	d.Session.End()

	if d.Adapter != nil {
		d.Adapter.Close()
	}
}
