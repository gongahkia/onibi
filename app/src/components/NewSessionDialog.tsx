import { useEffect, useMemo, useState } from "react";
import {
  AGENT_KINDS,
  AGENT_LABELS,
  DEFAULT_AGENT_COMMANDS,
  resolveAgentBinary,
  spawnAgentSession,
  type AgentKind,
  useSessionStore,
  workspaceFromPath,
} from "../lib/sessions";

export interface NewSessionDialogProps {
  open: boolean;
  onClose: () => void;
}

const OPEN_FOLDER_VALUE = "__open_folder__";

export function NewSessionDialog({ open, onClose }: NewSessionDialogProps) {
  const workspaces = useSessionStore((state) => state.workspaces);
  const settings = useSessionStore((state) => state.settings);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const [agent, setAgent] = useState<AgentKind>("claude-code");
  const [workspaceId, setWorkspaceId] = useState<string>(OPEN_FOLDER_VALUE);
  const [folderPath, setFolderPath] = useState("");
  const [initialPrompt, setInitialPrompt] = useState("");
  const [binaryPath, setBinaryPath] = useState<string | null>(null);
  const [checkingBinary, setCheckingBinary] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [spawning, setSpawning] = useState(false);

  const selectedWorkspace = useMemo(
    () => workspaces.find((workspace) => workspace.id === workspaceId) ?? null,
    [workspaceId, workspaces],
  );

  useEffect(() => {
    if (open && workspaces.length > 0 && workspaceId === OPEN_FOLDER_VALUE) {
      setWorkspaceId(workspaces[0].id);
    }
  }, [open, workspaceId, workspaces]);

  useEffect(() => {
    if (!open || agent === "shell") {
      setBinaryPath(null);
      setCheckingBinary(false);
      return;
    }
    let cancelled = false;
    setCheckingBinary(true);
    void resolveAgentBinary(agent, settings)
      .then((path) => {
        if (!cancelled) {
          setBinaryPath(path);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setBinaryPath(null);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setCheckingBinary(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [agent, open, settings]);

  if (!open) {
    return null;
  }

  const missingBinary = agent !== "shell" && !checkingBinary && !binaryPath;

  async function handleStart() {
    setError(null);
    setSpawning(true);
    try {
      let workspace = selectedWorkspace;
      if (!workspace) {
        if (!folderPath.trim()) {
          throw new Error("Choose a workspace folder.");
        }
        workspace = await workspaceFromPath(folderPath.trim());
        addWorkspace(workspace);
      }
      if (!workspace) {
        throw new Error("Choose a workspace folder.");
      }
      if (missingBinary) {
        throw new Error(
          `${AGENT_LABELS[agent]} is not on PATH. Install details will live in docs/adapters.md once Phase-03 lands.`,
        );
      }
      await spawnAgentSession(agent, workspace, initialPrompt);
      setInitialPrompt("");
      onClose();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setSpawning(false);
    }
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <section
        className="modal-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="new-session-title"
      >
        <header className="modal-header">
          <h2 className="modal-title" id="new-session-title">
            New Session
          </h2>
          <button
            type="button"
            className="icon-button"
            aria-label="Close new session dialog"
            onClick={onClose}
          >
            x
          </button>
        </header>
        <div className="modal-body">
          <div className="form-grid">
            <label className="field-label">
              Agent
              <select
                className="settings-select"
                value={agent}
                onChange={(event) => setAgent(event.target.value as AgentKind)}
              >
                {AGENT_KINDS.map((kind) => (
                  <option key={kind} value={kind}>
                    {AGENT_LABELS[kind]}
                  </option>
                ))}
              </select>
            </label>
            <label className="field-label">
              Workspace
              <select
                className="settings-select"
                value={workspaceId}
                onChange={(event) => setWorkspaceId(event.target.value)}
              >
                {workspaces.map((workspace) => (
                  <option key={workspace.id} value={workspace.id}>
                    {workspace.name}
                  </option>
                ))}
                <option value={OPEN_FOLDER_VALUE}>Open Folder...</option>
              </select>
            </label>
            {workspaceId === OPEN_FOLDER_VALUE ? (
              <label className="field-label">
                Folder path
                <input
                  className="settings-input"
                  value={folderPath}
                  onChange={(event) => setFolderPath(event.target.value)}
                  placeholder="/Users/name/project"
                />
              </label>
            ) : null}
            <label className="field-label">
              Initial prompt
              <textarea
                className="prompt-input"
                rows={4}
                value={initialPrompt}
                onChange={(event) => setInitialPrompt(event.target.value)}
              />
            </label>
            <div className="settings-note">
              Launch command:{" "}
              {settings.agentCommands[agent] || DEFAULT_AGENT_COMMANDS[agent] || "system shell"}
              {agent !== "shell" ? (
                <>
                  {" "}
                  ({checkingBinary ? "checking PATH" : binaryPath ?? "missing"})
                </>
              ) : null}
            </div>
          </div>
          {error ? <div className="editor-error">{error}</div> : null}
          <footer className="dialog-actions">
            <button type="button" className="text-button" onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className="text-button primary"
              disabled={spawning || checkingBinary}
              onClick={() => void handleStart()}
            >
              {spawning ? "Starting" : "Start"}
            </button>
          </footer>
        </div>
      </section>
    </div>
  );
}
