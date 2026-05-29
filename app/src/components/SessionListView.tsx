import { useEffect, useMemo, useState } from "react";
import { agentIconUrl } from "../lib/agent-icons";
import {
  AGENT_LABELS,
  sessionAttentionState,
  sessionNeedsAttention,
  useSessionStore,
  type Session,
  type SessionAttentionState,
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

function attentionLabel(state: SessionAttentionState): string {
  switch (state) {
    case "needs-approval":
      return "Needs approval";
    case "triggered":
      return "Triggered";
    case "failed":
      return "Failed";
    case "exited":
      return "Exited";
    case "stale":
      return "Stale";
    case "running":
      return "Running";
    default:
      return "Idle";
  }
}

export function SessionListView() {
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const setActiveSession = useSessionStore((state) => state.setActiveSession);
  const clearSessionAttention = useSessionStore((state) => state.clearSessionAttention);
  const [filter, setFilter] = useState<"all" | "attention">("all");
  const [now, setNow] = useState(Date.now());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const workspaceById = useMemo(
    () => new Map(workspaces.map((workspace) => [workspace.id, workspace])),
    [workspaces],
  );
  const attentionCount = useMemo(
    () => sessions.filter((session) => sessionNeedsAttention(session, now)).length,
    [now, sessions],
  );
  const orderedSessions = useMemo(
    () =>
      sessions
        .filter((session) =>
          filter === "attention" ? sessionNeedsAttention(session, now) : true,
        )
        .sort((left, right) => {
        const leftRunning = left.status === "running" || left.status === "awaiting-approval";
        const rightRunning =
          right.status === "running" || right.status === "awaiting-approval";
        if (leftRunning !== rightRunning) {
          return leftRunning ? -1 : 1;
        }
        return right.createdAt - left.createdAt;
      }),
    [filter, now, sessions],
  );

  if (orderedSessions.length === 0) {
    return (
      <section className="sessions-view" aria-label="Sessions">
        <SessionFilters
          filter={filter}
          attentionCount={attentionCount}
          onFilter={setFilter}
        />
        <div className="session-empty">
          {filter === "attention" ? "No sessions need attention." : "No running sessions."}
        </div>
      </section>
    );
  }

  return (
    <section className="sessions-view" aria-label="Sessions">
      <SessionFilters
        filter={filter}
        attentionCount={attentionCount}
        onFilter={setFilter}
      />
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
            onClearAttention={() => clearSessionAttention(session.id)}
          />
        );
      })}
    </section>
  );
}

function SessionFilters({
  filter,
  attentionCount,
  onFilter,
}: {
  filter: "all" | "attention";
  attentionCount: number;
  onFilter: (filter: "all" | "attention") => void;
}) {
  return (
    <div className="session-filter-row">
      <button
        type="button"
        className={filter === "all" ? "active" : ""}
        onClick={() => onFilter("all")}
      >
        All
      </button>
      <button
        type="button"
        className={filter === "attention" ? "active" : ""}
        onClick={() => onFilter("attention")}
      >
        Needs Attention
        {attentionCount > 0 ? <span>{attentionCount}</span> : null}
      </button>
    </div>
  );
}

function SessionCard({
  session,
  workspace,
  active,
  elapsed,
  onSelect,
  onClearAttention,
}: {
  session: Session;
  workspace?: Workspace;
  active: boolean;
  elapsed: string;
  onSelect: () => void;
  onClearAttention: () => void;
}) {
  const attention = sessionAttentionState(session);
  const needsAttention = sessionNeedsAttention(session);
  return (
    <div
      role="button"
      tabIndex={0}
      className={`session-card ${active ? "active" : ""} ${
        needsAttention ? "needs-attention" : ""
      }`}
      onClick={onSelect}
      onKeyDown={(event) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          onSelect();
        }
      }}
    >
      <img src={agentIconUrl(session.agent)} alt="" />
      <span className="session-card-main">
        <span className="session-card-title">{AGENT_LABELS[session.agent]}</span>
        <span className="session-card-meta">{session.cwd ?? workspace?.name ?? "Workspace"}</span>
        {session.preview ? (
          <span className="session-card-meta">{session.preview.url}</span>
        ) : null}
      </span>
      <span className="session-card-side">
        <span className={`session-card-status attention-${attention}`}>
          {attentionLabel(attention)}
        </span>
        <span className={`session-card-status ${session.status}`}>
          {statusLabel(session)}
        </span>
        <span className="session-card-time">{elapsed}</span>
        {session.pendingApprovals.length > 0 ? (
          <span className="session-card-approval">
            {session.pendingApprovals.length}
          </span>
        ) : null}
        {needsAttention && session.lastTrigger?.actions.includes("badge") ? (
          <span className="session-card-trigger" title={session.lastTrigger.line}>
            {session.lastTrigger.label}
          </span>
        ) : null}
        {needsAttention ? (
          <button
            type="button"
            className="session-card-clear"
            onClick={(event) => {
              event.stopPropagation();
              onClearAttention();
            }}
          >
            Clear
          </button>
        ) : null}
      </span>
    </div>
  );
}
