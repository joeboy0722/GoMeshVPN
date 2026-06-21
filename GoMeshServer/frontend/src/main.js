import './style.css';

let isRunning = false;

// Wails runtime binding is standard: window.runtime or via go.main.App methods

// LOG Handling
const logConsole = document.getElementById('log-console');
function log(msg) {
    const p = document.createElement('p');
    const time = new Date().toLocaleTimeString();
    p.innerText = `[${time}] ${msg}`;
    logConsole.appendChild(p);
    logConsole.scrollTop = logConsole.scrollHeight;
}

// Override console.log to print to UI
const oldLog = console.log;
console.log = function (...args) {
    oldLog(...args);
    log(args.join(' '));
};

// Listen for Server Logs
if (window.runtime) {
    window.runtime.EventsOn("server-log", (msg) => {
        // Msg usually has newline, trim it
        log("[SERVER] " + msg.trim());
    });
}

// --- Page Navigation ---
window.showPage = function (pageId) {
    document.querySelectorAll('.page').forEach(el => el.classList.remove('active'));
    document.querySelectorAll('nav button').forEach(el => el.classList.remove('active'));

    document.getElementById(pageId).classList.add('active');
    // Find button that calls this
    const btn = Array.from(document.querySelectorAll('nav button')).find(b => b.getAttribute('onclick').includes(pageId));
    if (btn) btn.classList.add('active');

    // Refresh Data
    if (pageId === 'users') refreshUsers();
    if (pageId === 'groups') refreshGroups();
};

// --- Server Control ---
window.toggleServer = async function () {
    const btn = document.getElementById('btn-toggle-server');
    const tcp = document.getElementById('config-tcp').value;
    const udp = document.getElementById('config-udp').value;
    const autoReg = document.getElementById('config-autoreg').checked;

    if (!isRunning) {
        // Start
        log("Initializing Server...");
        try {
            const res = await window.go.main.App.StartServer(tcp, udp, autoReg);
            if (res === "Success") {
                isRunning = true;
                updateStatus(true);
                log(`Server Started on TCP:${tcp} / UDP:${udp}`);
                btn.innerText = "TERMINATE SERVER";
                btn.classList.add("danger");
                // Disable inputs
                document.getElementById('config-tcp').disabled = true;
                document.getElementById('config-udp').disabled = true;
                document.getElementById('config-autoreg').disabled = true;
            } else {
                log("Error starting server: " + res);
            }
        } catch (e) {
            log("Exception: " + e);
        }
    } else {
        // Stop
        try {
            const res = await window.go.main.App.StopServer();
            isRunning = false;
            updateStatus(false);
            log("Server Stopped.");
            btn.innerText = "INITIALIZE SERVER";
            btn.classList.remove("danger");
            // Enable inputs
            document.getElementById('config-tcp').disabled = false;
            document.getElementById('config-udp').disabled = false;
            document.getElementById('config-autoreg').disabled = false;
        } catch (e) {
            log("Exception: " + e);
        }
    }
};

function updateStatus(running) {
    const dot = document.getElementById('status-dot');
    const text = document.getElementById('status-text');
    if (running) {
        dot.classList.add('running');
        text.innerText = "RUNNING";
        text.style.color = "#0f0";
    } else {
        dot.classList.remove('running');
        text.innerText = "STOPPED";
        text.style.color = "red";
    }
}

// --- User Management ---
async function refreshUsers() {
    const tbody = document.getElementById('user-list');
    tbody.innerHTML = '<tr><td colspan="4">Loading...</td></tr>';

    try {
        const users = await window.go.main.App.GetAllUsers();
        tbody.innerHTML = '';
        if (!users || users.length === 0) {
            tbody.innerHTML = '<tr><td colspan="4">No users found.</td></tr>';
            return;
        }

        users.forEach(u => {
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td>${u.id}</td>
                <td style="color:#fff; font-weight:bold;">${u.username}</td>
                <td>${u.virtual_ip}</td>
                <td class="action-cell">
                    <button class="btn danger" onclick="deleteUser(${u.id})">DEL</button>
                </td>
            `;
            tbody.appendChild(tr);
        });
    } catch (e) {
        tbody.innerHTML = `<tr><td colspan="4" style="color:red">Error: ${e}</td></tr>`;
    }
}

window.createUser = async function () {
    const user = document.getElementById('new-user-name').value;
    const pass = document.getElementById('new-user-pass').value;
    if (!user || !pass) {
        alert("Username and Password required");
        return;
    }

    const res = await window.go.main.App.CreateUser(user, pass);
    if (res === "Success") {
        closeModal('modal-add-user');
        document.getElementById('new-user-name').value = "";
        document.getElementById('new-user-pass').value = "";
        refreshUsers();
        log(`User ${user} created manually.`);
    } else {
        alert("Error: " + res);
    }
};

window.deleteUser = async function (id) {
    if (!confirm(`Delete User ID ${id}? This cannot be undone.`)) return;
    const res = await window.go.main.App.DeleteUser(id);
    if (res === "Success") {
        refreshUsers();
        log(`User ID ${id} deleted.`);
    } else {
        alert("Error: " + res);
    }
};

// --- Group Management ---
async function refreshGroups() {
    const tbody = document.getElementById('group-list');
    tbody.innerHTML = '<tr><td colspan="4">Loading...</td></tr>';

    try {
        const groups = await window.go.main.App.GetAllGroups();
        tbody.innerHTML = '';
        if (!groups || groups.length === 0) {
            tbody.innerHTML = '<tr><td colspan="4">No groups found.</td></tr>';
            return;
        }

        groups.forEach(g => {
            const tr = document.createElement('tr');
            tr.innerHTML = `
                <td>${g.id}</td>
                <td style="color:#fff; font-weight:bold;">${g.name}</td>
                <td>***</td>
                <td class="action-cell">
                    <button class="btn danger" onclick="deleteGroup(${g.id})">DEL</button>
                </td>
            `;
            tbody.appendChild(tr);
        });
    } catch (e) {
        tbody.innerHTML = `<tr><td colspan="4" style="color:red">Error: ${e}</td></tr>`;
    }
}

window.createGroup = async function () {
    const name = document.getElementById('new-group-name').value;
    const pass = document.getElementById('new-group-pass').value;
    if (!name) {
        alert("Group Name required");
        return;
    }

    const res = await window.go.main.App.CreateGroup(name, pass);
    if (res === "Success") {
        closeModal('modal-add-group');
        document.getElementById('new-group-name').value = "";
        document.getElementById('new-group-pass').value = "";
        refreshGroups();
        log(`Group ${name} created manually.`);
    } else {
        alert("Error: " + res);
    }
};

window.deleteGroup = async function (id) {
    if (!confirm(`Delete Group ID ${id}?`)) return;
    const res = await window.go.main.App.DeleteGroup(id);
    if (res === "Success") {
        refreshGroups();
        log(`Group ID ${id} deleted.`);
    } else {
        alert("Error: " + res);
    }
};

// --- Modals ---
window.openModal = function (id) {
    document.getElementById(id).classList.add('active');
};
window.closeModal = function (id) {
    document.getElementById(id).classList.remove('active');
};

// Init
// Poll status to see if server is already running (e.g. reload page)
setInterval(async () => {
    try {
        const running = await window.go.main.App.IsServerRunning();
        if (running !== isRunning) {
            isRunning = running;
            updateStatus(running);
            // Sync button state if needed, though pure poll might flicker inputs
            const btn = document.getElementById('btn-toggle-server');
            if (running) {
                btn.innerText = "TERMINATE SERVER";
                btn.classList.add("danger");
                document.getElementById('config-tcp').disabled = true;
                document.getElementById('config-udp').disabled = true;
                document.getElementById('config-autoreg').disabled = true;
            } else {
                btn.innerText = "INITIALIZE SERVER";
                btn.classList.remove("danger");
                document.getElementById('config-tcp').disabled = false;
                document.getElementById('config-udp').disabled = false;
                document.getElementById('config-autoreg').disabled = false;
            }
        }
    } catch (e) { }
}, 2000);
