type PostJSON = (path: string, body: Record<string, unknown>) => Promise<Response>;

type VAPIDPublicKeyResponse = {
  key: string;
};

export async function subscribePushFromGesture(postJSON: PostJSON): Promise<void> {
  assertStandalone();
  assertPushAvailable();
  const permission = await Notification.requestPermission();
  if (permission !== "granted") {
    throw new Error("Push permission denied.");
  }
  const registration = await navigator.serviceWorker.ready;
  const subscription = (await registration.pushManager.getSubscription()) ?? (await subscribe(registration));
  const response = await postJSON("/push/subscribe", subscription.toJSON() as Record<string, unknown>);
  if (!response.ok) {
    throw new Error(`push subscribe ${response.status}`);
  }
}

function assertStandalone(): void {
  if (!window.matchMedia("(display-mode: standalone)").matches) {
    throw new Error("Push requires Home Screen mode.");
  }
}

function assertPushAvailable(): void {
  if (!("Notification" in window) || !("serviceWorker" in navigator) || !("PushManager" in window)) {
    throw new Error("Push unavailable.");
  }
}

async function subscribe(registration: ServiceWorkerRegistration): Promise<PushSubscription> {
  const vapid = await getVAPIDPublicKey();
  return registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: base64URLToBytes(vapid.key)
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
