"use strict";

// Push only: no fetch handler, so nothing is cached and every page load
// still comes from the Mac.
self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    data = {};
  }
  event.waitUntil(
    self.registration.showNotification(data.title || "Onibi", {
      body: data.body || "",
      tag: data.tag || "onibi",
      renotify: true,
      icon: "icon-192.png",
      data: { url: data.url || "./" },
    }),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = new URL(
    event.notification.data?.url || "./",
    self.registration.scope,
  );
  if (url.origin !== self.location.origin) return;
  event.waitUntil(
    (async () => {
      const windows = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      const open = windows.find(
        (client) => new URL(client.url).origin === url.origin,
      );
      if (open) {
        await open.focus();
        open.postMessage({ type: "open", url: url.href });
      } else await self.clients.openWindow(url.href);
    })(),
  );
});
