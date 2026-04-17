import { useMemo, useState } from "react";
import type { RealtimeDebugEvent, RealtimeConnectionState } from "../api/realtimeClient";

interface DebugDrawerProps {
  events: RealtimeDebugEvent[];
  realtimeState: RealtimeConnectionState;
  reconnectAttempts: number;
  lastCloseCode: number | null;
  lastError: string | null;
  onClear?: () => void;
}

type LevelFilter = "all" | "debug" | "info" | "warn" | "error";

function formatTimestamp(ts: number): string {
  const date = new Date(ts);
  return date.toISOString().slice(11, 23);
}

/**
 * Extracted so the filter behavior is independently testable.
 */
export function filterEvents(events: RealtimeDebugEvent[], filter: LevelFilter): RealtimeDebugEvent[] {
  if (filter === "all") return events;
  return events.filter((event) => event.level === filter);
}

const FILTER_OPTIONS: LevelFilter[] = ["all", "error", "warn", "info", "debug"];

export function DebugDrawer({
  events,
  realtimeState,
  reconnectAttempts,
  lastCloseCode,
  lastError,
  onClear
}: DebugDrawerProps): JSX.Element {
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState<LevelFilter>("all");

  const visible = useMemo(() => filterEvents(events, filter), [events, filter]);

  const errorCount = useMemo(
    () => events.filter((event) => event.level === "error").length,
    [events]
  );

  const copyAll = () => {
    const header = `state=${realtimeState} reconnects=${reconnectAttempts} lastClose=${lastCloseCode ?? "-"} lastError=${lastError ?? "-"}\n\n`;
    const body = visible
      .map((event) => `${formatTimestamp(event.timestamp)} [${event.level}] ${event.message}`)
      .join("\n");
    navigator.clipboard.writeText(header + body).catch(() => undefined);
  };

  const exportJSON = () => {
    const payload = {
      exportedAt: new Date().toISOString(),
      realtimeState,
      reconnectAttempts,
      lastCloseCode,
      lastError,
      events: visible
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = `onibi-web-debug-${Date.now()}.json`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  if (!open) {
    return (
      <button
        type="button"
        className={errorCount > 0 ? "mf-debug-pill mf-debug-pill-error" : "mf-debug-pill"}
        onClick={() => setOpen(true)}
        aria-label="Open debug drawer"
      >
        <span aria-hidden="true">⛶</span>
        <span>{events.length}</span>
        {errorCount > 0 && <span className="mf-debug-pill-badge">{errorCount}</span>}
      </button>
    );
  }

  return (
    <aside className="mf-debug-drawer mf-debug-drawer-open" aria-label="Debug console">
      <header className="mf-debug-header">
        <button type="button" onClick={() => setOpen(false)} aria-expanded={open}>
          Hide
        </button>
        <span className="mf-debug-meta">state: {realtimeState}</span>
        <span className="mf-debug-meta">reconnects: {reconnectAttempts}</span>
        {lastCloseCode !== null && <span className="mf-debug-meta">close: {lastCloseCode}</span>}
        <span className="mf-debug-meta">{visible.length}/{events.length}</span>
      </header>

      <div className="mf-debug-actions">
        {FILTER_OPTIONS.map((option) => (
          <button
            key={option}
            type="button"
            className={option === filter ? "mf-debug-filter mf-debug-filter-active" : "mf-debug-filter"}
            onClick={() => setFilter(option)}
          >
            {option}
          </button>
        ))}
        <span className="mf-debug-actions-spacer" />
        <button type="button" className="button-secondary" onClick={copyAll}>Copy</button>
        <button type="button" className="button-secondary" onClick={exportJSON}>Export JSON</button>
        {onClear && <button type="button" className="button-secondary" onClick={onClear}>Clear</button>}
      </div>

      <ol className="mf-debug-log">
        {visible.length === 0 && <li className="mf-debug-empty">No matching events.</li>}
        {visible.map((event, index) => (
          <li key={`${event.timestamp}-${index}`} className={`mf-debug-row mf-debug-level-${event.level}`}>
            <span className="mf-debug-time">{formatTimestamp(event.timestamp)}</span>
            <span className="mf-debug-level">{event.level}</span>
            <span className="mf-debug-message">{event.message}</span>
          </li>
        ))}
      </ol>
    </aside>
  );
}
