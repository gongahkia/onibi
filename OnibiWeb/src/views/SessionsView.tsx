import type { ControllableSessionSnapshot } from "../types";

interface SessionsViewProps {
  sessions: ControllableSessionSnapshot[];
  realtimeState: string;
  onOpenSession: (sessionId: string) => void;
  onRefresh: () => void;
  onDisconnect: () => void;
}

function statusClassName(status: ControllableSessionSnapshot["status"]): string {
  switch (status) {
    case "running":
      return "status-pill status-running";
    case "starting":
      return "status-pill status-starting";
    case "exited":
      return "status-pill status-exited";
    case "failed":
      return "status-pill status-failed";
    default:
      return "status-pill";
  }
}

function realtimeBadgeClass(realtimeState: string): string {
  if (realtimeState === "authenticated") {
    return "mf-badge mf-badge-ok";
  }
  if (realtimeState === "reconnecting" || realtimeState === "connecting" || realtimeState === "authenticating") {
    return "mf-badge mf-badge-warn";
  }
  return "mf-badge";
}

function formatActivity(isoTimestamp: string): string {
  const timestamp = Date.parse(isoTimestamp);
  if (!Number.isFinite(timestamp)) {
    return "unknown";
  }

  const deltaMs = Date.now() - timestamp;
  if (deltaMs < 60_000) {
    return "just now";
  }
  if (deltaMs < 3_600_000) {
    const minutes = Math.floor(deltaMs / 60_000);
    return `${minutes}m ago`;
  }
  if (deltaMs < 86_400_000) {
    const hours = Math.floor(deltaMs / 3_600_000);
    return `${hours}h ago`;
  }
  const days = Math.floor(deltaMs / 86_400_000);
  return `${days}d ago`;
}

export function SessionsView({
  sessions,
  realtimeState,
  onOpenSession,
  onRefresh,
  onDisconnect
}: SessionsViewProps): JSX.Element {
  return (
    <main className="mf-page">
      <div className="mf-shell">
        <header className="mf-card mf-header-card">
          <div className="mf-header-top">
            <div>
              <h1>Terminal Sessions</h1>
              <p>Choose a session to open live control.</p>
            </div>
            <span className={realtimeBadgeClass(realtimeState)}>Realtime: {realtimeState}</span>
          </div>

          {realtimeState !== "authenticated" && sessions.length > 0 && (
            <p className="mf-alert mf-alert-info">Using cached sessions while realtime reconnects.</p>
          )}

          <div className="mf-header-actions">
            <button type="button" className="button-secondary" onClick={onRefresh}>
              Refresh
            </button>
            <button type="button" className="button-secondary" onClick={onDisconnect}>
              Disconnect
            </button>
          </div>
        </header>

        {sessions.length === 0 ? (
          <section className="mf-card mf-empty-state">
            <h2>No sessions available</h2>
            <p>No controllable terminal sessions are currently running on the host.</p>
          </section>
        ) : (
          <ul className="mf-session-list">
            {sessions.map((session) => (
              <li key={session.id}>
                <button
                  type="button"
                  className="mf-session-card"
                  onClick={() => onOpenSession(session.id)}
                  disabled={!session.isControllable}
                >
                  <div className="mf-session-card-top">
                    <strong>{session.displayName}</strong>
                    <span className={statusClassName(session.status)}>{session.status}</span>
                  </div>

                  {session.lastCommandPreview && (
                    <p className="mf-command-preview">{session.lastCommandPreview}</p>
                  )}

                  <div className="mf-session-card-bottom">
                    <span>Last activity {formatActivity(session.lastActivityAt)}</span>
                    {!session.isControllable && <span className="mf-readonly-pill">Read-only</span>}
                  </div>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </main>
  );
}
