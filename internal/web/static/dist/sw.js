"use strict";
const cacheName = "onibi-assets-v1";
const sw = self;
self.addEventListener("install", (event) => {
    event.waitUntil(sw.skipWaiting());
});
self.addEventListener("activate", (event) => {
    event.waitUntil(caches.keys().then((keys) => Promise.all(keys.filter((key) => key !== cacheName).map((key) => caches.delete(key)))).then(() => sw.clients.claim()));
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
