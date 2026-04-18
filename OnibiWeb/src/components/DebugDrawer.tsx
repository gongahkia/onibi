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
  const [filtersOpen, setFiltersOpen] = useState(false);

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
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
        </svg>
        <span>{events.length}</span>
        {errorCount > 0 && <span className="mf-debug-pill-badge">{errorCount}</span>}
      </button>
    );
  }

  return (
    <aside className="mf-debug-drawer mf-debug-drawer-open" aria-label="Debug console">
      <header className="mf-debug-header">
        <button type="button" className="mf-debug-close" onClick={() => setOpen(false)} aria-expanded={open} aria-label="Hide debug drawer">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M18 6L6 18M6 6l12 12" />
          </svg>
        </button>
        <span className={`mf-debug-state mf-debug-state-${realtimeState}`}>
          <span className="mf-debug-state-dot" aria-hidden="true" />
          {realtimeState}
        </span>
        <span className="mf-debug-meta">reconnects {reconnectAttempts}</span>
        {lastCloseCode !== null && <span className="mf-debug-meta">close {lastCloseCode}</span>}
        <span className="mf-debug-meta mf-debug-meta-count">{visible.length}/{events.length}</span>
      </header>

      <div className="mf-debug-actions">
        <button
          type="button"
          className={filtersOpen || filter !== "all" ? "mf-debug-filter-toggle mf-debug-filter-toggle-active" : "mf-debug-filter-toggle"}
          onClick={() => setFiltersOpen((open) => !open)}
          aria-expanded={filtersOpen}
          aria-label="Filter by level"
        >
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
            <path d="M3 5h18l-7 9v6l-4-2v-4L3 5z" />
          </svg>
        </button>
        {filtersOpen && (
          <div className="mf-debug-filter-group" role="group" aria-label="Level filter">
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
          </div>
        )}
        <span className="mf-debug-actions-spacer" />
        <button type="button" className="mf-debug-action" onClick={copyAll} aria-label="Copy log">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <rect x="9" y="9" width="11" height="11" rx="2" />
            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
          </svg>
        </button>
        <button type="button" className="mf-debug-action" onClick={exportJSON} aria-label="Export log as JSON">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
            <path d="M7 10l5 5 5-5" />
            <path d="M12 15V3" />
          </svg>
        </button>
        {onClear && (
          <button type="button" className="mf-debug-action" onClick={onClear} aria-label="Clear log">
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M3 6h18" />
              <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
              <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
            </svg>
          </button>
        )}
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
