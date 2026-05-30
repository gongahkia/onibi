import { useEffect, useMemo, useState } from "react";
import {
  AGENT_KINDS,
  AGENT_LABELS,
  DEFAULT_AGENT_COMMANDS,
  resolveAgentBinary,
  spawnAgentSession,
  type AgentKind,
  type TerminalPanePlacement,
  useSessionStore,
} from "../lib/sessions";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";

export interface NewSessionDialogProps {
  open: boolean;
  onClose: () => void;
  defaultWorkspaceId?: string | null;
  defaultCwd?: string | null;
  placement?: TerminalPanePlacement | null;
}

export function NewSessionDialog({
  open,
  onClose,
  defaultWorkspaceId,
  defaultCwd,
  placement,
}: NewSessionDialogProps) {
  const workspaces = useSessionStore((state) => state.workspaces);
  const settings = useSessionStore((state) => state.settings);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const [agent, setAgent] = useState<AgentKind>(() => settings.defaultAgent);
  const [workspaceId, setWorkspaceId] = useState<string>(
    () => defaultWorkspaceId ?? workspaces[0]?.id ?? "",
  );
  const [initialPrompt, setInitialPrompt] = useState("");
  const [cwdMode, setCwdMode] = useState<"workspace" | "current">("current");
  const [binaryPath, setBinaryPath] = useState<string | null>(null);
  const [checkingBinary, setCheckingBinary] = useState(false);
  const [choosingWorkspace, setChoosingWorkspace] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [spawning, setSpawning] = useState(false);

  const selectedWorkspace = useMemo(
    () => workspaces.find((workspace) => workspace.id === workspaceId) ?? null,
    [workspaceId, workspaces],
  );
  useEffect(() => {
    if (!open || workspaces.some((workspace) => workspace.id === workspaceId)) {
      return;
    }
    setWorkspaceId(defaultWorkspaceId ?? workspaces[0]?.id ?? "");
  }, [defaultWorkspaceId, open, workspaceId, workspaces]);

  useEffect(() => {
    if (!open || !defaultWorkspaceId) {
      return;
    }
    setWorkspaceId(defaultWorkspaceId);
  }, [defaultWorkspaceId, open]);

  useEffect(() => {
    if (!open) {
      return;
    }
    setAgent(settings.defaultAgent);
  }, [open, settings.defaultAgent]);

  const launchCommandText =
    agent === "shell"
      ? "system shell"
      : settings.agentCommands[agent] || DEFAULT_AGENT_COMMANDS[agent] || "system shell";

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

  async function chooseWorkspace() {
    setError(null);
    setChoosingWorkspace(true);
    try {
      const workspace = await chooseWorkspaceFolder();
      if (!workspace) {
        return;
      }
      addWorkspace(workspace);
      setWorkspaceId(workspace.id);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setChoosingWorkspace(false);
    }
  }

  async function handleStart() {
    setError(null);
    setSpawning(true);
    try {
      if (!selectedWorkspace) {
        throw new Error("Choose a workspace folder.");
      }
      if (missingBinary) {
        throw new Error(
          `${AGENT_LABELS[agent]} is not on PATH. Install it, or update its launch command in Settings > Agents.`,
        );
      }
      await spawnAgentSession(
        agent,
        selectedWorkspace,
        initialPrompt,
        placement,
        {
          cwd:
            cwdMode === "current" && defaultCwd?.startsWith(selectedWorkspace.path)
              ? defaultCwd
              : selectedWorkspace.path,
        },
      );
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
              <span className="workspace-picker-row">
                <select
                  className="settings-select"
                  value={workspaceId}
                  disabled={workspaces.length === 0}
                  onChange={(event) => setWorkspaceId(event.target.value)}
                >
                  {workspaces.length === 0 ? (
                    <option value="">No workspace selected</option>
                  ) : null}
                  {workspaces.map((workspace) => (
                    <option key={workspace.id} value={workspace.id}>
                      {workspace.name}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  className="text-button"
                  disabled={choosingWorkspace}
                  onClick={() => void chooseWorkspace()}
                >
                  {choosingWorkspace ? "Choosing" : "Choose Folder"}
                </button>
              </span>
            </label>
            {selectedWorkspace ? (
              <div className="settings-note">{selectedWorkspace.path}</div>
            ) : null}
            {selectedWorkspace && defaultCwd?.startsWith(selectedWorkspace.path) ? (
              <label className="field-label">
                Start directory
                <select
                  className="settings-select"
                  value={cwdMode}
                  onChange={(event) =>
                    setCwdMode(event.target.value as "workspace" | "current")
                  }
                >
                  <option value="current">{defaultCwd}</option>
                  <option value="workspace">{selectedWorkspace.path}</option>
                </select>
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
              {launchCommandText}
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
              disabled={spawning || checkingBinary || !selectedWorkspace}
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
