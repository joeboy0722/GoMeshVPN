# 第三方軟體授權說明 (Third-Party Licenses)

本專案 `GoMeshVPN` 採用 **MIT License** 授權釋出。專案中整合或使用到了多個第三方開源軟體套件，其各自的授權資訊如下所列：

---

## 1. 主要第三方套件 (Main Dependencies)

### 1.1 Wails
* **專案網址**: [github.com/wailsapp/wails/v2](https://github.com/wailsapp/wails/v2)
* **授權類型**: **MIT License**
* **用途**: 提供 Go 與前端 HTML/JS 溝通的核心 GUI 框架。

### 1.2 go-sqlite3
* **專案網址**: [github.com/mattn/go-sqlite3](https://github.com/mattn/go-sqlite3)
* **授權類型**: **MIT License**
* **用途**: 用於伺服器端使用者資料與群組資料庫的 SQLite3 驅動。

### 1.3 Go 官方輔助庫
* **專案網址**: [golang.org/x/...](https://golang.org) (`sys`, `crypto`, `net`, `text`)
* **授權類型**: **BSD-3-Clause License**
* **用途**: 提供系統底層呼叫、加密演算法以及網路協議處理。

---

## 2. Windows 虛擬網卡驅動與相關封裝 (VPN Driver)

### 2.1 Wintun Driver & Go Binding
* **專案網址**: 
  - Wintun 驅動: [wintun.net](https://www.wintun.net/)
  - Go Binding: [golang.zx2c4.com/wintun](https://golang.zx2c4.com/wintun)
* **授權類型**: **GPL-2.0-only** (GNU General Public License v2)
* **用途**: Windows 平台下用來建立虛擬網路介面卡 (TUN) 以便轉發 VPN 封包。
* **注意事項**: 
  - Wintun 驅動程式以 GPL-2.0 授權。本專案透過動態加載方式呼叫其 API，且本專案本身已完全開源。
  - 若您計畫將本專案用於**商業閉源**發行，您將必須自行向 Wintun 作者（Jason A. Donenfeld）申請商業授權，否則可能會違反 GPLv2 條款。

### 2.2 WireGuard Windows Helper
* **專案網址**: [golang.zx2c4.com/wireguard/windows](https://golang.zx2c4.com/wireguard/windows)
* **授權類型**: **MIT License / GPL-2.0-only**
* **用途**: Windows 下 WireGuard/Wintun 整合與底層輔助庫。

---

## 3. 前端開發工具 (Frontend Dev Tooling)

### 3.1 Vite
* **專案網址**: [vitejs.dev](https://vitejs.dev/)
* **授權類型**: **MIT License**
* **用途**: 用於前端 Vanilla JS/CSS 的建置與打包工具。僅在開發階段（devDependencies）使用，不會隨編譯成品散佈。
