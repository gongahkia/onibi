import { useMemo } from "react";
import { useSessionStore } from "../lib/sessions";

export function StatusBar() {
  const sessions = useSessionStore((state) => state.sessions);
  const setView = useSessionStore((state) => state.setActiveSidebarView);
  const pendingApprovals = useMemo(
    () => sessions.reduce((sum, session) => sum + session.pendingApprovals.length, 0),
    [sessions],
  );

  if (pendingApprovals === 0) {
    return (
      <footer
        className="status-bar idle"
        data-variant="idle"
        role="contentinfo"
        aria-label="Status"
      />
    );
  }

  return (
    <footer
      className="status-bar"
      data-variant="attention"
      role="contentinfo"
      aria-label="Status"
    >
      <button
        type="button"
        className="status-bar-item attention"
        onClick={() => setView("approvals")}
        title="Pending approvals"
      >
        <i className="codicon codicon-bell-dot" aria-hidden="true" />
        <span>{pendingApprovals} pending</span>
      </button>
    </footer>
  );
}
