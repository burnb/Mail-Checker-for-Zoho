const api = typeof browser !== "undefined" ? browser : chrome;

async function getBackendUrl() {
    const { backendUrl } = await api.storage.local.get("backendUrl");
    return backendUrl;
}

async function openOAuth() {
    const { session_id } = await api.storage.local.get("session_id");
    const backendUrl = await getBackendUrl();
    const callbackUrl = api.runtime.getURL("callback.html");
    api.tabs.create({ url: `${backendUrl}/auth/zoho?session_id=${encodeURIComponent(session_id)}&extension_callback=${encodeURIComponent(callbackUrl)}` });
}

// Request deduplication
let listLoading = false;
let foldersLoading = false;
let lastListLoad = 0;
const LIST_DEBOUNCE_MS = 500; // Min 500ms between list loads

// Helper to clear syntax safety
function clearContent(element) {
    while (element.firstChild) {
        element.removeChild(element.firstChild);
    }
}

// Theme management
function initTheme() {
    const savedTheme = localStorage.getItem("theme") || "default";
    document.body.setAttribute("data-theme", savedTheme);
}

function toggleTheme() {
    const current = document.body.getAttribute("data-theme");
    const next = current === "light-popup" ? "default" : "light-popup";
    document.body.setAttribute("data-theme", next);
    localStorage.setItem("theme", next);
}

// Time formatting
function formatMailDate(timestamp) {
    const value = typeof timestamp === "string" && /^\d+$/.test(timestamp)
        ? Number(timestamp)
        : timestamp;
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) {
        return "";
    }
    const now = new Date();

    if (d.toDateString() === now.toDateString()) {
        return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }).toLowerCase();
    }

    const yesterday = new Date();
    yesterday.setDate(now.getDate() - 1);
    if (d.toDateString() === yesterday.toDateString()) {
        return "Yesterday";
    }

    return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
}

// Update WebSocket connection status indicator
async function updateStatus() {
    const indicator = document.querySelector(".status-indicator");
    if (!indicator) return;

    const { eventsConnected } = await api.storage.local.get("eventsConnected");
    if (eventsConnected) {
        indicator.classList.remove("offline");
    } else {
        indicator.classList.add("offline");
    }
}

// Update UI state
async function updateUI() {
    const data = await api.storage.local.get(["lastUnread", "authError", "jwt", "accountEmail"]);

    const userBar = document.getElementById("userBar");
    const mainContent = document.getElementById("mainContent");
    const userEmail = document.getElementById("userEmail");
    const avatarInitial = document.getElementById("avatarInitial");
    const unreadBadge = document.getElementById("unreadBadge");

    if (!data.jwt) {
        // No account at all
        userBar.style.display = "none";
        clearContent(mainContent);

        const authDiv = document.createElement("div");
        authDiv.className = "auth-required";

        const iconDiv = document.createElement("div");
        iconDiv.className = "icon";
        iconDiv.textContent = "🔐";

        const h2 = document.createElement("h2");
        h2.textContent = "Connect Your Zoho Account";

        const p = document.createElement("p");
        p.textContent = "Connect your Zoho Mail account to view your unread messages directly from your browser toolbar.";

        const btn = document.createElement("button");
        btn.id = "connectBtn";
        btn.className = "btn-primary";
        btn.textContent = "Connect Zoho Mail";
        btn.onclick = async () => {
            // Send session_id to backend via OAuth state
            openOAuth();
            window.close();
        };

        authDiv.appendChild(iconDiv);
        authDiv.appendChild(h2);
        authDiv.appendChild(p);
        authDiv.appendChild(btn);
        mainContent.appendChild(authDiv);

    } else if (data.authError) {
        // We have a token but backend says it's invalid
        userBar.style.display = "none";
        clearContent(mainContent);

        const isFirstSetup = (data.lastUnread === undefined);
        const titleText = isFirstSetup ? "Setup Incomplete" : "Authentication Problem";
        const descText = isFirstSetup
            ? "We couldn't finalize your connection. Please check your internet and try again."
            : "There was a problem verifying your account. This could be a temporary issue.";
        const actionText = isFirstSetup ? "Try Setup Again" : "Re-connect";

        const authDiv = document.createElement("div");
        authDiv.className = "auth-required";

        const iconDiv = document.createElement("div");
        iconDiv.className = "icon";
        iconDiv.textContent = isFirstSetup ? "🔌" : "⚠️";

        const h2 = document.createElement("h2");
        h2.textContent = titleText;

        const p = document.createElement("p");
        p.textContent = descText;

        const btnContainer = document.createElement("div");
        btnContainer.style.display = "flex";
        btnContainer.style.gap = "10px";

        const retryBtn = document.createElement("button");
        retryBtn.id = "retryBtn";
        retryBtn.className = "btn-primary";
        retryBtn.style.background = "var(--text-sub)";
        retryBtn.textContent = "Try Again";
        retryBtn.onclick = handleRefresh;

        const reconnectBtn = document.createElement("button");
        reconnectBtn.id = "reconnectBtn";
        reconnectBtn.className = "btn-primary";
        reconnectBtn.textContent = actionText;
        reconnectBtn.onclick = async () => {
            // Send session_id to backend via OAuth state
            openOAuth();
            window.close();
        };

        btnContainer.appendChild(retryBtn);
        btnContainer.appendChild(reconnectBtn);

        authDiv.appendChild(iconDiv);
        authDiv.appendChild(h2);
        authDiv.appendChild(p);
        authDiv.appendChild(btnContainer);
        mainContent.appendChild(authDiv);

    } else {
        // Authenticated - show user bar and mail list
        userBar.style.display = "flex";

        // Apply visibility settings
        const settings = (await api.storage.local.get("settings")).settings || {};

        // Folder support reserved for future release
        // Hide folder dropdown (coming soon)
        const folderSelect = document.getElementById("folderSelect");
        if (folderSelect) {
            folderSelect.style.display = "none";
        }

        // Show only Zoho email (no name)
        if (data.accountEmail) {
            userEmail.textContent = data.accountEmail;
            userEmail.style.cursor = "pointer";
            userEmail.title = "Open Inbox";
            userEmail.onclick = () => {
                api.tabs.create({ url: "https://mail.zoho.com/zm/" });
                window.close();
            };

            avatarInitial.textContent = data.accountEmail.substring(0, 1).toUpperCase();
        }

        // Update unread badge
        const count = data.lastUnread !== undefined ? data.lastUnread : "--";
        unreadBadge.textContent = `${count} Unread`;

        // Load mail list and folders
        loadList();
        updateStatus();
    }
}

// Load mail list
async function loadList(folderId = null) {
    // Deduplication: prevent concurrent requests
    if (listLoading) {
        console.log("[loadList] Already loading, skipping");
        return;
    }

    // Debouncing: prevent requests within 500ms
    const now = Date.now();
    if (!folderId && now - lastListLoad < LIST_DEBOUNCE_MS) {
        console.log("[loadList] Too soon since last load, skipping");
        return;
    }

    listLoading = true;
    if (!folderId) lastListLoad = now;

    try {
        const { jwt, lastItems, accountEmail } = await api.storage.local.get(["jwt", "lastItems", "accountEmail"]);

        // Optimistic render only on first load
        if (lastItems && lastItems.length > 0 && !folderId) {
            renderList(lastItems);
        }

        if (!jwt) return;

        const backendUrl = await getBackendUrl();
        const url = new URL(`${backendUrl}/mail/unread/list`);
        url.searchParams.append("limit", "50");
        // Use cached data from background poll (no forced refresh)
        if (folderId) url.searchParams.append("folder", folderId);

        const res = await fetch(url, {
            headers: { Authorization: `Bearer ${jwt}` }
        });

        if (!res.ok) return;

        const data = await res.json();

        if (!folderId) {
            await api.storage.local.set({
                lastItems: data.items,
                accountEmail: data.account?.email
            });
        }

        renderList(data.items);

        if (data.account?.email) {
            updateUI();
        }
    } catch (err) {
        console.error("Failed to load mail list:", err);
    } finally {
        listLoading = false;
    }
}

// Load folders (reserved for future release)
async function loadFolders() {
    const { jwt } = await api.storage.local.get(["jwt"]);
    if (!jwt) return;

    try {
        const backendUrl = await getBackendUrl();
        const res = await fetch(`${backendUrl}/mail/folders`, {
            headers: { Authorization: `Bearer ${jwt}` }
        });

        if (!res.ok) return;

        const data = await res.json();
        const select = document.getElementById("folderSelect");
        if (!select) return;

        const currentVal = select.value;
        clearContent(select);

        data.folders.forEach(folder => {
            const option = document.createElement("option");
            option.value = folder.id;
            option.textContent = folder.unread > 0 ? `${folder.name} (${folder.unread})` : folder.name;
            if (folder.id === currentVal) option.selected = true;
            select.appendChild(option);
        });
    } catch (err) {
        console.error("Failed to load folders:", err);
    }
}

async function markRead(messageIds) {
    if (messageIds.length === 0) return;

    const { jwt } = await api.storage.local.get("jwt");
    if (!jwt) return;

    const backendUrl = await getBackendUrl();
    const response = await fetch(`${backendUrl}/mail/read`, {
        method: "PUT",
        headers: {
            Authorization: `Bearer ${jwt}`,
            "Content-Type": "application/json"
        },
        body: JSON.stringify({ messageIds })
    });
    if (!response.ok) {
        throw new Error(`Failed to mark mail as read: ${response.status}`);
    }

    const { lastItems = [], lastUnread = 0 } = await api.storage.local.get(["lastItems", "lastUnread"]);
    const markedIds = new Set(messageIds);
    const items = lastItems.filter((item) => !markedIds.has(item.id));
    const unread = Math.max(0, lastUnread - (lastItems.length - items.length));
    await api.storage.local.set({ lastItems: items, lastUnread: unread });
}

async function handleMarkAllRead() {
    const { lastItems = [] } = await api.storage.local.get("lastItems");
    try {
        await markRead(lastItems.map((item) => item.id));
    } catch (error) {
        console.error("Failed to mark all mail as read:", error);
    }
}

async function messageHTML(item) {
    if (item.html) return item.html;

    const { jwt } = await api.storage.local.get("jwt");
    if (!jwt || !item.folderId) {
        throw new Error("Message content was not included in the event and cannot be requested without a folder ID");
    }
    const backendUrl = await getBackendUrl();
    const url = new URL(`${backendUrl}/mail/messages/${encodeURIComponent(item.id)}/content`);
    url.searchParams.set("folderId", item.folderId);
    const response = await fetch(url, { headers: { Authorization: `Bearer ${jwt}` } });
    if (!response.ok) {
        throw new Error(`Failed to load message content: ${response.status}`);
    }
    return (await response.json()).html;
}

async function openMessage(item) {
    const html = await messageHTML(item);
    const list = document.getElementById("mainContent");
    clearContent(list);

    const frame = document.createElement("iframe");
    frame.className = "message-body";
    frame.sandbox = "";
    frame.srcdoc = html;
    frame.title = item.subject || "Email message";
    list.appendChild(frame);

    await markRead([item.id]);
}

// Render mail list (DOM Safe)
async function renderList(items) {
    const settings = (await api.storage.local.get("settings")).settings || {};
    const list = document.getElementById("mainContent");
    clearContent(list);

    if (!items || items.length === 0) {
        const emptyState = document.createElement("div");
        emptyState.className = "empty-state";

        const iconDiv = document.createElement("div");
        iconDiv.style.fontSize = "36px";
        iconDiv.textContent = "📩";

        const msgP = document.createElement("p");
        msgP.style.fontWeight = "700";
        msgP.style.fontSize = "16px";
        msgP.textContent = "All caught up!";

        emptyState.appendChild(iconDiv);
        emptyState.appendChild(msgP);
        list.appendChild(emptyState);
        return;
    }

    items.forEach(item => {
        const div = document.createElement("div");
        div.className = "mail-item";

        const avatarInitial = (item.fromName || item.fromEmail || "U")[0].toUpperCase();
        const timeStr = formatMailDate(item.receivedAt);

        // Avatar
        const avatarDiv = document.createElement("div");
        avatarDiv.className = "mail-avatar";
        avatarDiv.textContent = avatarInitial;

        // Content
        const contentDiv = document.createElement("div");
        contentDiv.className = "mail-content";

        // Top Row
        const topRow = document.createElement("div");
        topRow.className = "mail-row-top";

        const senderSpan = document.createElement("span");
        senderSpan.className = "sender";
        senderSpan.textContent = item.fromName || item.fromEmail || '';

        const timeSpan = document.createElement("span");
        timeSpan.className = "time";
        timeSpan.textContent = timeStr;

        topRow.appendChild(senderSpan);
        topRow.appendChild(timeSpan);

        // Subject
        const subjectDiv = document.createElement("div");
        subjectDiv.className = "subject";
        subjectDiv.textContent = item.subject || '';

        contentDiv.appendChild(topRow);
        contentDiv.appendChild(subjectDiv);

        // Snippet (Optional)
        if (settings.showSnippets !== false) {
            const snippetDiv = document.createElement("div");
            snippetDiv.className = "snippet";
            snippetDiv.textContent = item.snippet || '';
            contentDiv.appendChild(snippetDiv);
        }

        div.appendChild(avatarDiv);
        div.appendChild(contentDiv);

        const markReadBtn = document.createElement("button");
        markReadBtn.className = "mark-read-btn";
        markReadBtn.type = "button";
        markReadBtn.title = "Mark as read";
        markReadBtn.textContent = "✓";
        markReadBtn.onclick = async (event) => {
            event.stopPropagation();
            try {
                await markRead([item.id]);
            } catch (error) {
                console.error("Failed to mark mail as read:", error);
            }
        };
        div.appendChild(markReadBtn);

        div.onclick = async () => {
            try {
                await openMessage(item);
            } catch (error) {
                console.error("Failed to open message:", error);
            }
        };
        list.appendChild(div);
    });
}

// Refresh handler
async function handleRefresh() {
    const refreshBtn = document.getElementById("refreshBtn");
    const overlay = document.getElementById("loadingOverlay");

    refreshBtn.style.animation = "spin 0.8s cubic-bezier(0.4, 0, 0.2, 1) infinite";
    overlay.style.display = "flex";

    api.runtime.sendMessage({ action: "refresh" }, () => {
        setTimeout(async () => {
            overlay.style.display = "none";
            refreshBtn.style.animation = "none";
            await updateUI();
        }, 800);
    });
}

// Event listeners
document.getElementById("themeToggle").addEventListener("click", toggleTheme);
document.getElementById("refreshBtn").addEventListener("click", handleRefresh);
document.getElementById("markAllReadBtn").addEventListener("click", handleMarkAllRead);

document.getElementById("menuBtn").addEventListener("click", (e) => {
    e.stopPropagation();
    document.getElementById("dropdownMenu").classList.toggle("show");
});

window.addEventListener("click", () => {
    document.getElementById("dropdownMenu").classList.remove("show");
});

document.getElementById("settingsBtn").addEventListener("click", () => {
    window.location.href = "settings.html";
});

document.getElementById("signOutBtn").addEventListener("click", () => {
    api.tabs.create({
        url: "https://accounts.zoho.in/home#sessions/userconnectedapps",
        active: true
    });

    const mainContent = document.getElementById("mainContent");
    clearContent(mainContent);

    const disconnectDiv = document.createElement("div");
    disconnectDiv.className = "auth-required";

    const iconDiv = document.createElement("div");
    iconDiv.className = "icon";
    iconDiv.textContent = "⏳";

    const h2 = document.createElement("h2");
    h2.textContent = "Disconnecting...";

    const p = document.createElement("p");
    p.textContent = "Please revoke access on the Zoho page that just opened. The extension will automatically detect the change.";

    disconnectDiv.appendChild(iconDiv);
    disconnectDiv.appendChild(h2);
    disconnectDiv.appendChild(p);

    mainContent.appendChild(disconnectDiv);
});

// Storage change listener
api.storage.onChanged.addListener((changes, area) => {
    if (area === "local" && (changes.lastUnread || changes.authError || changes.jwt)) {
        updateUI();
    }
    if (area === "local" && changes.eventsConnected) updateStatus();
});

// Initialize
document.addEventListener("DOMContentLoaded", () => {
    initTheme();
    updateUI();
    // Don't call loadList() here - it's called by updateUI() only when authenticated

    const folderSelect = document.getElementById("folderSelect");
    if (folderSelect) {
        folderSelect.addEventListener("change", (e) => {
            loadList(e.target.value);
        });
    }

    updateStatus();
});
