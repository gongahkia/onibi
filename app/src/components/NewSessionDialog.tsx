import { useEffect, useMemo, useState } from "react";
import {
  AGENT_KINDS,
  DEFAULT_AGENT_COMMANDS,
  agentDisplayLabel,
  resolveAgentBinary,
  resolveCommandBinary,
  spawnAgentSession,
  type AgentKind,
  type TerminalPanePlacement,
  useSessionStore,
} from "../lib/sessions";
import {
  acpAdapterForAgent,
  acpCommandLine,
  isAcpTransportEnabled,
  promptAcpAgentSession,
} from "../lib/acp";
import { useConfigStatusQuery } from "../lib/queries";
import { chooseWorkspaceFolder } from "../lib/workspace-picker";
import { ProseComposer } from "./composer/ProseComposer";

export interface NewSessionDialogProps {
  open: boolean;
  onClose: () => void;
  defaultWorkspaceId?: string | null;
  defaultCwd?: string | null;
  defaultAgent?: AgentKind | null;
  placement?: TerminalPanePlacement | null;
}

export function NewSessionDialog({
  open,
  onClose,
  defaultWorkspaceId,
  defaultCwd,
  defaultAgent,
  placement,
}: NewSessionDialogProps) {
  const workspaces = useSessionStore((state) => state.workspaces);
  const sessions = useSessionStore((state) => state.sessions);
  const settings = useSessionStore((state) => state.settings);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const setActiveSession = useSessionStore((state) => state.setActiveSession);
  const [agent, setAgent] = useState<AgentKind>(
    () => defaultAgent ?? settings.defaultAgent,
  );
  const [workspaceId, setWorkspaceId] = useState<string>(
    () => defaultWorkspaceId ?? workspaces[0]?.id ?? "",
  );
  const [initialPrompt, setInitialPrompt] = useState("");
  const [cwdMode, setCwdMode] = useState<"workspace" | "current">("current");
  const [trustMode, setTrustMode] = useState<"approval-required" | "full-access">(
    "approval-required",
  );
  const [worktreeStrategy, setWorktreeStrategy] = useState<"inherit" | "auto">("inherit");
  const [binaryPath, setBinaryPath] = useState<string | null>(null);
  const [checkingBinary, setCheckingBinary] = useState(false);
  const [choosingWorkspace, setChoosingWorkspace] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [spawning, setSpawning] = useState(false);
  const { data: configStatus } = useConfigStatusQuery({
    enabled: open,
    staleTime: 5_000,
  });

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
    setAgent(defaultAgent ?? settings.defaultAgent);
  }, [defaultAgent, open, settings.defaultAgent]);

  const acpAdapter = useMemo(
    () => acpAdapterForAgent(agent, configStatus),
    [agent, configStatus],
  );
  const acpEnabled = isAcpTransportEnabled(agent, configStatus);

  useEffect(() => {
    if ((agent === "shell" || acpEnabled) && worktreeStrategy === "auto") {
      setWorktreeStrategy("inherit");
    }
  }, [acpEnabled, agent, worktreeStrategy]);

  const launchCommandText =
    agent === "shell"
      ? "system shell"
      : acpEnabled && acpAdapter
        ? acpCommandLine(acpAdapter)
        : settings.agentCommands[agent] || DEFAULT_AGENT_COMMANDS[agent] || "system shell";

  useEffect(() => {
    if (!open || agent === "shell") {
      setBinaryPath(null);
      setCheckingBinary(false);
      return;
    }
    let cancelled = false;
    setCheckingBinary(true);
    const resolver =
      acpEnabled && acpAdapter
        ? resolveCommandBinary(acpAdapter.acpCommand)
        : resolveAgentBinary(agent, settings);
    void resolver
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
  }, [acpAdapter, acpEnabled, agent, open, settings]);

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
          acpEnabled
            ? `${agentDisplayLabel(agent, settings)} ACP command is not on PATH. Update [adapters.${agent === "hermes" ? "hermes" : "claude"}].acp_command in config.toml.`
            : `${agentDisplayLabel(agent, settings)} is not on PATH. Install it, or update its launch command in Settings > Agents.`,
        );
      }
      const existing = sessions.find(
        (session) =>
          !acpEnabled &&
          worktreeStrategy === "inherit" &&
          session.agent === agent &&
          session.workspaceId === selectedWorkspace.id &&
          session.status !== "stale" &&
          session.status !== "completed" &&
          session.status !== "error",
      );
      if (existing) {
        setActiveSession(existing.id);
        onClose();
        return;
      }
      const cwd =
        cwdMode === "current" && defaultCwd?.startsWith(selectedWorkspace.path)
          ? defaultCwd
          : selectedWorkspace.path;
      if (acpEnabled) {
        const prompt = initialPrompt.trim();
        if (!prompt) {
          throw new Error("Enter an initial prompt for ACP launch.");
        }
        const acpAgent =
          agent === "claude-code" || agent === "hermes" ? agent : null;
        if (!acpAgent) {
          throw new Error(`${agentDisplayLabel(agent, settings)} does not support ACP launch.`);
        }
        await promptAcpAgentSession({
          agent: acpAgent,
          cwd,
          prompt,
        });
        setInitialPrompt("");
        onClose();
        return;
      }
      await spawnAgentSession(
        agent,
        selectedWorkspace,
        initialPrompt,
        placement,
        {
          cwd,
          trustMode,
          worktreeStrategy,
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
                    {agentDisplayLabel(kind, settings)}
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
            <label className="field-label">
              Trust mode
              <select
                className="settings-select"
                value={trustMode}
                disabled={acpEnabled}
                onChange={(event) =>
                  setTrustMode(event.target.value as "approval-required" | "full-access")
                }
              >
                <option value="approval-required">Approval required</option>
                <option value="full-access">Full access</option>
              </select>
            </label>
            <label className="settings-checkbox-row">
              <input
                type="checkbox"
                checked={worktreeStrategy === "auto"}
                disabled={agent === "shell" || acpEnabled}
                onChange={(event) =>
                  setWorktreeStrategy(event.target.checked ? "auto" : "inherit")
                }
              />
              <span>Auto worktree</span>
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
              <ProseComposer
                ariaLabel="Initial prompt"
                className="prompt-input prompt-composer"
                value={initialPrompt}
                onChange={setInitialPrompt}
              />
            </label>
            <div className="settings-note">
              {acpEnabled ? "ACP command: " : "Launch command: "}
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
