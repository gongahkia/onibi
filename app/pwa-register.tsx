"use client";

import { useEffect, useState } from "react";

export default function PwaRegister() {
  const [waitingWorker, setWaitingWorker] = useState<ServiceWorker | null>(null);

  useEffect(() => {
    if (!("serviceWorker" in navigator)) return;
    let registration: ServiceWorkerRegistration | undefined;
    const onControllerChange = () => window.location.reload();
    const checkForUpdate = () => { if (registration?.waiting) setWaitingWorker(registration.waiting); };
    void navigator.serviceWorker.register("/sw.js", { updateViaCache: "none" }).then((next) => {
      registration = next;
      checkForUpdate();
      next.addEventListener("updatefound", () => next.installing?.addEventListener("statechange", checkForUpdate));
    }).catch(() => undefined);
    navigator.serviceWorker.addEventListener("controllerchange", onControllerChange);
    return () => navigator.serviceWorker.removeEventListener("controllerchange", onControllerChange);
  }, []);

  if (!waitingWorker) return null;
  return <button type="button" className="pwa-update" onClick={() => waitingWorker.postMessage({ type: "SKIP_WAITING" })}>Update ready</button>;
}
