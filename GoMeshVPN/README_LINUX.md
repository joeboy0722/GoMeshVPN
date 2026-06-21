# GoMeshVPN for Linux

## Prerequisites

1.  **Go**: Version 1.21+
2.  **C Compiler**: `gcc`
3.  **Library Dependencies**:
    *   `libgtk-3-dev`
    *   `libwebkit2gtk-4.0-dev` (or 4.1)

    **Debian/Ubuntu:**
    ```bash
    sudo apt update
    sudo apt install build-essential libgtk-3-dev libwebkit2gtk-4.0-dev
    ```

    **Fedora:**
    ```bash
    sudo dnf install gtk3-devel webkit2gtk3-devel
    ```

    **Arch Linux:**
    ```bash
    sudo pacman -S gtk3 webkit2gtk
    ```

## Build Instructions

### Method 1: Using Wails (Recommended)

1.  Install Wails CLI:
    ```bash
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    ```
2.  Build:
    ```bash
    wails build
    ```
    The binary will be in `build/bin/GoMeshVPN`.

### Method 2: Manual Build (If Wails CLI is not available)

1.  Build Frontend:
    ```bash
    cd frontend
    npm install
    npm run build
    cd ..
    ```
2.  Build Backend:
    ```bash
    go mod tidy
    go build -tags linux -o GoMeshVPN
    ```

## Running

Linux requires `root` privileges to create TUN interfaces (`/dev/net/tun`) and configure IP addresses.

```bash
sudo ./build/bin/GoMeshVPN
# OR
sudo ./GoMeshVPN
```
