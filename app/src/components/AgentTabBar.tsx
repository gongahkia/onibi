import { useMemo, useState } from "react";
import { AGENT_LABELS, useSessionStore } from "../lib/sessions";
import { agentIconUrl } from "../lib/agent-icons";
import { NewSessionDialog } from "./NewSessionDialog";
import { SettingsPane } from "./SettingsPane";

export interface AgentTabBarProps {
  orientation: "vertical" | "horizontal";
}

export function AgentTabBar({ orientation }: AgentTabBarProps) {
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const sessions = useSessionStore((state) => state.sessions);
  const workspaces = useSessionStore((state) => state.workspaces);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const setActiveSession = useSessionStore((state) => state.setActiveSession);
  const workspaceById = useMemo(
    () => new Map(workspaces.map((workspace) => [workspace.id, workspace])),
    [workspaces],
  );

  return (
    <nav className={`agent-tab-bar ${orientation}`} aria-label="Agent sessions">
      <div className="agent-tabs">
        {sessions.map((session) => {
          const workspace = workspaceById.get(session.workspaceId);
          const flash =
            session.status === "awaiting-approval" || session.status === "completed";
          return (
            <button
              key={session.id}
              type="button"
              className={`tab ${activeSessionId === session.id ? "active" : ""} ${
                flash ? "flash" : ""
              }`}
              title={`${AGENT_LABELS[session.agent]} · ${workspace?.name ?? "Workspace"}`}
              aria-label={`${AGENT_LABELS[session.agent]} session`}
              onClick={() => setActiveSession(session.id)}
            >
              <img src={agentIconUrl(session.agent)} alt="" />
              <span className={`tab-status ${session.status}`} />
            </button>
          );
        })}
      </div>
      <div className="agent-actions">
        <button
          type="button"
          className="icon-button"
          aria-label="New session"
          title="New session"
          onClick={() => setNewSessionOpen(true)}
        >
          +
        </button>
        <button
          type="button"
          className="icon-button"
          aria-label="Settings"
          title="Settings"
          onClick={() => setSettingsOpen(true)}
        >
          ⚙
        </button>
      </div>
      <NewSessionDialog
        open={newSessionOpen}
        onClose={() => setNewSessionOpen(false)}
      />
      <SettingsPane open={settingsOpen} onClose={() => setSettingsOpen(false)} />
    </nav>
  );
}
