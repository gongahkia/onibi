import { useEffect, useMemo, useState } from "react";
import { agentIconUrl } from "../lib/agent-icons";
import {
  AGENT_LABELS,
  sessionAttentionState,
  sessionNeedsAttention,
  useSessionStore,
  type CommandBlock,
  type Session,
  type SessionAttentionState,
  type SessionEvent,
  type Workspace,
} from "../lib/sessions";
import { ptyWrite } from "../lib/tauri-bridge";

type ActivityFilter = "all" | "current" | "attention" | "commands" | "logs" | "events";

const ACTIVITY_FILTERS: Array<{ id: ActivityFilter; label: string }> = [
  { id: "all", label: "All" },
  { id: "current", label: "Current" },
  { id: "attention", label: "Attention" },
  { id: "commands", label: "Commands" },
  { id: "logs", label: "Logs" },
  { id: "events", label: "Events" },
];

function formatTime(timestamp: number): string {
  return new Intl.DateTimeFormat(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).format(timestamp);
}

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

function eventLabel(event: SessionEvent): string {
  if (event.agent) {
    return `${AGENT_LABELS[event.agent]} · ${event.type.replace(/-/g, " ")}`;
  }
  return event.type.replace(/-/g, " ");
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

function statusLabel(session: Session): string {
  if (session.status === "awaiting-approval") {
    return "Needs approval";
  }
  return session.status[0].toUpperCase() + session.status.slice(1);
}

function searchable(parts: Array<string | number | null | undefined>, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return true;
  }
  return parts.join(" ").toLowerCase().includes(needle);
}

function isCurrentSession(session: Session): boolean {
  return session.status !== "completed" && session.status !== "stale";
}

function commandNeedsAttention(block: CommandBlock): boolean {
  return (
    block.status === "failed" ||
    block.status === "aborted" ||
    block.attention === "failed" ||
    block.attention === "needs-approval" ||
    block.attention === "triggered" ||
    block.attention === "exited"
  );
}

function workspaceName(workspace: Workspace | undefined): string {
  return workspace?.name ?? "Workspace";
}

export function SessionHistoryView() {
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<ActivityFilter>("all");
  const [now, setNow] = useState(Date.now());
  const events = useSessionStore((state) => state.sessionEvents);
  const workspaces = useSessionStore((state) => state.workspaces);
  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const commandBlocks = useSessionStore((state) => state.commandBlocks);
  const activeCommandBlocks = useSessionStore((state) => state.activeCommandBlocks);
  const openWebUrl = useSessionStore((state) => state.openWebUrl);
  const selectFile = useSessionStore((state) => state.selectFile);
  const setActiveSession = useSessionStore((state) => state.setActiveSession);
  const clearSessionAttention = useSessionStore((state) => state.clearSessionAttention);

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  const workspaceById = useMemo(
    () => new Map(workspaces.map((workspace) => [workspace.id, workspace])),
    [workspaces],
  );

  const latestSessions = useMemo(
    () =>
      sessions
        .filter((session) => {
          if (filter === "commands" || filter === "logs" || filter === "events") {
            return false;
          }
          if (filter === "current" && !isCurrentSession(session)) {
            return false;
          }
          if (filter === "attention" && !sessionNeedsAttention(session, now)) {
            return false;
          }
          const workspace = workspaceById.get(session.workspaceId);
          return searchable(
            [
              session.title,
              AGENT_LABELS[session.agent],
              session.cwd,
              session.status,
              session.lastExitCode,
              session.lastTrigger?.label,
              session.lastTrigger?.line,
              session.transcript?.text,
              workspace?.name,
              workspace?.path,
            ],
            query,
          );
        })
        .sort((left, right) => {
          const leftAttention = sessionNeedsAttention(left, now);
          const rightAttention = sessionNeedsAttention(right, now);
          if (leftAttention !== rightAttention) {
            return leftAttention ? -1 : 1;
          }
          const leftLive = isCurrentSession(left);
          const rightLive = isCurrentSession(right);
          if (leftLive !== rightLive) {
            return leftLive ? -1 : 1;
          }
          return right.createdAt - left.createdAt;
        }),
    [filter, now, query, sessions, workspaceById],
  );

  const latestBlocks = useMemo(
    () =>
      [...Object.values(activeCommandBlocks), ...commandBlocks]
        .filter((block, index, all) => all.findIndex((item) => item.id === block.id) === index)
        .filter((block) => {
          if (filter === "current" || filter === "logs" || filter === "events") {
            return false;
          }
          if (filter === "attention" && !commandNeedsAttention(block)) {
            return false;
          }
          const workspace = workspaceById.get(block.workspaceId);
          return searchable(
            [
              block.command,
              block.cwd,
              block.outputPreview,
              block.changedFiles.join(" "),
              block.status,
              AGENT_LABELS[block.agent],
              workspace?.name,
              workspace?.path,
            ],
            query,
          );
        })
        .sort((a, b) => b.startedAt - a.startedAt),
    [activeCommandBlocks, commandBlocks, filter, query, workspaceById],
  );

  const latestTranscripts = useMemo(
    () =>
      sessions
        .filter((session) => {
          if (filter === "current" || filter === "attention" || filter === "commands" || filter === "events") {
            return false;
          }
          if (!session.transcript?.text.trim()) {
            return false;
          }
          const workspace = workspaceById.get(session.workspaceId);
          return searchable(
            [
              session.title,
              session.cwd,
              session.transcript.text,
              AGENT_LABELS[session.agent],
              workspace?.name,
              workspace?.path,
            ],
            query,
          );
        })
        .sort(
          (a, b) =>
            (b.transcript?.updatedAt ?? b.createdAt) -
            (a.transcript?.updatedAt ?? a.createdAt),
        ),
    [filter, query, sessions, workspaceById],
  );

  const latestEvents = useMemo(
    () =>
      [...events]
        .filter((event) => {
          if (filter !== "all" && filter !== "events") {
            return false;
          }
          const workspace = event.workspaceId
            ? workspaceById.get(event.workspaceId)
            : undefined;
          return searchable(
            [eventLabel(event), event.summary, workspace?.name, workspace?.path],
            query,
          );
        })
        .sort((a, b) => b.timestamp - a.timestamp),
    [events, filter, query, workspaceById],
  );

  const visibleText = useMemo(
    () =>
      [
        ...latestSessions.map((session) => {
          const workspace = workspaceById.get(session.workspaceId);
          return [
            `${AGENT_LABELS[session.agent]} session`,
            `status: ${statusLabel(session)}`,
            `workspace: ${workspaceName(workspace)}`,
            session.cwd ? `cwd: ${session.cwd}` : "",
            session.lastTrigger ? `trigger: ${session.lastTrigger.label}` : "",
          ]
            .filter(Boolean)
            .join("\n");
        }),
        ...latestBlocks.map((block) =>
          [
            `${AGENT_LABELS[block.agent]} command`,
            block.command,
            block.outputPreview,
            `workspace: ${workspaceName(workspaceById.get(block.workspaceId))}`,
          ]
            .filter(Boolean)
            .join("\n"),
        ),
        ...latestTranscripts.map((session) =>
          [
            `${AGENT_LABELS[session.agent]} chat log`,
            `workspace: ${workspaceName(workspaceById.get(session.workspaceId))}`,
            session.transcript?.text ?? "",
          ]
            .filter(Boolean)
            .join("\n"),
        ),
        ...latestEvents.map((event) =>
          [
            eventLabel(event),
            event.summary,
            `workspace: ${
              workspaceName(
                event.workspaceId ? workspaceById.get(event.workspaceId) : undefined,
              )
            }`,
          ]
            .filter(Boolean)
            .join("\n"),
        ),
      ].join("\n\n"),
    [latestBlocks, latestEvents, latestSessions, latestTranscripts, workspaceById],
  );

  function copyVisibleActivity() {
    void navigator.clipboard?.writeText(visibleText);
  }

  function exportVisibleActivity() {
    const payload = {
      exportedAt: new Date().toISOString(),
      filter,
      query,
      sessions: latestSessions,
      commandBlocks: latestBlocks,
      transcripts: latestTranscripts.map((session) => ({
        sessionId: session.id,
        agent: session.agent,
        workspaceId: session.workspaceId,
        title: session.title,
        transcript: session.transcript,
      })),
      events: latestEvents,
    };
    if (typeof URL.createObjectURL !== "function") {
      void navigator.clipboard?.writeText(JSON.stringify(payload, null, 2));
      return;
    }
    const blob = new Blob([JSON.stringify(payload, null, 2)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `onibi-activity-${Date.now()}.json`;
    anchor.click();
    URL.revokeObjectURL(url);
  }

  const empty =
    latestSessions.length === 0 &&
    latestBlocks.length === 0 &&
    latestTranscripts.length === 0 &&
    latestEvents.length === 0;

  return (
    <section className="session-history-view" aria-label="Session history">
      <div className="activity-toolbar">
        <input
          className="settings-input"
          aria-label="Search command timeline"
          placeholder="Search sessions, commands, output, files"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <div className="activity-actions">
          <button
            type="button"
            className="text-button"
            disabled={!visibleText}
            onClick={copyVisibleActivity}
          >
            Copy
          </button>
          <button
            type="button"
            className="text-button"
            disabled={empty}
            onClick={exportVisibleActivity}
          >
            Export JSON
          </button>
        </div>
      </div>
      <div className="activity-filter-row" role="tablist" aria-label="Activity filter">
        {ACTIVITY_FILTERS.map((item) => (
          <button
            key={item.id}
            type="button"
            className={filter === item.id ? "active" : ""}
            aria-selected={filter === item.id}
            role="tab"
            onClick={() => setFilter(item.id)}
          >
            {item.label}
          </button>
        ))}
      </div>
      {empty ? <div className="source-control-empty">No activity matches this filter.</div> : null}
      {latestSessions.map((session) => {
        const workspace = workspaceById.get(session.workspaceId);
        return (
          <SessionActivityRow
            key={`session:${session.id}`}
            session={session}
            workspace={workspace}
            active={session.id === activeSessionId}
            now={now}
            onSelect={() => setActiveSession(session.id)}
            onClearAttention={() => clearSessionAttention(session.id)}
          />
        );
      })}
      {latestBlocks.map((block) => {
        const workspace = workspaceById.get(block.workspaceId);
        const session = sessions.find((item) => item.id === block.sessionId);
        return (
          <CommandBlockRow
            key={block.id}
            block={block}
            workspaceName={workspaceName(workspace)}
            live={Boolean(session && session.status !== "stale")}
            onRerun={() => {
              void ptyWrite(
                block.sessionId,
                new TextEncoder().encode(`${block.command}\n`),
              );
            }}
            onOpenPreview={() => {
              if (block.previewUrl) {
                openWebUrl(block.previewUrl, block.sessionId);
              }
            }}
            onOpenFile={(path) => {
              if (!workspace) {
                return;
              }
              const fullPath = path.startsWith("/")
                ? path
                : `${workspace.path.replace(/\/+$/, "")}/${path}`;
              selectFile({
                type: "file",
                workspaceId: workspace.id,
                workspaceRoot: workspace.path,
                path: fullPath,
                name: fullPath.split("/").pop() || fullPath,
                size: 0,
              });
            }}
          />
        );
      })}
      {latestTranscripts.map((session) => {
        const workspace = workspaceById.get(session.workspaceId);
        return (
          <article className="history-event transcript-event" key={`log:${session.id}`}>
            <div className="history-event-time">
              {formatTime(session.transcript?.updatedAt ?? session.createdAt)}
            </div>
            <div className="history-event-body">
              <div className="history-event-label">
                {AGENT_LABELS[session.agent]} · Chat Log
              </div>
              <div className="history-event-summary">{session.title}</div>
              <pre className="history-output">
                {(session.transcript?.text ?? "").trim().slice(-6000)}
              </pre>
              {workspace ? (
                <div className="history-event-meta">{workspace.name}</div>
              ) : null}
              <div className="history-actions">
                <button
                  type="button"
                  className="text-button"
                  onClick={() =>
                    void navigator.clipboard?.writeText(session.transcript?.text ?? "")
                  }
                >
                  Copy Log
                </button>
              </div>
            </div>
          </article>
        );
      })}
      {latestEvents.map((event) => {
        const workspace = event.workspaceId
          ? workspaceById.get(event.workspaceId)
          : undefined;
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

function SessionActivityRow({
  session,
  workspace,
  active,
  now,
  onSelect,
  onClearAttention,
}: {
  session: Session;
  workspace?: Workspace;
  active: boolean;
  now: number;
  onSelect: () => void;
  onClearAttention: () => void;
}) {
  const attention = sessionAttentionState(session, now);
  const needsAttention = sessionNeedsAttention(session, now);
  return (
    <article
      className={`history-event activity-session-event ${active ? "active" : ""} ${
        needsAttention ? "needs-attention" : ""
      }`}
    >
      <button
        type="button"
        className="history-event-icon"
        aria-label={`Focus ${AGENT_LABELS[session.agent]} session`}
        onClick={onSelect}
      >
        <img src={agentIconUrl(session.agent)} alt="" />
      </button>
      <div className="history-event-body">
        <div className="history-event-label">
          {AGENT_LABELS[session.agent]} · {attentionLabel(attention)}
        </div>
        <div className="history-event-summary">{session.title}</div>
        <div className="history-event-meta">
          {session.cwd ?? workspace?.path ?? workspace?.name ?? "Workspace"}
        </div>
        <div className="history-session-status-row">
          <span className={`session-card-status attention-${attention}`}>
            {attentionLabel(attention)}
          </span>
          <span className={`session-card-status ${session.status}`}>
            {statusLabel(session)}
          </span>
          <span>{formatElapsed(session.createdAt, now)}</span>
          {session.pendingApprovals.length > 0 ? (
            <span>{session.pendingApprovals.length} approvals</span>
          ) : null}
          {needsAttention && session.lastTrigger?.actions.includes("badge") ? (
            <span title={session.lastTrigger.line}>{session.lastTrigger.label}</span>
          ) : null}
        </div>
        <div className="history-actions">
          <button type="button" className="text-button" onClick={onSelect}>
            Focus
          </button>
          <button
            type="button"
            className="text-button"
            onClick={() => void navigator.clipboard?.writeText(session.id)}
          >
            Copy ID
          </button>
          {session.transcript?.text ? (
            <button
              type="button"
              className="text-button"
              onClick={() => void navigator.clipboard?.writeText(session.transcript?.text ?? "")}
            >
              Copy Log
            </button>
          ) : null}
          {needsAttention ? (
            <button type="button" className="text-button" onClick={onClearAttention}>
              Clear
            </button>
          ) : null}
        </div>
      </div>
    </article>
  );
}

function durationLabel(block: CommandBlock): string {
  if (!block.endedAt) {
    return "running";
  }
  const seconds = Math.max(0, (block.endedAt - block.startedAt) / 1000);
  return seconds < 10 ? `${seconds.toFixed(1)}s` : `${Math.round(seconds)}s`;
}

function CommandBlockRow({
  block,
  workspaceName,
  live,
  onRerun,
  onOpenPreview,
  onOpenFile,
}: {
  block: CommandBlock;
  workspaceName: string;
  live: boolean;
  onRerun: () => void;
  onOpenPreview: () => void;
  onOpenFile: (path: string) => void;
}) {
  return (
    <article className={`history-event command-block command-${block.status}`}>
      <div className="history-event-time">{formatTime(block.startedAt)}</div>
      <div className="history-event-body">
        <div className="history-event-label">
          {AGENT_LABELS[block.agent]} · {block.status} · {durationLabel(block)}
        </div>
        <code className="history-command">{block.command || "shell command"}</code>
        {block.outputPreview ? (
          <pre className="history-output">{block.outputPreview}</pre>
        ) : null}
        <div className="history-event-meta">{workspaceName}</div>
        {block.changedFiles.length > 0 ? (
          <div className="history-files">
            {block.changedFiles.slice(0, 6).map((path) => (
              <button
                type="button"
                className="text-button"
                key={path}
                onClick={() => onOpenFile(path)}
              >
                {path}
              </button>
            ))}
          </div>
        ) : null}
        <div className="history-actions">
          <button
            type="button"
            className="text-button"
            onClick={() => void navigator.clipboard?.writeText(block.command)}
          >
            Copy Cmd
          </button>
          {block.outputPreview ? (
            <button
              type="button"
              className="text-button"
              onClick={() => void navigator.clipboard?.writeText(block.outputPreview)}
            >
              Copy Out
            </button>
          ) : null}
          <button type="button" className="text-button" disabled={!live} onClick={onRerun}>
            Rerun
          </button>
          {block.previewUrl ? (
            <button type="button" className="text-button" onClick={onOpenPreview}>
              Preview
            </button>
          ) : null}
        </div>
      </div>
    </article>
  );
}
