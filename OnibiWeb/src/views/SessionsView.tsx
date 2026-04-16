import type { ControllableSessionSnapshot } from "../types";

interface SessionsViewProps {
  sessions: ControllableSessionSnapshot[];
  realtimeState: string;
  onOpenSession: (sessionId: string) => void;
  onRefresh: () => void;
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

export function SessionsView({
  sessions,
  realtimeState,
  onOpenSession,
  onRefresh
}: SessionsViewProps): JSX.Element {
  return (
    <main className="screen">
      <header className="screen-header with-actions">
        <div>
          <h1>Sessions</h1>
          <p>Realtime: {realtimeState}</p>
        </div>
        <button type="button" className="button-secondary" onClick={onRefresh}>
          Refresh
        </button>
      </header>

      {sessions.length === 0 ? (
        <p className="empty-state">No controllable sessions are currently available.</p>
      ) : (
        <ul className="session-list">
          {sessions.map((session) => (
            <li key={session.id} className="session-list-item">
              <button
                type="button"
                className="session-row"
                onClick={() => onOpenSession(session.id)}
                disabled={!session.isControllable}
              >
                <div className="session-row-main">
                  <strong>{session.displayName}</strong>
                  <span className={statusClassName(session.status)}>{session.status}</span>
                </div>
                <div className="session-meta">
                  <span>{new Date(session.lastActivityAt).toLocaleString()}</span>
                  {session.lastCommandPreview && <span className="command-preview">{session.lastCommandPreview}</span>}
                </div>
              </button>
            </li>
          ))}
        </ul>
      )}
    </main>
  );
}
