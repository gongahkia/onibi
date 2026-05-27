import { useMemo, useState } from "react";
import { AGENT_LABELS, type AgentKind, useSessionStore } from "../lib/sessions";
import { NewSessionDialog } from "./NewSessionDialog";
import { SettingsPane } from "./SettingsPane";
import claudeIcon from "../assets/agents/claude-code.svg";
import codexIcon from "../assets/agents/codex.svg";
import opencodeIcon from "../assets/agents/opencode.svg";
import geminiIcon from "../assets/agents/gemini.svg";
import aiderIcon from "../assets/agents/aider.svg";
import cursorIcon from "../assets/agents/cursor.svg";
import gooseIcon from "../assets/agents/goose.svg";
import shellIcon from "../assets/agents/shell.svg";

export interface AgentTabBarProps {
  orientation: "vertical" | "horizontal";
}

const AGENT_ICONS: Record<AgentKind, string> = {
  "claude-code": claudeIcon,
  codex: codexIcon,
  opencode: opencodeIcon,
  gemini: geminiIcon,
  aider: aiderIcon,
  cursor: cursorIcon,
  goose: gooseIcon,
  shell: shellIcon,
};

export function agentIconUrl(agent: AgentKind): string {
  return AGENT_ICONS[agent];
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
