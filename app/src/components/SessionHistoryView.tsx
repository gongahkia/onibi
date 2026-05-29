import { useMemo, useState } from "react";
import {
  AGENT_LABELS,
  useSessionStore,
  type CommandBlock,
  type SessionEvent,
} from "../lib/sessions";
import { ptyWrite } from "../lib/tauri-bridge";

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
  const [query, setQuery] = useState("");
  const events = useSessionStore((state) => state.sessionEvents);
  const workspaces = useSessionStore((state) => state.workspaces);
  const sessions = useSessionStore((state) => state.sessions);
  const commandBlocks = useSessionStore((state) => state.commandBlocks);
  const activeCommandBlocks = useSessionStore((state) => state.activeCommandBlocks);
  const openWebUrl = useSessionStore((state) => state.openWebUrl);
  const selectFile = useSessionStore((state) => state.selectFile);
  const latestEvents = [...events].reverse();
  const latestTranscripts = useMemo(
    () =>
      sessions
        .filter((session) => session.transcript?.text.trim())
        .filter((session) => {
          const needle = query.trim().toLowerCase();
          if (!needle) {
            return true;
          }
          return [
            session.title,
            session.cwd,
            session.transcript?.text,
            AGENT_LABELS[session.agent],
          ]
            .join(" ")
            .toLowerCase()
            .includes(needle);
        })
        .sort(
          (a, b) =>
            (b.transcript?.updatedAt ?? b.createdAt) -
            (a.transcript?.updatedAt ?? a.createdAt),
        ),
    [query, sessions],
  );
  const latestBlocks = useMemo(
    () =>
      [
        ...Object.values(activeCommandBlocks),
        ...commandBlocks,
      ]
        .filter((block, index, all) => all.findIndex((item) => item.id === block.id) === index)
        .filter((block) => {
          const needle = query.trim().toLowerCase();
          if (!needle) {
            return true;
          }
          return [
            block.command,
            block.cwd,
            block.outputPreview,
            block.changedFiles.join(" "),
            block.status,
            AGENT_LABELS[block.agent],
          ]
            .join(" ")
            .toLowerCase()
            .includes(needle);
        })
        .sort((a, b) => b.startedAt - a.startedAt),
    [activeCommandBlocks, commandBlocks, query],
  );

  if (
    latestBlocks.length === 0 &&
    latestTranscripts.length === 0 &&
    latestEvents.length === 0
  ) {
    return (
      <section className="session-history-view">
        <input
          className="settings-input"
          aria-label="Search command timeline"
          placeholder="Search timeline"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
        />
        <div className="source-control-empty">No session history yet.</div>
      </section>
    );
  }

  return (
    <section className="session-history-view" aria-label="Session history">
      <input
        className="settings-input"
        aria-label="Search command timeline"
        placeholder="Search commands, output, files"
        value={query}
        onChange={(event) => setQuery(event.target.value)}
      />
      {latestBlocks.map((block) => {
        const workspace = workspaces.find((item) => item.id === block.workspaceId);
        const session = sessions.find((item) => item.id === block.sessionId);
        return (
          <CommandBlockRow
            key={block.id}
            block={block}
            workspaceName={workspace?.name ?? "Workspace"}
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
        const workspace = workspaces.find((item) => item.id === session.workspaceId);
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
      {latestBlocks.length === 0 && latestTranscripts.length === 0
        ? latestEvents.map((event) => {
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
          })
        : null}
    </section>
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
