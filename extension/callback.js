const api = typeof browser !== "undefined" ? browser : chrome;

(async function () {
  try {
    const { backendUrl } = await api.storage.local.get("backendUrl");
    const expectedOrigin = new URL(backendUrl).origin;
    if (!document.referrer || new URL(document.referrer).origin !== expectedOrigin) {
      console.error("Invalid referrer:", document.referrer);
      window.close();
      return;
    }

    // Parse token from URL
    const urlParams = new URLSearchParams(window.location.search);
    const token = urlParams.get("token");

    // Validate token structure
    if (!token || typeof token !== "string" || token.length < 20) {
      console.error("Invalid or missing token");
      window.close();
      return;
    }

    // Save token to storage
    // Background script will detect this change and trigger initial poll
    await api.storage.local.set({
      jwt: token,
      authError: false
    });

    console.log("Token saved successfully");

    // Close this tab
    window.close();
  } catch (err) {
    console.error("Failed to save token:", err);
    window.close();
  }
})();
