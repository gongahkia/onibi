import { useEffect, useMemo, useState } from "react";
import {
  AGENT_KINDS,
  DEFAULT_AGENT_COMMANDS,
  agentDisplayLabel,
  recordAcpPromptSession,
  resolveAgentBinary,
  resolveCommandBinary,
  spawnAgentSession,
  type AgentKind,
  type Session,
  type TerminalPanePlacement,
  useSessionStore,
} from "../lib/sessions";
import {
  type AcpAgentKind,
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
  const [acpResumeSessionId, setAcpResumeSessionId] = useState("");
  const [acpResult, setAcpResult] = useState<string | null>(null);
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
  const acpResumeOptions = useMemo(
    () => acpResumeOptionsFor(agent, acpEnabled, sessions),
    [acpEnabled, agent, sessions],
  );

  useEffect(() => {
    if (!open || !acpEnabled) {
      setAcpResumeSessionId("");
      return;
    }
    if (
      acpResumeSessionId &&
      !acpResumeOptions.some((option) => option.id === acpResumeSessionId)
    ) {
      setAcpResumeSessionId("");
    }
  }, [acpEnabled, acpResumeOptions, acpResumeSessionId, open]);

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
    setAcpResult(null);
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
    setAcpResult(null);
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
        const result = await promptAcpAgentSession({
          agent: acpAgent,
          cwd,
          prompt,
          resumeSessionId: acpResumeSessionId || null,
        });
        recordAcpPromptSession(agent, selectedWorkspace, result);
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
                onChange={(event) => {
                  setAcpResult(null);
                  setAgent(event.target.value as AgentKind);
                }}
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
                  onChange={(event) => {
                    setAcpResult(null);
                    setWorkspaceId(event.target.value);
                  }}
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
                  onChange={(event) => {
                    setAcpResult(null);
                    setCwdMode(event.target.value as "workspace" | "current");
                  }}
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
                onChange={(value) => {
                  setAcpResult(null);
                  setInitialPrompt(value);
                }}
              />
            </label>
            {acpEnabled && acpResumeOptions.length > 0 ? (
              <label className="field-label">
                Resume ACP session
                <select
                  className="settings-select"
                  aria-label="Resume ACP session"
                  value={acpResumeSessionId}
                  onChange={(event) => {
                    setAcpResult(null);
                    setAcpResumeSessionId(event.target.value);
                  }}
                >
                  <option value="">New ACP session</option>
                  {acpResumeOptions.map((option) => (
                    <option key={option.id} value={option.id}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}
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
          {acpResult ? (
            <div className="settings-note" role="status">
              {acpResult}
            </div>
          ) : null}
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

interface AcpResumeOption {
  id: string;
  label: string;
}

function acpResumeOptionsFor(
  agent: AgentKind,
  acpEnabled: boolean,
  sessions: Session[],
): AcpResumeOption[] {
  if (!acpEnabled || !isAcpAgent(agent)) {
    return [];
  }
  const seen = new Set<string>();
  return sessions.flatMap((session) => {
    if (session.agent !== agent) {
      return [];
    }
    const id = acpResumeId(session);
    if (!id || seen.has(id)) {
      return [];
    }
    seen.add(id);
    return [{ id, label: acpResumeLabel(session, id) }];
  });
}

function isAcpAgent(agent: AgentKind): agent is AcpAgentKind {
  return agent === "claude-code" || agent === "hermes";
}

function acpResumeId(session: Session): string | null {
  const provider = session.provider;
  return (
    cleanString(provider?.providerSessionId) ??
    cleanString(provider?.conversationId) ??
    lastNonEmpty(provider?.resume?.args) ??
    null
  );
}

function acpResumeLabel(session: Session, id: string): string {
  const provider = session.provider;
  const details = [
    cleanString(provider?.providerSessionId)
      ? `provider ${cleanString(provider?.providerSessionId)}`
      : null,
    cleanString(provider?.conversationId)
      ? `conversation ${cleanString(provider?.conversationId)}`
      : null,
    cleanString(provider?.resume?.source),
  ].filter((value): value is string => value !== null);
  const state =
    session.status === "running" || session.status === "awaiting-approval"
      ? "reattach"
      : "resume";
  return `${session.title || session.id} - ${state} - ${details.join(" · ") || id}`;
}

function cleanString(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

function lastNonEmpty(values: string[] | undefined): string | null {
  return (
    values
      ?.map(cleanString)
      .filter((value): value is string => value !== null)
      .at(-1) ?? null
  );
}
