import { useEffect, useState } from "react";
import {
  dispatchOnibiNotification,
  notificationEvents,
  type OnibiNotificationToast,
} from "../lib/notifications";
import { useSessionStore } from "../lib/sessions";

interface PtyNotificationDetail {
  ptyId?: string;
  source?: string;
  title?: string;
  body?: string | null;
  urgency?: string | null;
}

export function NotificationToastHost() {
  const [toasts, setToasts] = useState<OnibiNotificationToast[]>([]);

  useEffect(() => {
    function handleToast(event: Event) {
      const toast = (event as CustomEvent<OnibiNotificationToast>).detail;
      setToasts((current) => [toast, ...current].slice(0, 4));
      window.setTimeout(() => {
        setToasts((current) => current.filter((item) => item.id !== toast.id));
      }, 5200);
    }

    window.addEventListener(notificationEvents.toast, handleToast);
    return () => window.removeEventListener(notificationEvents.toast, handleToast);
  }, []);

  useEffect(() => {
    function handlePtyNotification(event: Event) {
      const detail = (event as CustomEvent<PtyNotificationDetail>).detail;
      const session = detail.ptyId
        ? useSessionStore
            .getState()
            .sessions.find((candidate) => candidate.id === detail.ptyId)
        : null;
      void dispatchOnibiNotification({
        title: detail.title?.trim() || "Terminal notification",
        body: detail.body ?? null,
        kind: "request",
        source: "pty",
        sessionId: detail.ptyId ?? null,
        agent: session?.agent ?? null,
      });
    }

    window.addEventListener("onibi:pty-notification", handlePtyNotification);
    return () =>
      window.removeEventListener("onibi:pty-notification", handlePtyNotification);
  }, []);

  if (toasts.length === 0) {
    return null;
  }

  return (
    <div className="notification-toast-stack" aria-live="polite">
      {toasts.map((toast) => (
        <section className={`notification-toast kind-${toast.kind}`} key={toast.id}>
          <button
            type="button"
            className="icon-button notification-toast-close"
            aria-label="Dismiss notification"
            onClick={() =>
              setToasts((current) => current.filter((item) => item.id !== toast.id))
            }
          >
            <i className="codicon codicon-close" aria-hidden="true" />
          </button>
          <strong>{toast.title}</strong>
          {toast.body ? <span>{toast.body}</span> : null}
        </section>
      ))}
    </div>
  );
}
