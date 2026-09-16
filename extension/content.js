const api = typeof browser !== "undefined" ? browser : chrome;

// Listen for messages from the web page (OAuth callback bridge)
window.addEventListener("message", (event) => {
    if (event.source !== window) return;

    // Validate token structure
    if (
        event.data &&
        event.data.type === "ZOHO_AUTH_TOKEN" &&
        typeof event.data.token === "string" &&
        event.data.token.length > 20
    ) {
        console.log("Content script received valid token, forwarding to background...");
        api.runtime.sendMessage(event.data, (response) => {
            console.log("Background response:", response);
        });
    }
});
