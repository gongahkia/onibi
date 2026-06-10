/// <reference lib="webworker" />

import { cleanupOutdatedCaches, precacheAndRoute } from "workbox-precaching";

declare const self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<{ url: string; revision: string | null }>;
};

cleanupOutdatedCaches();
precacheAndRoute(self.__WB_MANIFEST);

self.addEventListener("push", (event) => {
  const data = readPushData(event);
  event.waitUntil(
    self.registration.showNotification(data.title, {
      body: data.body,
      tag: data.tag,
      data: data.url,
      icon: "/favicon.svg",
      badge: "/favicon.svg",
    }),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const url = String(event.notification.data ?? "/m/");
  event.waitUntil(
    self.clients.matchAll({ type: "window", includeUncontrolled: true }).then((clients) => {
      const existing = clients.find((client) => "focus" in client) as WindowClient | undefined;
      if (existing) {
        return existing.navigate(url).then(() => existing.focus());
      }
      return self.clients.openWindow(url);
    }),
  );
});

function readPushData(event: PushEvent) {
  const fallback = {
    title: "Onibi approval",
    body: "A tool call is waiting.",
    tag: "onibi-approval",
    url: "/m/",
  };
  try {
    const data = event.data?.json() as
      | {
          approval_id?: string;
          agent?: string;
          tool?: string;
          cwd?: string;
        }
      | undefined;
    if (!data) {
      return fallback;
    }
    return {
      title: `${data.agent ?? "Agent"} needs approval`,
      body: `${data.tool ?? "Tool call"}${data.cwd ? ` in ${data.cwd}` : ""}`,
      tag: data.approval_id ?? fallback.tag,
      url: data.approval_id
        ? `/m/?approval=${encodeURIComponent(data.approval_id)}`
        : "/m/",
    };
  } catch {
    return fallback;
  }
}
