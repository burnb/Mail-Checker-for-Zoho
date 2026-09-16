const api = typeof browser !== "undefined" ? browser : chrome;

const NOTIFICATION_ID = "new-mail-notify";
let eventSocket = null;

// Poll mutex
let pollInProgress = false;

// Badge state tracking (never read from browser)
let badgeState = {
    text: "",
    color: ""
};

// Initial setup - Generate session_id once and force first fetch
api.runtime.onInstalled.addListener(async () => {
    console.log("Zoho Extension: Initialized. Waiting for auth.");

    // Generate session_id once per browser install (multi-session support)
    const { session_id } = await api.storage.local.get('session_id');
    if (!session_id) {
        const sessionId = crypto.randomUUID();
        await api.storage.local.set({ session_id: sessionId });
        console.log('Session ID generated:', sessionId);
    } else {
        console.log('Existing session ID:', session_id);
    }

    checkMail(true);
});

// Listen for JWT storage to activate live mail events after OAuth.
api.storage.onChanged.addListener((changes, area) => {
    if (area === "local" && changes.jwt && changes.jwt.newValue) {
        checkMail(true);
        connectEvents();
    }
});

// Alarm persistence and startup fetch for Edge
api.runtime.onStartup.addListener(async () => {
    checkMail(true);
    connectEvents();
})

// Handle icon click (Manual Refresh)
if (api.action) {
    api.action.onClicked.addListener(() => {
        checkMail(true);
    });
}

async function checkMail(force = false, retryCount = 0) {
    if (pollInProgress) {
        console.log("Poll already in progress, skipping.");
        return;
    }

    pollInProgress = true;

    try {
        const data = await api.storage.local.get(["jwt", "lastUnread", "lastNotificationTime"]);
        if (!data.jwt) {
            updateBadge(""); // New user: show nothing on icon
            return;
        }

        const backendUrl = await getBackendUrl();
        const url = `${backendUrl}/mail/unread${force ? "?refresh=true" : ""}`;

        // Show loading if online (doesn't overwrite badgeState)
        if (navigator.onLine) {
            updateBadge("⏳", "#6B7280");
        }

        const res = await fetch(url, {
            headers: { "Authorization": `Bearer ${data.jwt}` }
        });

        if (res.status === 401 || res.status === 403) {
            const errData = await res.json().catch(() => ({ error: "unknown" }));

            // Retry once before validating the failure (handles backend cold starts/hiccups)
            if (retryCount < 1) {
                console.log("Auth error detected, retrying once...");
                pollInProgress = false; // Unlock to allow retry recursion
                await new Promise(r => setTimeout(r, 2000));
                return checkMail(force, retryCount + 1);
            }

            // After retry, determine if this is genuine first setup or actual auth failure
            // "New User" means lastUnread is undefined AND jwt is newly set (not a revocation)
            const isFirstSetup = data.lastUnread === undefined;

            if (!isFirstSetup) {
                // User had working auth before - this is revocation or token expiry
                console.log("Auth failed: User token revoked or expired");
                updateBadge("!", "#EF4444");
                api.storage.local.set({ authError: true });
            } else {
                // First setup attempt - could be transient, but still show warning
                console.log("Auth failed during initial setup");
                updateBadge("!", "#FFA500"); // Orange for setup issues
                // Don't set authError=true to allow auto-recovery
            }
            return;
        }

        if (!res.ok) {
            // Non-auth error (500, 503, etc.) - restore previous badge, don't show "?"
            console.error("Backend error:", res.status);
            updateBadge(badgeState.text, badgeState.color);
            return;
        }

        const responseData = await res.json();
        const unread = responseData.unread;

        if (unread === undefined) {
            throw new Error("Invalid response format: missing unread count");
        }


        const settings = (await api.storage.local.get("settings")).settings || {};

        // Badge logic
        if (settings.showBadge !== false) {
            handleBadgeUpdate(unread);
        } else {
            updateBadge("");
        }

        // Capture previous value before updating storage
        const previousUnread = data.lastUnread;

        // Save state FIRST (before notification logic)
        await api.storage.local.set({
            lastUnread: unread,
            authError: false
        });

        // Notification logic AFTER state is stable
        if (settings.enableNotifications !== false) {
            handleNotification(unread, previousUnread, data.lastNotificationTime);
        }

    } catch (err) {
        console.error("Poll failed:", err);
        // Restore previous badge on error (don't show "?")
        updateBadge(badgeState.text, badgeState.color);
    } finally {
        pollInProgress = false;
    }
}

// Wrapper to handle badge safely across browsers (Chrome MV3 vs Firefox MV2)
function updateBadge(text, color) {
    const action = api.action || api.browserAction;
    if (!action) return;

    // Firefox MV2 requires strings, Chrome MV3 allows integers but strings are safer
    const textStr = text ? text.toString() : "";

    action.setBadgeText({ text: textStr });

    if (color && action.setBadgeBackgroundColor) {
        // Chrome requires RGB array format [R, G, B, A] or hex string
        // Convert common colors to RGB arrays for better compatibility
        let colorValue = color;
        if (color === "#EF4444") colorValue = [239, 68, 68, 255]; // Red
        else if (color === "#FFA500") colorValue = [255, 165, 0, 255]; // Orange
        else if (color === "#1178D2") colorValue = [17, 120, 210, 255]; // Zoho Blue
        else if (color === "#6B7280") colorValue = [107, 114, 128, 255]; // Gray (loading)

        action.setBadgeBackgroundColor({ color: colorValue });
    }

    // Set badge text color to white for better visibility (Chrome 110+)
    if (action.setBadgeTextColor) {
        action.setBadgeTextColor({ color: [255, 255, 255, 255] }); // White
    }

    // Save to badgeState (except for loading indicator)
    if (text !== "⏳") {
        badgeState = { text: textStr, color: color || "" };
    }
}

function handleBadgeUpdate(current) {
    let text = current.toString();
    if (current > 99) text = "99+";
    if (current === 0) text = "";

    updateBadge(text, "#1178D2"); // Zoho Blue
}

function handleNotification(current, previous, lastNotifyTime) {
    // Do not notify on first-ever fetch
    if (previous === undefined) return;

    // Notify only if unread count increased
    if (current <= previous) return;

    // Rate limit notifications (60s for testing, can increase to 5 mins later)
    const now = Date.now();
    if (lastNotifyTime && (now - lastNotifyTime < 60 * 1000)) return;

    api.notifications.create(NOTIFICATION_ID, {
        type: "basic",
        iconUrl: "icons/icon-128.png",
        title: "New Zoho Mail",
        message: `You have ${current} unread messages.`,
        priority: 2
    });

    api.storage.local.set({ lastNotificationTime: now });
}

// Listen for messages from popup (e.g., "manual refresh" or "set token")
api.runtime.onMessage.addListener((msg, sender, sendResponse) => {
    if (msg.action === "refresh") {
        checkMail(true).then(() => {
            sendResponse({ status: "refreshed" });
        });
        return true; // Keep channel open for async response
    }

    if (msg.action === "auth_success") {
        console.log("Auth success received, enabling mail events...");
        checkMail(true);
        connectEvents();
        sendResponse({ status: "polling_restarted" });
    }

    if (msg.type === "ZOHO_AUTH_TOKEN" && msg.token) {
        // Optional: Validate sender origin (extra security layer)
        const backendUrl = getBackendUrl();
        if (sender.url && !sender.url.startsWith(backendUrl)) {
            console.warn("Token rejected: invalid sender origin", sender.url);
            sendResponse({ success: false, error: "invalid_origin" });
            return true;
        }

        api.storage.local.set({
            jwt: msg.token,
            authError: false
        }, () => {
            checkMail(true);
        });
        sendResponse({ success: true });
    }

    return true;
});

async function getBackendUrl() {
    const { backendUrl } = await api.storage.local.get("backendUrl");
    return backendUrl;
}

async function connectEvents() {
    const { jwt } = await api.storage.local.get("jwt");
    if (!jwt || eventSocket?.readyState === WebSocket.OPEN || eventSocket?.readyState === WebSocket.CONNECTING) return;

    const backendUrl = await getBackendUrl();
    const eventsUrl = new URL(`${backendUrl}/events`);
    eventsUrl.protocol = eventsUrl.protocol === "https:" ? "wss:" : "ws:";
    eventsUrl.searchParams.set("access_token", jwt);
    eventSocket = new WebSocket(eventsUrl);
    eventSocket.onmessage = (event) => {
        try {
            if (JSON.parse(event.data).type === "mail.received") checkMail(true);
        } catch (error) {
            console.error("Invalid server event", error);
        }
    };
    eventSocket.onclose = () => {
        eventSocket = null;
        setTimeout(connectEvents, 5000);
    };
    eventSocket.onerror = () => eventSocket.close();
}
