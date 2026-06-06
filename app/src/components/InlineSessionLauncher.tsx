import { useState } from "react";
import {
  agentDisplayLabel,
  useSessionStore,
  type AgentKind,
  type Workspace,
} from "../lib/sessions";
import { agentIconUrl } from "../lib/agent-icons";
import { NewSessionDialog } from "./NewSessionDialog";

const QUICK_LAUNCH_AGENTS: AgentKind[] = [
  "claude-code",
  "codex",
  "opencode",
  "gemini",
  "goose",
  "aider",
  "cursor",
  "copilot",
  "shell",
];

interface InlineSessionLauncherProps {
  workspace: Workspace;
}

export function InlineSessionLauncher({ workspace }: InlineSessionLauncherProps) {
  const agentLabelOverrides = useSessionStore(
    (state) => state.settings.agentLabelOverrides,
  );
  const defaultAgent = useSessionStore((state) => state.settings.defaultAgent);
  const [dialogAgent, setDialogAgent] = useState<AgentKind | null>(null);

  function launch(agent: AgentKind) {
    setDialogAgent(agent);
  }

  return (
    <>
      <div className="inline-launcher" data-testid="inline-session-launcher">
        <div className="inline-launcher-head">
          <div className="inline-launcher-eyebrow">No session in this tab</div>
          <h2 className="inline-launcher-title">
            Launch an agent in <span className="inline-launcher-ws">{workspace.name}</span>
          </h2>
          <p className="inline-launcher-path" title={workspace.path}>
            {workspace.path}
          </p>
        </div>
        <div className="inline-launcher-grid" role="list">
          {QUICK_LAUNCH_AGENTS.map((agent) => {
            const label = agentDisplayLabel(agent, agentLabelOverrides);
            return (
              <button
                key={agent}
                type="button"
                role="listitem"
                className={`inline-launcher-card${agent === defaultAgent ? " default" : ""}`}
                onClick={() => launch(agent)}
                title={`Launch ${label} in ${workspace.name}`}
              >
                <img
                  className="inline-launcher-icon"
                  src={agentIconUrl(agent)}
                  alt=""
                  aria-hidden="true"
                />
                <span className="inline-launcher-card-label">{label}</span>
                {agent === defaultAgent ? (
                  <span className="inline-launcher-card-tag">default</span>
                ) : null}
              </button>
            );
          })}
        </div>
        <div className="inline-launcher-foot">
          <button
            type="button"
            className="text-button"
            onClick={() => launch(defaultAgent)}
          >
            New session…
          </button>
          <span className="inline-launcher-hint">
            Tip: <kbd>⌘K</kbd> opens the command palette
          </span>
        </div>
      </div>
      <NewSessionDialog
        open={dialogAgent !== null}
        onClose={() => setDialogAgent(null)}
        defaultWorkspaceId={workspace.id}
        defaultCwd={workspace.path}
        defaultAgent={dialogAgent}
      />
    </>
  );
}
