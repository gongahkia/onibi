import { urlBase64ToUint8Array } from "./utils";

export async function createPushSubscription(
  vapidPublicKey: string | undefined,
): Promise<unknown | null> {
  if (
    !vapidPublicKey ||
    typeof window === "undefined" ||
    !("serviceWorker" in navigator) ||
    !("PushManager" in window) ||
    !("Notification" in window)
  ) {
    return null;
  }
  if (Notification.permission === "default") {
    const permission = await Notification.requestPermission();
    if (permission !== "granted") {
      return null;
    }
  }
  if (Notification.permission !== "granted") {
    return null;
  }
  const registration = await navigator.serviceWorker.ready;
  const existing = await registration.pushManager.getSubscription();
  const subscription =
    existing ??
    (await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(vapidPublicKey),
    }));
  return subscription.toJSON();
}
