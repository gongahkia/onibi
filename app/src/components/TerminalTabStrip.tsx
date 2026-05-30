import { useMemo } from "react";
import { agentIconUrl } from "../lib/agent-icons";
import {
  AGENT_LABELS,
  sessionAttentionState,
  useSessionStore,
  type Session,
  type TerminalLeafPane,
} from "../lib/sessions";

export interface TerminalTabStripProps {
  leaf: TerminalLeafPane;
  sessions: Session[];
  onAddSession: (paneId: string) => void;
  onCloseSession: (session: Session) => void;
}

export function TerminalTabStrip({
  leaf,
  sessions,
  onAddSession,
  onCloseSession,
}: TerminalTabStripProps) {
  const setActivePaneSession = useSessionStore((state) => state.setActivePaneSession);
  const sessionById = useMemo(
    () => new Map(sessions.map((session) => [session.id, session])),
    [sessions],
  );
  const ids = leaf.sessionIds && leaf.sessionIds.length > 0 ? leaf.sessionIds : [leaf.sessionId];
  const tabs = ids
    .map((id) => sessionById.get(id))
    .filter((session): session is Session => Boolean(session));

  // Hide the strip when there's only a single tab to avoid clutter.
  if (tabs.length <= 1) {
    return (
      <div className="terminal-tab-strip terminal-tab-strip-empty">
        <button
          type="button"
          className="terminal-tab-add"
          aria-label="New terminal in this pane"
          title="New terminal in this pane"
          onClick={(event) => {
            event.stopPropagation();
            onAddSession(leaf.paneId);
          }}
        >
          <i className="codicon codicon-add" aria-hidden="true" />
        </button>
      </div>
    );
  }

  return (
    <div className="terminal-tab-strip" role="tablist" aria-label="Terminals in pane">
      {tabs.map((session) => {
        const isActive = session.id === leaf.sessionId;
        const attention = sessionAttentionState(session);
        return (
          <div
            key={session.id}
            role="tab"
            aria-selected={isActive}
            className={`terminal-tab ${isActive ? "active" : ""}`}
            data-attention={attention}
            onClick={(event) => {
              event.stopPropagation();
              setActivePaneSession(leaf.paneId, session.id);
            }}
            onAuxClick={(event) => {
              if (event.button === 1) {
                event.preventDefault();
                event.stopPropagation();
                onCloseSession(session);
              }
            }}
            title={`${AGENT_LABELS[session.agent]} · ${session.title}`}
          >
            <img className="terminal-tab-icon" src={agentIconUrl(session.agent)} alt="" />
            <span className="terminal-tab-label">{session.title}</span>
            <span className={`terminal-tab-status ${session.status}`} aria-hidden="true" />
            <button
              type="button"
              className="terminal-tab-close"
              aria-label="Close terminal"
              title="Close terminal"
              onClick={(event) => {
                event.stopPropagation();
                onCloseSession(session);
              }}
            >
              <i className="codicon codicon-close" aria-hidden="true" />
            </button>
          </div>
        );
      })}
      <button
        type="button"
        className="terminal-tab-add"
        aria-label="New terminal in this pane"
        title="New terminal in this pane"
        onClick={(event) => {
          event.stopPropagation();
          onAddSession(leaf.paneId);
        }}
      >
        <i className="codicon codicon-add" aria-hidden="true" />
      </button>
    </div>
  );
}
