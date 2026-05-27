import { useEffect, useState } from "react";
import {
  AGENT_KINDS,
  AGENT_LABELS,
  DEFAULT_AGENT_COMMANDS,
  type AgentKind,
  type TabBarOrientation,
  type TabBarPosition,
  type ThemeMode,
  resolveAgentBinary,
  useSessionStore,
  workspaceFromPath,
} from "../lib/sessions";

export interface SettingsPaneProps {
  open: boolean;
  onClose: () => void;
}

type SettingsSection = "general" | "layout" | "agents" | "workspaces";
type BinaryStatus = Record<AgentKind, string | null>;

export function SettingsPane({ open, onClose }: SettingsPaneProps) {
  const settings = useSessionStore((state) => state.settings);
  const workspaces = useSessionStore((state) => state.workspaces);
  const updateSettings = useSessionStore((state) => state.updateSettings);
  const updateAgentCommand = useSessionStore((state) => state.updateAgentCommand);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const removeWorkspace = useSessionStore((state) => state.removeWorkspace);
  const [section, setSection] = useState<SettingsSection>("general");
  const [binaryStatus, setBinaryStatus] = useState<BinaryStatus>(() =>
    Object.fromEntries(AGENT_KINDS.map((agent) => [agent, null])) as BinaryStatus,
  );
  const [workspacePath, setWorkspacePath] = useState("");
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    void Promise.all(
      AGENT_KINDS.map(async (agent) => {
        const path = await resolveAgentBinary(agent, settings).catch(() => null);
        return [agent, path] as const;
      }),
    ).then((entries) => {
      if (!cancelled) {
        setBinaryStatus(Object.fromEntries(entries) as BinaryStatus);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [open, settings]);

  if (!open) {
    return null;
  }

  async function addWorkspaceFromInput() {
    if (!workspacePath.trim()) {
      return;
    }
    setError(null);
    try {
      const workspace = await workspaceFromPath(workspacePath.trim());
      addWorkspace(workspace);
      setWorkspacePath("");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : String(caught));
    }
  }

  return (
    <div className="modal-backdrop settings-pane" role="presentation">
      <section
        className="modal-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
      >
        <header className="modal-header">
          <h2 className="modal-title" id="settings-title">
            Settings
          </h2>
          <button
            type="button"
            className="icon-button"
            aria-label="Close settings"
            onClick={onClose}
          >
            x
          </button>
        </header>
        <div className="settings-grid">
          <nav className="settings-nav" aria-label="Settings sections">
            {(["general", "layout", "agents", "workspaces"] as const).map((item) => (
              <button
                key={item}
                type="button"
                className={`text-button ${section === item ? "primary" : ""}`}
                onClick={() => setSection(item)}
              >
                {labelFor(item)}
              </button>
            ))}
          </nav>
          {section === "general" ? (
            <GeneralSettings
              theme={settings.theme}
              fontFamily={settings.fontFamily}
              fontSize={settings.fontSize}
              onTheme={(theme) => updateSettings({ theme })}
              onFontFamily={(fontFamily) => updateSettings({ fontFamily })}
              onFontSize={(fontSize) => updateSettings({ fontSize })}
            />
          ) : null}
          {section === "layout" ? (
            <LayoutSettings
              orientation={settings.tabBarOrientation}
              position={settings.tabBarPosition}
              onOrientation={(orientation) =>
                updateSettings({
                  tabBarOrientation: orientation,
                  tabBarPosition: orientation === "horizontal" ? "top" : "left",
                })
              }
              onPosition={(tabBarPosition) => updateSettings({ tabBarPosition })}
            />
          ) : null}
          {section === "agents" ? (
            <AgentSettings
              binaryStatus={binaryStatus}
              commands={settings.agentCommands}
              onCommand={updateAgentCommand}
            />
          ) : null}
          {section === "workspaces" ? (
            <WorkspaceSettings
              workspacePath={workspacePath}
              error={error}
              workspaces={workspaces}
              onPath={setWorkspacePath}
              onAdd={() => void addWorkspaceFromInput()}
              onRemove={removeWorkspace}
            />
          ) : null}
        </div>
      </section>
    </div>
  );
}

function labelFor(value: string): string {
  return value[0].toUpperCase() + value.slice(1);
}

interface GeneralSettingsProps {
  theme: ThemeMode;
  fontFamily: string;
  fontSize: number;
  onTheme: (theme: ThemeMode) => void;
  onFontFamily: (fontFamily: string) => void;
  onFontSize: (fontSize: number) => void;
}

function GeneralSettings({
  theme,
  fontFamily,
  fontSize,
  onTheme,
  onFontFamily,
  onFontSize,
}: GeneralSettingsProps) {
  return (
    <section className="settings-section" aria-label="General settings">
      <label className="settings-row">
        <span>Theme</span>
        <select
          className="settings-select"
          value={theme}
          onChange={(event) => onTheme(event.target.value as ThemeMode)}
        >
          <option value="dark">Dark</option>
          <option value="light">Light</option>
          <option value="system">System</option>
        </select>
      </label>
      <label className="settings-row">
        <span>Font family</span>
        <input
          className="settings-input"
          value={fontFamily}
          onChange={(event) => onFontFamily(event.target.value)}
        />
      </label>
      <label className="settings-row">
        <span>Font size</span>
        <input
          className="settings-input"
          type="number"
          min={10}
          max={24}
          value={fontSize}
          onChange={(event) => onFontSize(Number(event.target.value))}
        />
      </label>
    </section>
  );
}

interface LayoutSettingsProps {
  orientation: TabBarOrientation;
  position: TabBarPosition;
  onOrientation: (orientation: TabBarOrientation) => void;
  onPosition: (position: TabBarPosition) => void;
}

function LayoutSettings({
  orientation,
  position,
  onOrientation,
  onPosition,
}: LayoutSettingsProps) {
  const positions =
    orientation === "horizontal"
      ? (["top", "bottom"] as const)
      : (["left", "right"] as const);
  return (
    <section className="settings-section" aria-label="Layout settings">
      <label className="settings-row">
        <span>Tab bar orientation</span>
        <select
          className="settings-select"
          value={orientation}
          onChange={(event) => onOrientation(event.target.value as TabBarOrientation)}
        >
          <option value="vertical">Vertical</option>
          <option value="horizontal">Horizontal</option>
        </select>
      </label>
      <label className="settings-row">
        <span>Tab bar position</span>
        <select
          className="settings-select"
          value={position}
          onChange={(event) => onPosition(event.target.value as TabBarPosition)}
        >
          {positions.map((item) => (
            <option key={item} value={item}>
              {labelFor(item)}
            </option>
          ))}
        </select>
      </label>
    </section>
  );
}

interface AgentSettingsProps {
  commands: Record<AgentKind, string>;
  binaryStatus: BinaryStatus;
  onCommand: (agent: AgentKind, command: string) => void;
}

function AgentSettings({ commands, binaryStatus, onCommand }: AgentSettingsProps) {
  return (
    <section className="settings-section" aria-label="Agent settings">
      {AGENT_KINDS.map((agent) => {
        const status = agent === "shell" ? "system shell" : binaryStatus[agent] ?? "missing";
        return (
          <label className="agent-command-row" key={agent}>
            <span>{AGENT_LABELS[agent]}</span>
            <input
              className="settings-input"
              value={commands[agent] ?? DEFAULT_AGENT_COMMANDS[agent]}
              onChange={(event) => onCommand(agent, event.target.value)}
            />
            <span
              className={`binary-status ${
                status === "missing" ? "missing" : "found"
              }`}
              title={status}
            >
              {status === "missing" ? "missing" : "found"}
            </span>
          </label>
        );
      })}
    </section>
  );
}

interface WorkspaceSettingsProps {
  workspacePath: string;
  error: string | null;
  workspaces: Array<{ id: string; path: string; name: string }>;
  onPath: (path: string) => void;
  onAdd: () => void;
  onRemove: (id: string) => void;
}

function WorkspaceSettings({
  workspacePath,
  error,
  workspaces,
  onPath,
  onAdd,
  onRemove,
}: WorkspaceSettingsProps) {
  return (
    <section className="settings-section" aria-label="Workspace settings">
      <div className="workspace-settings-row">
        <span>Add workspace</span>
        <input
          className="settings-input"
          value={workspacePath}
          placeholder="/Users/name/project"
          onChange={(event) => onPath(event.target.value)}
        />
        <button type="button" className="text-button" onClick={onAdd}>
          Add
        </button>
      </div>
      {error ? <div className="editor-error">{error}</div> : null}
      {workspaces.map((workspace) => (
        <div className="workspace-settings-row" key={workspace.id}>
          <span>{workspace.name}</span>
          <span className="settings-note">{workspace.path}</span>
          <button
            type="button"
            className="text-button danger"
            onClick={() => onRemove(workspace.id)}
          >
            Remove
          </button>
        </div>
      ))}
    </section>
  );
}
