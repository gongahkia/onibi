import { useEffect, useMemo, useState } from "react";
import { agentIconUrl } from "../lib/agent-icons";
import {
  AGENT_LABELS,
  useSessionStore,
  type Session,
  type Workspace,
} from "../lib/sessions";

function formatElapsed(startedAt: number, now: number): string {
  const seconds = Math.max(0, Math.floor((now - startedAt) / 1000));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${rest}s`;
  }
  return `${rest}s`;
}

function statusLabel(session: Session): string {
  if (session.status === "awaiting-approval") {
    return "Needs approval";
  }
  return session.status[0].toUpperCase() + session.status.slice(1);
}

export function SessionListView() {
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const setActiveSession = useSessionStore((state) => state.setActiveSession);
  const [now, setNow] = useState(Date.now());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const workspaceById = useMemo(
    () => new Map(workspaces.map((workspace) => [workspace.id, workspace])),
    [workspaces],
  );
  const orderedSessions = useMemo(
    () =>
      [...sessions].sort((left, right) => {
        const leftRunning = left.status === "running" || left.status === "awaiting-approval";
        const rightRunning =
          right.status === "running" || right.status === "awaiting-approval";
        if (leftRunning !== rightRunning) {
          return leftRunning ? -1 : 1;
        }
        return right.createdAt - left.createdAt;
      }),
    [sessions],
  );

  if (orderedSessions.length === 0) {
    return (
      <section className="sessions-view" aria-label="Sessions">
        <div className="session-empty">No running sessions.</div>
      </section>
    );
  }

  return (
    <section className="sessions-view" aria-label="Sessions">
      {orderedSessions.map((session) => {
        const workspace = workspaceById.get(session.workspaceId);
        return (
          <SessionCard
            key={session.id}
            session={session}
            workspace={workspace}
            active={session.id === activeSessionId}
            elapsed={formatElapsed(session.createdAt, now)}
            onSelect={() => setActiveSession(session.id)}
          />
        );
      })}
    </section>
  );
}

function SessionCard({
  session,
  workspace,
  active,
  elapsed,
  onSelect,
}: {
  session: Session;
  workspace?: Workspace;
  active: boolean;
  elapsed: string;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      className={`session-card ${active ? "active" : ""}`}
      onClick={onSelect}
    >
      <img src={agentIconUrl(session.agent)} alt="" />
      <span className="session-card-main">
        <span className="session-card-title">{AGENT_LABELS[session.agent]}</span>
        <span className="session-card-meta">{session.cwd ?? workspace?.name ?? "Workspace"}</span>
      </span>
      <span className="session-card-side">
        <span className={`session-card-status ${session.status}`}>
          {statusLabel(session)}
        </span>
        <span className="session-card-time">{elapsed}</span>
        {session.pendingApprovals.length > 0 ? (
          <span className="session-card-approval">
            {session.pendingApprovals.length}
          </span>
        ) : null}
        {session.lastTrigger?.actions.includes("badge") ? (
          <span className="session-card-trigger" title={session.lastTrigger.line}>
            {session.lastTrigger.label}
          </span>
        ) : null}
      </span>
    </button>
  );
}
