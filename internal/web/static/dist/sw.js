"use strict";
const cacheName = "onibi-assets-v1";
const sw = self;
self.addEventListener("install", (event) => {
    event.waitUntil(sw.skipWaiting());
});
self.addEventListener("activate", (event) => {
    event.waitUntil(caches
        .keys()
        .then((keys) => Promise.all(keys.filter((key) => key !== cacheName).map((key) => caches.delete(key))))
        .then(() => sw.clients.claim()));
});
self.addEventListener("fetch", (event) => {
    const fetchEvent = event;
    const request = fetchEvent.request;
    if (request.method !== "GET") {
        return;
    }
    const url = new URL(request.url);
    if (url.origin !== location.origin || !url.pathname.startsWith("/assets/")) {
        return;
    }
    fetchEvent.respondWith(staleWhileRevalidate(request));
});
self.addEventListener("push", (event) => {
    const pushEvent = event;
    pushEvent.waitUntil(showPushNotification(pushEvent));
});
self.addEventListener("notificationclick", (event) => {
    const clickEvent = event;
    clickEvent.notification.close();
    clickEvent.waitUntil(openNotificationTarget(clickEvent.notification.data));
});
async function staleWhileRevalidate(request) {
    const cache = await caches.open(cacheName);
    const cached = await cache.match(request);
    const fresh = fetch(request).then((response) => {
        if (response.ok) {
            void cache.put(request, response.clone());
        }
        return response;
    });
    return cached ?? fresh;
}
async function showPushNotification(event) {
    const payload = parsePushPayload(event.data);
    const sessionID = stringValue(payload.session_id);
    const approvalID = stringValue(payload.approval_id) ?? stringValue(payload.approval?.id);
    const title = stringValue(payload.title) ?? "Onibi approval";
    const body = stringValue(payload.body) ?? notificationBody(payload);
    const url = notificationURL(sessionID, approvalID);
    await sw.registration.showNotification(title, {
        body,
        tag: approvalID === undefined ? "onibi-approval" : `onibi-approval-${approvalID}`,
        icon: "/icons/onibi-192.png",
        badge: "/icons/onibi-192.png",
        data: { url }
    });
}
function parsePushPayload(data) {
    if (data === null) {
        return {};
    }
    const text = data.text();
    try {
        const value = JSON.parse(text);
        return isRecord(value) ? value : { body: text };
    }
    catch {
        return { body: text };
    }
}
function notificationBody(payload) {
    const tool = stringValue(payload.tool);
    const agent = stringValue(payload.agent);
    if (tool !== undefined && agent !== undefined) {
        return `${agent} requests ${tool}`;
    }
    if (tool !== undefined) {
        return `approval requested: ${tool}`;
    }
    return "approval requested";
}
function notificationURL(sessionID, approvalID) {
    const url = new URL(sessionID === undefined ? "/" : `/s/${encodeURIComponent(sessionID)}`, sw.location.origin);
    if (approvalID !== undefined) {
        url.searchParams.set("approval", approvalID);
    }
    return url.href;
}
async function openNotificationTarget(data) {
    const url = isRecord(data) && typeof data.url === "string"
        ? data.url
        : new URL("/", sw.location.origin).href;
    const clients = await sw.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const client of clients) {
        if (isWindowClient(client)) {
            const navigated = await client.navigate(url);
            await (navigated ?? client).focus();
            return;
        }
    }
    await sw.clients.openWindow(url);
}
function stringValue(value) {
    return typeof value === "string" && value.trim() !== "" ? value : undefined;
}
function isRecord(value) {
    return typeof value === "object" && value !== null;
}
function isWindowClient(client) {
    return "focus" in client && "navigate" in client;
}
