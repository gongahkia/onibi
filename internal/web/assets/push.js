"use strict";

// Notifications are opt-in per browser. The server holds the subscription
// against this browser's grant, so revoking the browser also stops them.
const pushState = { publicKey: "", subscribed: false, busy: false };

function pushSupported() {
  return (
    window.isSecureContext &&
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  );
}
function needsHomeScreen() {
  return (
    /iPhone|iPad|iPod/.test(navigator.userAgent) &&
    !navigator.standalone &&
    !matchMedia("(display-mode: standalone)").matches
  );
}
function applicationServerKey(base64url) {
  const raw = atob(base64url.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(raw, (ch) => ch.charCodeAt(0));
}
async function pushRegistration() {
  await navigator.serviceWorker.register("sw.js");
  return navigator.serviceWorker.ready;
}

function renderPush(note) {
  const toggle = byId("notify-toggle");
  const test = byId("notify-test");
  let status;
  toggle.hidden = test.hidden = true;
  if (!pushSupported()) {
    status = needsHomeScreen()
      ? "On iPhone and iPad, add Onibi to your Home Screen (Share, then Add to Home Screen) and open it from there to turn on notifications."
      : "This browser can’t receive notifications from Onibi.";
  } else if (Notification.permission === "denied") {
    status =
      "Notifications are blocked for this site. Allow them in your browser’s site settings, then come back here.";
  } else {
    toggle.hidden = false;
    test.hidden = !pushState.subscribed;
    toggle.textContent = pushState.subscribed
      ? "Turn off notifications"
      : "Turn on notifications";
    status = pushState.subscribed
      ? "On. This browser is notified when a persistent session needs input or rings the bell. Notifications name the session, never its output."
      : "Off. Turn on to be notified when a persistent session needs input or rings the bell.";
  }
  byId("notify-status").textContent = note || status;
  toggle.disabled = test.disabled = pushState.busy;
}

async function loadPush() {
  if (!pushSupported()) return renderPush();
  try {
    // Only look: nothing is installed until notifications are turned on.
    const [status, registration] = await Promise.all([
      api("/api/push"),
      navigator.serviceWorker.getRegistration(),
    ]);
    const subscription = await registration?.pushManager.getSubscription();
    pushState.publicKey = status.publicKey;
    pushState.subscribed = Boolean(status.subscribed && subscription);
    renderPush();
  } catch (error) {
    renderPush("Notification settings couldn’t be loaded. " + error.message);
  }
}

async function turnOnPush() {
  // Ask first: Safari only shows the prompt during the tap itself.
  const permission = await Notification.requestPermission();
  if (permission !== "granted") return renderPush();
  const registration = await pushRegistration();
  const subscription =
    (await registration.pushManager.getSubscription()) ||
    (await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: applicationServerKey(pushState.publicKey),
    }));
  await api("/api/push/subscribe", {
    method: "POST",
    body: JSON.stringify(subscription.toJSON()),
  });
  pushState.subscribed = true;
}

async function turnOffPush() {
  await api("/api/push/unsubscribe", { method: "POST", body: "{}" });
  const registration = await navigator.serviceWorker.getRegistration();
  const subscription = await registration?.pushManager.getSubscription();
  await subscription?.unsubscribe();
  pushState.subscribed = false;
}

byId("notify-toggle").addEventListener("click", async () => {
  pushState.busy = true;
  renderPush();
  try {
    if (pushState.subscribed) await turnOffPush();
    else await turnOnPush();
    renderPush();
  } catch (error) {
    renderPush("Notifications couldn’t be changed. " + error.message);
  } finally {
    pushState.busy = false;
    byId("notify-toggle").disabled = byId("notify-test").disabled = false;
  }
});

byId("notify-test").addEventListener("click", async () => {
  byId("notify-test").disabled = true;
  try {
    await api("/api/push/test", { method: "POST", body: "{}" });
    renderPush("Test sent. It should arrive within a few seconds.");
  } catch (error) {
    renderPush(error.message);
  } finally {
    byId("notify-test").disabled = false;
  }
});

byId("open-devices").addEventListener("click", loadPush);

if ("serviceWorker" in navigator)
  navigator.serviceWorker.addEventListener("message", (event) => {
    if (event.data?.type === "open") openFromNotification(event.data.url);
  });
