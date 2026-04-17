import { useState } from "react";
import type { RealtimeDebugEvent } from "../api/realtimeClient";
import type { RealtimeConnectionState } from "../api/realtimeClient";

interface DebugDrawerProps {
  events: RealtimeDebugEvent[];
  realtimeState: RealtimeConnectionState;
  reconnectAttempts: number;
  lastCloseCode: number | null;
  lastError: string | null;
}

function formatTimestamp(ts: number): string {
  const date = new Date(ts);
  return date.toISOString().slice(11, 23); // hh:mm:ss.mmm
}

export function DebugDrawer({
  events,
  realtimeState,
  reconnectAttempts,
  lastCloseCode,
  lastError
}: DebugDrawerProps): JSX.Element {
  const [open, setOpen] = useState(false);

  const copyAll = () => {
    const body = events
      .map((event) => `${formatTimestamp(event.timestamp)} [${event.level}] ${event.message}`)
      .join("\n");
    const header = `state=${realtimeState} reconnects=${reconnectAttempts} lastClose=${lastCloseCode ?? "-"} lastError=${lastError ?? "-"}\n\n`;
    navigator.clipboard.writeText(header + body).catch(() => {
      // no-op: clipboard write denied
    });
  };

  return (
    <aside className={open ? "mf-debug-drawer mf-debug-drawer-open" : "mf-debug-drawer"} aria-label="Debug console">
      <header className="mf-debug-header">
        <button type="button" onClick={() => setOpen((value) => !value)} aria-expanded={open}>
          {open ? "Hide debug" : "Debug"} ({events.length})
        </button>
        {open && (
          <>
            <span className="mf-debug-meta">state: {realtimeState}</span>
            <span className="mf-debug-meta">reconnects: {reconnectAttempts}</span>
            {lastCloseCode !== null && <span className="mf-debug-meta">close: {lastCloseCode}</span>}
            <button type="button" className="button-secondary" onClick={copyAll}>
              Copy
            </button>
          </>
        )}
      </header>

      {open && (
        <ol className="mf-debug-log">
          {events.length === 0 && <li className="mf-debug-empty">No events yet.</li>}
          {events.map((event, index) => (
            <li key={`${event.timestamp}-${index}`} className={`mf-debug-row mf-debug-level-${event.level}`}>
              <span className="mf-debug-time">{formatTimestamp(event.timestamp)}</span>
              <span className="mf-debug-level">{event.level}</span>
              <span className="mf-debug-message">{event.message}</span>
            </li>
          ))}
        </ol>
      )}
    </aside>
  );
}
