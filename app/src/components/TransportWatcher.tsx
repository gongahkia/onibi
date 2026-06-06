import { useEffect, useRef } from "react";
import { dispatchOnibiNotification } from "../lib/notifications";
import { fetchTransportStatus } from "../lib/transports";

export function TransportWatcher() {
  const prevRunningRef = useRef<Set<string> | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function pull() {
      try {
        const snap = await fetchTransportStatus();
        if (cancelled) {
          return;
        }
        const now = new Set(
          snap.filter((t) => t.status.state === "running").map((t) => t.name),
        );
        const prev = prevRunningRef.current;
        if (prev) {
          for (const t of snap) {
            const wasRunning = prev.has(t.name);
            const isRunning = now.has(t.name);
            if (!wasRunning && isRunning) {
              void dispatchOnibiNotification({
                source: "session",
                kind: "info",
                title: `${t.label || t.name} is reachable`,
                body: "Your phone can now pair with this machine.",
              });
            } else if (wasRunning && !isRunning) {
              const message = t.status.state === "failed"
                ? t.status.message
                : "Transport stopped.";
              void dispatchOnibiNotification({
                source: "session",
                kind: "info",
                title: `${t.label || t.name} went offline`,
                body: message,
              });
            }
          }
        }
        prevRunningRef.current = now;
      } catch {
        if (!cancelled) {
          prevRunningRef.current = new Set();
        }
      }
    }
    void pull();
    const id = window.setInterval(pull, 20_000);
    const onFocus = () => void pull();
    window.addEventListener("focus", onFocus);
    return () => {
      cancelled = true;
      window.clearInterval(id);
      window.removeEventListener("focus", onFocus);
    };
  }, []);

  return null;
}
