# GoMeshVPN Linux 用戶端 - 從零開始打包手冊

本指南假設您擁有一台全新的 Linux 虛擬機（以 **Ubuntu / Debian** 為例），裡面沒有安裝過任何開發環境。只要按照以下步驟依序執行，就能成功打包並執行帶有前端介面的 `GoMeshVPN`。

---

## 階段一：準備基礎環境

在全新的 Linux 系統中，我們需要先更新軟體源，並安裝最基本的系統底層套件與 C 語言編譯器。

1. **更新軟體源清單：**
   ```bash
   sudo apt update
   sudo apt upgrade -y
   ```

2. **安裝編譯工具 (C 編譯器與 Make)：**
   ```bash
   sudo apt install -y build-essential
   ```

---

## 階段二：安裝 GUI 介面依賴

Wails 應用程式的圖形介面在 Linux 上依賴 GTK3 與 WebKit 引擎。缺少這些依賴將無法成功編譯。

1. **安裝 GTK3 和 WebKit2 開發庫：**
   ```bash
   sudo apt install -y libgtk-3-dev libwebkit2gtk-4.0-dev
   ```

*(備註：如果是較新的 Ubuntu，例如 24.04，可能需要將套件名稱換成 `libwebkit2gtk-4.1-dev`)*

---

## 階段三：安裝 Go 語言 (Golang)

Wails 後端由 Go 語言編寫，我們需要手動安裝最新版本的 Go（需 >= 1.21）。

1. **下載 Go 壓縮檔：**
   ```bash
   wget https://go.dev/dl/go1.23.0.linux-amd64.tar.gz
   ```

2. **解壓縮並安裝到系統目錄：**
   ```bash
   sudo rm -rf /usr/local/go
   sudo tar -C /usr/local -xzf go1.23.0.linux-amd64.tar.gz
   ```

3. **設定環境變數：**
   將 Go 的執行路徑加入到你的 `~/.bashrc` 中：
   ```bash
   echo "export PATH=\$PATH:/usr/local/go/bin" >> ~/.bashrc
   echo "export PATH=\$PATH:\$HOME/go/bin" >> ~/.bashrc
   source ~/.bashrc
   ```

4. **驗證安裝：**
   ```bash
   go version
   ```
   *應輸出類似 `go version go1.23.0 linux/amd64`*

---

## 階段四：安裝 Node.js 與 npm

Wails 在打包時，需要呼叫 Node.js 來編譯你的前端 Vue/React 資源。最穩定的安裝方式是透過 NodeSource。

1. **加入 Node.js 官方源 (以 LTS 版本 20 為例)：**
   ```bash
   curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
   ```

2. **安裝 Node.js：**
   ```bash
   sudo apt install -y nodejs
   ```

3. **驗證安裝：**
   ```bash
   node -v
   npm -v
   ```

---

## 階段五：安裝 Wails 打包工具

既然 Go 與 Node.js 都準備好了，我們現在可以安裝 Wails 本身。

1. **透過 Go 安裝 Wails CLI：**
   ```bash
   go install github.com/wailsapp/wails/v2/cmd/wails@latest
   ```

2. **驗證 Wails 安裝：**
   ```bash
   wails doctor
   ```
   *執行這行指令時，Wails 會檢查系統環境，如果全部顯示綠色打勾，代表你的 Linux 已經完美準備好打包了！*

---

## 階段六：傳輸程式碼並打包 (Build)

1. **將程式碼傳進虛擬機：**
   把你在 Windows 上的整個 `GoMeshVPN_linux` 資料夾透過 SFTP、SCP 或是 Git 複製到這台虛擬機上。
   **注意：如果你是直接從 Windows 複製過去，必定會遺失 Linux 的執行權限。進入目錄後，請務必先刪除前端的快取，讓虛擬機重新下載：**
   ```bash
   cd ~/GoMeshVPN_linux/frontend  # 進入前端目錄
   rm -rf node_modules package-lock.json  # 刪除從 Windows 複製過來的舊包
   npm install  # 讓 Linux 重新下載與建立具有正確執行權限的編譯工具
   cd ..  # 回到主目錄
   ```

2. **進入專案主目錄：**
   ```bash
   cd ~/GoMeshVPN_linux  # 確保你目前在後端專案的根目錄
   ```

3. **執行 Wails 終極打包指令：**
   ```bash
   wails build -tags webkit2_41
   ```
   
   這行指令會自動：
   - 使用 npm 下載前端依賴。
   - 將前端網頁資源編譯壓縮。
   - 下載 Go 的依賴包。
   - 將前端和後端一起打包成一隻原生的 Linux 執行檔。

---

## 階段七：啟動 VPN！

打包結束後，你會在專案目錄底下的 `build/bin/` 裡面找到熱騰騰的 `GoMeshVPN` 執行檔。

**特別注意**：VPN 在底層需要建立 `TUN` 虛擬網卡（`/dev/net/tun`）並設定路由表，這屬於 Linux 的核心層級操作，**必須宣告最高權限**才能執行。

1. **啟動 VPN 程式：**
   ```bash
   sudo ./build/bin/GoMeshVPN
   ```

程式成功啟動後，你就會在 Linux 桌面上看到 VPN 的登入介面了！

---

## 💡 跨架構與 ARM64 特別說明

### 1. AMD64 vs ARM64 不相容
如果你在 **AMD64** (一般 Intel/AMD 處理器) 的虛擬機上打包，產出的執行檔**無法**直接在 **ARM64** (如 Apple Silicon M1/M2 虛擬機、Raspberry Pi、或是 ARM 版運算執行個體) 上執行。

### 2. 如何為 ARM64 打包？
由於本專案使用了 CGO (為了連接 Linux 的圖形庫)，直接進行跨架構編譯 (Cross-compile) 非常複雜。**最簡單且推薦的方式**是：
- **在 ARM64 的 Linux 環境中重複上述手冊步驟**：在 ARM64 的虛擬機中安裝 Go、Node.js 與 GTK 依賴，然後直接執行 `wails build`，產出的就會是原生支援 ARM64 的執行檔。

### 3. Wails 跨架構指令（僅供參考）
如果你具備交叉編譯鏈環境，Wails 支援指定平台：
```bash
wails build -platform linux/arm64
```
*注意：在沒有設定好交叉編譯工具 (aarch64-linux-gnu-gcc) 的情況下，此指令通常會因為 CGO 錯誤而失敗。*
