import './style.css';

// State
let groupsData = {}; // { "GroupName": [peers...] }
let targetGroupForAction = "";
let connectionStatus = "DISCONNECTED"; // CONNECTED, CONNECTING, DISCONNECTED

// Wails Runtime Events
const runtime = window.runtime;
const goApp = window.go.main.App;

// UI Elements
const consoleBox = document.getElementById('console-log');
const peerList = document.getElementById('peer-list');
const peerCount = document.getElementById('peer-count');
const myIpDisplay = document.getElementById('my-ip');
const currentGroupDisplay = document.getElementById('current-group');
const ctxMenu = document.getElementById('context-menu');
const btnLeave = document.getElementById('btn-leave-group');

const views = {
    login: document.getElementById('app-login'),
    dashboard: document.getElementById('app-dashboard')
};

// --- Helper Functions ---

function log(msg) {
    const div = document.createElement('div');
    div.className = 'log-line';
    const time = new Date().toLocaleTimeString();
    div.innerText = `[${time}] ${msg}`;
    consoleBox.appendChild(div);
    consoleBox.scrollTop = consoleBox.scrollHeight;
}

function updateConnectionStatus(status) {
    connectionStatus = status;
    const statusDisplay = document.getElementById('status-display');

    if (status === 'CONNECTED') {
        statusDisplay.innerText = 'SECURE';
        statusDisplay.className = 'value text-green';
    } else if (status === 'CONNECTING') {
        statusDisplay.innerText = 'CONNECTING...';
        statusDisplay.className = 'value text-yellow';
    } else {
        statusDisplay.innerText = 'OFFLINE';
        statusDisplay.className = 'value text-red';
    }
}

function switchView(viewName) {
    if (viewName === 'dashboard') {
        const login = document.getElementById('app-login');
        const dash = document.getElementById('app-dashboard');

        login.classList.remove('visible');
        login.classList.add('hidden');
        dash.classList.remove('hidden');
        dash.classList.add('visible');
    }
}

// 載入上次登入資訊
function loadLastLogin() {
    const lastAddr = localStorage.getItem('vpn_last_addr');
    const lastUser = localStorage.getItem('vpn_last_user');

    if (lastAddr) {
        document.getElementById('login-addr').value = lastAddr;
    }
    if (lastUser) {
        document.getElementById('login-user').value = lastUser;
    }
}

// 儲存登入資訊
function saveLogin(addr, user) {
    localStorage.setItem('vpn_last_addr', addr);
    localStorage.setItem('vpn_last_user', user);
}

// --- Render Logic ---

function renderGroups() {
    peerList.innerHTML = '';

    let totalPeers = 0;
    const groupNames = Object.keys(groupsData).sort();

    groupNames.forEach(gName => {
        const peers = groupsData[gName];
        totalPeers += peers.length;

        // Container
        const groupBlock = document.createElement('li');
        groupBlock.className = 'group-block';

        // Header
        const header = document.createElement('div');
        header.className = 'group-header';
        header.innerText = `// SECTOR: ${gName} [${peers.length}]`;

        // Context Menu Trigger
        header.addEventListener('contextmenu', (e) => {
            e.preventDefault();
            showContextMenu(e.clientX, e.clientY, gName);
        });

        groupBlock.appendChild(header);

        // Peers UL
        const ul = document.createElement('ul');
        ul.className = 'group-peers';

        peers.forEach(p => {
            // Debug: 輸出 peer 資料以診斷問題
            console.log('[DEBUG] Peer:', p);

            const li = document.createElement('li');
            // 根據在線狀態添加不同的 class
            // 注意：JSON 欄位名稱可能是 is_online 或 IsOnline
            const isOnline = p.is_online || p.IsOnline || false;
            li.className = isOnline ? 'peer-card online' : 'peer-card offline';
            li.innerHTML = `
                <div class="peer-info">
                    <span class="peer-ip">${p.virtual_ip || p.VirtualIP}</span>
                    <span class="peer-user">${p.username || p.Username}</span>
                </div>
                <div class="signal-bar ${isOnline ? 'active' : 'inactive'}"></div>
            `;
            ul.appendChild(li);
        });

        groupBlock.appendChild(ul);
        peerList.appendChild(groupBlock);
    });

    peerCount.innerText = totalPeers;
}

// --- Context Menu ---

function showContextMenu(x, y, groupName) {
    targetGroupForAction = groupName;
    ctxMenu.style.display = 'flex';
    ctxMenu.style.left = x + 'px';
    ctxMenu.style.top = y + 'px';
    btnLeave.innerText = `LEAVE SECTOR: ${groupName}`;
}

function hideContextMenu() {
    ctxMenu.style.display = 'none';
}

document.addEventListener('click', hideContextMenu);

btnLeave.addEventListener('click', async () => {
    if (!targetGroupForAction) return;

    log(`[CMD] Leaving Sector: ${targetGroupForAction}...`);
    try {
        const result = await goApp.LeaveGroup(targetGroupForAction);
        if (result === 'success') {
            log(`Success. Disconnected from ${targetGroupForAction}.`);
            delete groupsData[targetGroupForAction];
            renderGroups();
        } else {
            log(`[ERR] Failed to leave: ${result}`);
        }
    } catch (e) {
        log(`[ERR] ${e}`);
    }
});


// --- Go Interaction ---

window.doConnect = async function () {
    const addr = document.getElementById('login-addr').value;
    const user = document.getElementById('login-user').value;
    const pass = document.getElementById('login-pass').value;
    const msgBox = document.getElementById('login-msg');

    if (!addr || !user) {
        msgBox.innerText = "ERROR: MISSING_FIELDS";
        return;
    }

    log("Initiating Handshake Protocol...");
    msgBox.innerText = "CONNECTING...";
    updateConnectionStatus('CONNECTING');

    try {
        const result = await goApp.Connect(addr, user, pass);

        if (result === "success") {
            log("Connection Established.");
            msgBox.innerText = "";
            updateConnectionStatus('CONNECTED');

            // 儲存登入資訊
            saveLogin(addr, user);

            const myIP = await goApp.GetMyIP();
            myIpDisplay.innerText = myIP;

            switchView('dashboard');
        } else {
            msgBox.innerText = "FAIL: " + result;
            log("Connection Failed: " + result);
            updateConnectionStatus('DISCONNECTED');
        }
    } catch (e) {
        msgBox.innerText = "CRITICAL ERROR";
        console.error(e);
        updateConnectionStatus('DISCONNECTED');
    }
};

window.doDisconnect = function () {
    updateConnectionStatus('DISCONNECTED');
    // 清除所有狀態
    groupsData = {};
    location.reload();
};

window.doJoinGroup = async function () {
    const name = document.getElementById('group-name').value;
    const pass = document.getElementById('group-pass').value;
    if (!name) return;

    log(`Requesting access to sector: ${name}`);
    const res = await goApp.JoinGroup(name, pass);
    if (res === "success") {
        log(`Access Granted: ${name}`);
        // Wait for peer-update to populate list
    } else {
        log(`Access Denied: ${res}`);
    }
};

window.doCreateGroup = async function () {
    const name = document.getElementById('group-name').value;
    const pass = document.getElementById('group-pass').value;
    if (!name) return;

    log(`Initializing new sector protocols: ${name}...`);
    const res = await goApp.CreateGroup(name, pass);

    if (res === "success") {
        log(`Sector Initialized: ${name}`);
    } else {
        log(`Initialization Failed: ${res}`);
    }
};

// --- Events Listener ---

runtime.EventsOn("peer-update", (payload) => {
    // Payload is now StatusPayload: { message, virtual_ip, group_name, peers }
    log(`[NET] Update Signal. Group: ${payload.group_name || 'N/A'}, Peers: ${payload.peers ? payload.peers.length : 0}`);

    if (payload.group_name) {
        if (payload.peers && payload.peers.length > 0) {
            groupsData[payload.group_name] = payload.peers;
        } else {
            // Empty update likely means no other peers, but we are still in it.
            groupsData[payload.group_name] = [];
        }
        renderGroups();
    } else {
        // Fallback for unexpected payloads or initial status?
        if (payload.message) log(`[MSG] ${payload.message}`);
    }
});

runtime.EventsOn("status-update", (msg) => {
    log(`SYSTEM: ${msg}`);

    if (msg === "Disconnected") {
        updateConnectionStatus('DISCONNECTED');
    } else if (msg === "Reconnecting") {
        updateConnectionStatus('CONNECTING');
    } else if (msg === "Connected") {
        updateConnectionStatus('CONNECTED');
    }
});

// Initialize
loadLastLogin();

// 綁定 Enter 鍵進行登入
const loginInputs = ['login-addr', 'login-user', 'login-pass'];
loginInputs.forEach(id => {
    const el = document.getElementById(id);
    if (el) {
        el.addEventListener('keydown', (event) => {
            if (event.key === 'Enter') {
                window.doConnect();
            }
        });
    }
});