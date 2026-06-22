//go:build linux

package tun

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

type LinuxDevice struct {
	File *os.File
	Name string
	IP   string
	mu   sync.Mutex
}

func NewLinuxDevice(name string, ip string) (*LinuxDevice, error) {
	// Open TUN device
	fd, err := unix.Open("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/net/tun error: %v", err)
	}

	// Create interface
	// struct ifreq {
	//    char name[16];
	//    short flags;
	//    ...
	// }
	var ifr struct {
		Name  [16]byte
		Flags uint16
		_     [22]byte
	}

	copy(ifr.Name[:], name)
	ifr.Flags = unix.IFF_TUN | unix.IFF_NO_PI

	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.TUNSETIFF),
		uintptr(unsafe.Pointer(&ifr)),
	)
	if errno != 0 {
		unix.Close(fd)
		return nil, fmt.Errorf("ioctl TUNSETIFF error: %v", errno)
	}

	actualName := string(ifr.Name[:])
	// Remove null termination
	for i, c := range actualName {
		if c == 0 {
			actualName = actualName[:i]
			break
		}
	}

	// Create os.File from fd
	file := os.NewFile(uintptr(fd), actualName)

	dev := &LinuxDevice{
		File: file,
		Name: actualName,
		IP:   ip,
	}

	if err := dev.SetIP(ip); err != nil {
		file.Close()
		return nil, err
	}

	return dev, nil
}

func (d *LinuxDevice) InterfaceName() string {
	return d.Name
}

func (d *LinuxDevice) ReadPacket() ([]byte, error) {
	buf := make([]byte, 2048) // MTU + overhead
	n, err := d.File.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func (d *LinuxDevice) WritePacket(data []byte) error {
	_, err := d.File.Write(data)
	return err
}

func (d *LinuxDevice) Close() {
	d.File.Close()
}

func (d *LinuxDevice) Destroy() {
	d.Close()
}

func (d *LinuxDevice) SetIP(ip string) error {
	// ip link set dev <name> up
	// We execute ip command because netlink is complex to implement from scratch
	cmdLink := exec.Command("ip", "link", "set", "dev", d.Name, "up")
	if out, err := cmdLink.CombinedOutput(); err != nil {
		return fmt.Errorf("ip link up error: %v, output: %s", err, string(out))
	}

	// 設定 MTU (與 Windows 端統一為 1280，防止大封包丟失)
	cmdMTU := exec.Command("ip", "link", "set", "dev", d.Name, "mtu", "1280")
	if out, err := cmdMTU.CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set mtu error: %v, output: %s", err, string(out))
	}

	// ip addr add <ip>/10 dev <name>
	cmdAddr := exec.Command("ip", "addr", "add", ip+"/10", "dev", d.Name)
	if out, err := cmdAddr.CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr add error: %v, output: %s", err, string(out))
	}

	return nil
}

func New(name string, ip string, onWait func()) (Device, error) {
	if onWait != nil {
		onWait()
	}
	return NewLinuxDevice(name, ip)
}
