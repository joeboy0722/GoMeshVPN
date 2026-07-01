# GoMeshServer

GoMeshServer 是 GoMeshVPN 的伺服器端應用程式。本專案使用 Wails 框架建構，同時支援桌面 GUI 介面與無 GUI 的命令列控制台（Console Mode），適合於個人桌面或 Linux/Windows 伺服器部署。

## 功能特點
- **雙模式支援**：無參數執行時自動開啟桌面 GUI 介面；帶有命令列參數執行時自動切換至 Console 互動模式。
- **即時命令控制**：命令列模式下支援 `start`、`stop`、`status`、`shutdown` 等互動指令。
- **無縫背景部署**：提供 PID 記錄，可由外部指令/指令碼（如 `stop`）優雅停用背景伺服器。

---

## 快速開始 (命令列模式)

要在伺服器上無 GUI 啟動，請在終端機（CMD / PowerShell）中執行：

```cmd
GoMeshServer.exe start -tcp_port 8889 -udp_port 8888 -auto_registration true
```
或者省略 `start` 子命令：
```cmd
GoMeshServer.exe -tcp_port 8889 -udp_port 8888 -auto_registration true
```

### 命令列參數 (Flags)
- `-tcp_port`：指定 TCP 監聽埠 (預設 `"8889"`)
- `-udp_port`：指定 UDP 監聽埠 (預設 `"8888"`)
- `-auto_registration`：是否開啟用戶自動註冊功能 (預設 `false`)

### 互動指令說明
啟動後會出現 `>` 提示符，您可以直接輸入：
- `status`：查看當前 VPN 服務狀態。
- `stop`：暫時停止 VPN 服務（程式本身保持運行）。
- `start`：重新啟動 VPN 服務。
- `shutdown` 或 `exit`：停止 VPN 服務並關閉程式。
- `help`：顯示可用指令說明。

### 外部控制 (非互動模式)
若伺服器已在背景運行，您可以在另一個視窗直接輸入以下指令安全停止它：
```cmd
GoMeshServer.exe stop
```
*這會自動讀取同目錄下的 `server.pid` 並關閉對應進程。*

---

## 快速開始 (GUI 桌面模式)

直接雙擊 `GoMeshServer.exe` 執行，或者在不帶任何參數的情況下透過 CMD 啟動即可。在 Windows 上啟動 GUI 模式時，程式會自動隱藏彈出的黑色控制台視窗。

---

## 建置與打包

本專案需要 Go 語言支援。

### 1. 純命令列模式打包 (推薦伺服器端部署 💡)
若您要在 Windows Server 等無 GUI 或使用 Session 0 背景管理的環境下部署，請使用 `headless` 標籤編譯。此版本完全不依賴 GUI 視窗與 WebView2，體積更小且絕不會報 DLL 載入失敗錯誤：
```bash
go build -tags headless -o GoMeshServer-cli.exe .
```
*這會產生帶有 `-cli` 尾綴的純命令列執行檔。*

### 2. 使用 Wails 打包 (包含 GUI 介面)
如果您需要保留桌面 GUI 面板，且希望在 CMD 執行時能進行指令互動，請在 Wails 打包時加上 `-windowsconsole`：
```bash
wails build -windowsconsole
```
*打包後的執行檔會位於 `build/bin/GoMeshServer.exe`。在 Windows 雙擊運行 GUI 時，程式內部會自動隱藏彈出的黑色控制台視窗。*

### 3. 本地開發偵錯
- 啟動熱重載開發環境：
  ```bash
  wails dev
  ```
