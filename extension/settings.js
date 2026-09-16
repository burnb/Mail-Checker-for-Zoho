const api = typeof browser !== "undefined" ? browser : chrome;

const defaultSettings = {
    enableFolders: false,
    showSnippets: true,
    enableNotifications: true,
    showBadge: true
};

// Map settings to their inputs
const inputs = {
    enableFolders: "enableFolders",
    showSnippets: "showSnippets",
    enableNotifications: "enableNotifications",
    showBadge: "showBadge"
};

// Initialize theme
function initTheme() {
    const savedTheme = localStorage.getItem("theme") || "default";
    document.body.setAttribute("data-theme", savedTheme);
}

// Load settings
document.addEventListener("DOMContentLoaded", async () => {
    initTheme();

    // 1. Get current settings
    const result = await api.storage.local.get(["settings", "accountEmail", "backendUrl"]);
    const settings = result.settings || defaultSettings;
    const email = result.accountEmail || "Not Connected";

    // 2. Set account email
    const emailEl = document.getElementById("userEmail");
    if (emailEl) {
        emailEl.textContent = email;
    }

    // 3. Populate inputs
    for (const [key, id] of Object.entries(inputs)) {
        const el = document.getElementById(id);
        if (!el) {
            console.error(`Element not found: ${id}`);
            continue;
        }

        if (el.type === "checkbox") {
            el.checked = (settings[key] !== undefined) ? settings[key] : defaultSettings[key];
        } else {
            el.value = settings[key] || defaultSettings[key];
        }

        // Add listeners
        el.addEventListener("change", saveSettings);
    }

    const backendUrl = document.getElementById("backendUrl");
    backendUrl.value = result.backendUrl;
    backendUrl.addEventListener("change", saveBackendUrl);

    // 4. Back button listener
    document.getElementById("backBtn").addEventListener("click", () => {
        window.location.href = "popup.html";
    });
});

// Save settings automatically
async function saveSettings() {
    const newSettings = {};

    for (const [key, id] of Object.entries(inputs)) {
        const el = document.getElementById(id);
        if (!el) continue;

        if (el.type === "checkbox") {
            newSettings[key] = el.checked;
        } else {
            newSettings[key] = parseInt(el.value, 10);
        }
    }

    // Update storage
    await api.storage.local.set({ settings: newSettings });

    // Refresh badge immediately if toggled
    if (newSettings.showBadge === false) {
        if (api.action || api.browserAction) {
            const action = api.action || api.browserAction;
            action.setBadgeText({ text: "" });
        }
    } else {
        // Trigger a refresh to show badge again
        api.runtime.sendMessage({ action: "refresh" });
    }
}

async function saveBackendUrl() {
    const input = document.getElementById("backendUrl");
    let url;
    try {
        url = new URL(input.value);
        if (!/^https?:$/.test(url.protocol)) throw new Error("unsupported protocol");
    } catch {
        return;
    }

    const normalized = url.origin;
    if (api.permissions && api.permissions.request) {
        const granted = await api.permissions.request({ origins: [`${normalized}/*`] });
        if (!granted) {
            return;
        }
    }
    await api.storage.local.remove(["jwt", "lastUnread", "lastItems", "accountEmail", "authError"]);
    await api.storage.local.set({ backendUrl: normalized, authError: false });
}
