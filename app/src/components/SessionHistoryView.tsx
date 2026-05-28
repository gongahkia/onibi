import { AGENT_LABELS, useSessionStore, type SessionEvent } from "../lib/sessions";

function formatTime(timestamp: number): string {
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(timestamp);
}

function eventLabel(event: SessionEvent): string {
  if (event.agent) {
    return `${AGENT_LABELS[event.agent]} · ${event.type.replace(/-/g, " ")}`;
  }
  return event.type.replace(/-/g, " ");
}

export function SessionHistoryView() {
  const events = useSessionStore((state) => state.sessionEvents);
  const workspaces = useSessionStore((state) => state.workspaces);
  const latest = [...events].reverse();

  if (latest.length === 0) {
    return (
      <section className="session-history-view">
        <div className="source-control-empty">No session history yet.</div>
      </section>
    );
  }

  return (
    <section className="session-history-view" aria-label="Session history">
      {latest.map((event) => {
        const workspace = workspaces.find((item) => item.id === event.workspaceId);
        return (
          <article className="history-event" key={event.id}>
            <div className="history-event-time">{formatTime(event.timestamp)}</div>
            <div className="history-event-body">
              <div className="history-event-label">{eventLabel(event)}</div>
              <div className="history-event-summary">{event.summary}</div>
              {workspace ? (
                <div className="history-event-meta">{workspace.name}</div>
              ) : null}
            </div>
          </article>
        );
      })}
    </section>
  );
}
