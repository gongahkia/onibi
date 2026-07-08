type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;

type VAPIDPublicKeyResponse = {
  key: string;
};

const pushEnabledKey = "onibi-push-enabled";

export async function subscribePushFromGesture(postJSON: PostJSON): Promise<void> {
  assertStandalone();
  assertPushAvailable();
  const permission = await Notification.requestPermission();
  if (permission !== "granted") {
    markPushDisabled();
    throw new Error("Push permission denied.");
  }
  const registration = await navigator.serviceWorker.ready;
  const subscription = await currentSubscription(registration);
  await storeSubscription(postJSON, subscription);
  markPushEnabled();
}

export async function refreshPushSubscription(postJSON: PostJSON): Promise<void> {
  if (!shouldRefreshPush()) {
    return;
  }
  if (!isStandalone() || !pushAvailable()) {
    return;
  }
  if (Notification.permission === "denied") {
    markPushDisabled();
    return;
  }
  if (Notification.permission !== "granted") {
    return;
  }
  const registration = await navigator.serviceWorker.ready;
  const subscription = await currentSubscription(registration);
  await storeSubscription(postJSON, subscription);
}

async function storeSubscription(
  postJSON: PostJSON,
  subscription: PushSubscription
): Promise<void> {
  const response = await postJSON(
    "/push/subscribe",
    subscription.toJSON() as Record<string, unknown>
  );
  if (!response.ok) {
    throw new Error(`push subscribe ${response.status}`);
  }
}

function assertStandalone(): void {
  if (!isStandalone()) {
    throw new Error("Push requires Home Screen mode.");
  }
}

function assertPushAvailable(): void {
  if (!pushAvailable()) {
    throw new Error("Push unavailable.");
  }
}

function isStandalone(): boolean {
  return window.matchMedia("(display-mode: standalone)").matches;
}

function pushAvailable(): boolean {
  return "Notification" in window && "serviceWorker" in navigator && "PushManager" in window;
}

async function currentSubscription(
  registration: ServiceWorkerRegistration
): Promise<PushSubscription> {
  const vapid = await getVAPIDPublicKey();
  const applicationServerKey = base64URLToBytes(vapid.key);
  const existing = await registration.pushManager.getSubscription();
  if (existing !== null) {
    if (subscriptionUsesKey(existing, applicationServerKey)) {
      return existing;
    }
    await existing.unsubscribe();
  }
  return registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey
  });
}

async function getVAPIDPublicKey(): Promise<VAPIDPublicKeyResponse> {
  const response = await fetch("/push/vapid-public-key", { credentials: "same-origin" });
  if (!response.ok) {
    throw new Error(`push key ${response.status}`);
  }
  return (await response.json()) as VAPIDPublicKeyResponse;
}

function base64URLToBytes(value: string): Uint8Array {
  const padded = value + "=".repeat((4 - (value.length % 4)) % 4);
  const raw = window.atob(padded.replaceAll("-", "+").replaceAll("_", "/"));
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) {
    out[i] = raw.charCodeAt(i);
  }
  return out;
}

function subscriptionUsesKey(subscription: PushSubscription, expected: Uint8Array): boolean {
  const actual = subscription.options.applicationServerKey;
  if (actual === null) {
    return false;
  }
  const actualBytes = new Uint8Array(actual);
  if (actualBytes.byteLength !== expected.byteLength) {
    return false;
  }
  for (let i = 0; i < expected.byteLength; i += 1) {
    if (actualBytes[i] !== expected[i]) {
      return false;
    }
  }
  return true;
}

function shouldRefreshPush(): boolean {
  return window.localStorage.getItem(pushEnabledKey) === "1";
}

function markPushEnabled(): void {
  window.localStorage.setItem(pushEnabledKey, "1");
}

function markPushDisabled(): void {
  window.localStorage.removeItem(pushEnabledKey);
}
