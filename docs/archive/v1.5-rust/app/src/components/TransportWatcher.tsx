import { useEffect, useRef } from "react";
import { dispatchOnibiNotification } from "../lib/notifications";
import { useTransportStatusQuery } from "../lib/queries";

export function TransportWatcher() {
  const prevRunningRef = useRef<Set<string> | null>(null);
  const { data: snap, isError } = useTransportStatusQuery({
    refetchInterval: 20_000,
  });

  useEffect(() => {
    if (isError) {
      prevRunningRef.current = new Set();
      return;
    }
    if (!snap) {
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
  }, [isError, snap]);

  return null;
}
