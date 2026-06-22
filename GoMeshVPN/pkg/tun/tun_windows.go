//go:build windows

package tun

import (
	"crypto/md5"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wintun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

type WintunDevice struct {
	Adapter *wintun.Adapter
	Session wintun.Session
	Name    string
	IP      string
	mu      sync.Mutex
}

func generateGUIDFromName(name string) *windows.GUID {
	// 使用 MD5 雜湊來產生固定一致的 GUID，防止每次重新連線時建立新的網路介面卡並產生 "GoMeshVPN 103" 等無限遞增的數字
	hash := md5.Sum([]byte(name))
	return &windows.GUID{
		Data1: uint32(hash[0]) | uint32(hash[1])<<8 | uint32(hash[2])<<16 | uint32(hash[3])<<24,
		Data2: uint16(hash[4]) | uint16(hash[5])<<8,
		Data3: uint16(hash[6]) | uint16(hash[7])<<8,
		Data4: [8]byte{hash[8], hash[9], hash[10], hash[11], hash[12], hash[13], hash[14], hash[15]},
	}
}

func NewWintunDevice(name string, ip string, onWait func()) (*WintunDevice, error) {
	var adapter *wintun.Adapter
	var session wintun.Session
	var err error

	guid := generateGUIDFromName(name)

	// 1. 優先嘗試直接打開現有的 Wintun 適配器 (瞬間完成，免去 PnP 的 15 秒延遲)
	adapter, err = wintun.OpenAdapter(name)
	if err == nil {
		fmt.Printf("[INFO] 成功打開現有的 Wintun 網路介面卡: %s\n", name)
		fmt.Printf("[INFO] 正在現有介面卡上啟動 Wintun 會話...\n")
		session, err = adapter.StartSession(0x800000)
		if err != nil {
			fmt.Printf("[WARN] 在現有介面卡上啟動會話失敗: %v, 將嘗試全新重建...\n", err)
			adapter.Close()
			adapter = nil
		}
	}

	// 2. 如果不存在現有介面卡，或者開啟後啟動會話失敗，則回退至原本的 CreateAdapter 流程
	if adapter == nil {
		fmt.Printf("[INFO] 未找到現有介面卡或其不可用，開始創建新的 Wintun 網路介面卡...\n")
		maxRetries := 3
		for i := 0; i < maxRetries; i++ {
			fmt.Printf("[INFO] 正在創建 Wintun 網路介面卡 (嘗試 %d/%d)...\n", i+1, maxRetries)
			adapter, err = wintun.CreateAdapter(name, "GoMeshVPN", guid)
			if err != nil {
				fmt.Printf("[WARN] CreateAdapter 失敗: %v, 正在重試...\n", err)
				if onWait != nil {
					onWait()
				}
				time.Sleep(time.Second)
				continue
			}

			fmt.Printf("[INFO] 正在啟動 Wintun 會話...\n")
			session, err = adapter.StartSession(0x800000)
			if err == nil {
				break
			}

			fmt.Printf("[WARN] 啟動會話失敗 (嘗試 %d): %v\n", i+1, err)
			adapter.Close()
			adapter = nil
			if onWait != nil {
				onWait()
			}
			time.Sleep(time.Second)
		}
	}

	if adapter == nil {
		return nil, fmt.Errorf("在多次重試後仍無法初始化 Wintun 介面卡: %v", err)
	}

	dev := &WintunDevice{
		Adapter: adapter,
		Session: session,
		Name:    name,
		IP:      ip,
	}

	// 呼叫改寫為 WinAPI 的 SetIP
	if err := dev.SetIP(ip); err != nil {
		// SetIP 失敗時，必須 Destroy 而非 Close，
		// 否則損壞的 Adapter 會殘留，導致下次 OpenAdapter 拿到 IP 配置異常的舊網卡
		dev.Destroy()
		return nil, err
	}

	return dev, nil
}

func (d *WintunDevice) SetIP(ip string) error {
	// 【修正】將 wintun 的 LUID 轉型為 winipcfg 所需的型別
	luid := winipcfg.LUID(d.Adapter.LUID())

	// 解析 IP 與子網路遮罩 (/10)
	ipPrefix, err := netip.ParsePrefix(fmt.Sprintf("%s/10", ip))
	if err != nil {
		return fmt.Errorf("invalid IP format: %v", err)
	}

	// 使用 WinAPI 直接設定 IP 與路由 (瞬間完成，免外部 netsh)
	err = luid.SetIPAddresses([]netip.Prefix{ipPrefix})
	if err != nil {
		return fmt.Errorf("winipcfg set ip error: %v", err)
	}

	// 設定 MTU (改為 1280 以提高不同網路環境下的相容性，防範大封包丟失)
	ipif, err := luid.IPInterface(windows.AF_INET)
	if err != nil {
		fmt.Printf("[WARN] Failed to get IP interface: %v\n", err)
	} else {
		ipif.NLMTU = 1280
		if err := ipif.Set(); err != nil {
			fmt.Printf("[WARN] Failed to set MTU via winipcfg: %v\n", err)
		} else {
			fmt.Printf("[INFO] MTU set to 1280 via WinAPI\n")
		}
	}

	return nil
}

func (d *WintunDevice) ReadPacket() ([]byte, error) {
	pkt, err := d.Session.ReceivePacket()
	switch err {
	case nil:
		buf := make([]byte, len(pkt))
		copy(buf, pkt)
		d.Session.ReleaseReceivePacket(pkt)
		return buf, nil
	case windows.ERROR_NO_MORE_ITEMS:
		windows.WaitForSingleObject(d.Session.ReadWaitEvent(), windows.INFINITE)
		return d.ReadPacket()
	default:
		return nil, err
	}
}

func (d *WintunDevice) WritePacket(data []byte) error {
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
		return nil
	default:
		return err
	}
}

func (d *WintunDevice) Destroy() {
	d.Session.End()

	if d.Adapter != nil {
		d.Adapter.Close()
	}
}

func (d *WintunDevice) Close() {
	// 僅關閉 Session，保留網卡 Adapter 以便運行或重連時秒開，避免每次都經歷 15 秒的 Windows PnP 建立延遲
	d.Session.End()
}

func (d *WintunDevice) InterfaceName() string {
	return d.Name
}

func New(name string, ip string, onWait func()) (Device, error) {
	return NewWintunDevice(name, ip, onWait)
}
