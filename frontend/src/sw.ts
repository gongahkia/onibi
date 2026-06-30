const cacheName = "onibi-assets-v1";
const sw = self as unknown as ServiceWorkerGlobalScope;

self.addEventListener("install", (event) => {
  (event as ExtendableEvent).waitUntil(sw.skipWaiting());
});

self.addEventListener("activate", (event) => {
  (event as ExtendableEvent).waitUntil(
    caches.keys().then((keys) => Promise.all(keys.filter((key) => key !== cacheName).map((key) => caches.delete(key)))).then(() => sw.clients.claim())
  );
});

self.addEventListener("fetch", (event) => {
  const fetchEvent = event as FetchEvent;
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
  const pushEvent = event as PushEvent;
  pushEvent.waitUntil(showPushNotification(pushEvent));
});

self.addEventListener("notificationclick", (event) => {
  const clickEvent = event as NotificationEvent;
  clickEvent.notification.close();
  clickEvent.waitUntil(openNotificationTarget(clickEvent.notification.data));
});

async function staleWhileRevalidate(request: Request): Promise<Response> {
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

type PushPayload = {
  title?: unknown;
  body?: unknown;
  session_id?: unknown;
  approval_id?: unknown;
  approval?: { id?: unknown };
  tool?: unknown;
  agent?: unknown;
};

async function showPushNotification(event: PushEvent): Promise<void> {
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

function parsePushPayload(data: PushMessageData | null): PushPayload {
  if (data === null) {
    return {};
  }
  const text = data.text();
  try {
    const value = JSON.parse(text) as unknown;
    return isRecord(value) ? (value as PushPayload) : { body: text };
  } catch {
    return { body: text };
  }
}

function notificationBody(payload: PushPayload): string {
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

function notificationURL(sessionID: string | undefined, approvalID: string | undefined): string {
  const url = new URL(sessionID === undefined ? "/" : `/s/${encodeURIComponent(sessionID)}`, sw.location.origin);
  if (approvalID !== undefined) {
    url.searchParams.set("approval", approvalID);
  }
  return url.href;
}

async function openNotificationTarget(data: unknown): Promise<void> {
  const url = isRecord(data) && typeof data.url === "string" ? data.url : new URL("/", sw.location.origin).href;
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

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isWindowClient(client: Client): client is WindowClient {
  return "focus" in client && "navigate" in client;
}
