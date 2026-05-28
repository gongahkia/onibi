import { useEffect, useState } from "react";
import {
  AGENT_KINDS,
  AGENT_LABELS,
  COLOR_SCHEME_COLOR_KEYS,
  COLOR_SCHEME_COLOR_LABELS,
  COLOR_SCHEME_OPTIONS,
  DEFAULT_AGENT_COMMANDS,
  DEFAULT_AGENT_INSTALL_COMMANDS,
  type AgentKind,
  type ColorSchemeColorKey,
  type CustomColorScheme,
  type DiffViewMode,
  type TabBarOrientation,
  type TabBarPosition,
  type ThemeMode,
  type WebOpenMode,
  listLocalFontFamilies,
  resolveAgentBinary,
  useSessionStore,
  workspaceFromPath,
} from "../lib/sessions";
import { ptySpawn, shellPath } from "../lib/tauri-bridge";

export interface SettingsPaneProps {
  open: boolean;
  onClose: () => void;
}

type SettingsSection = "general" | "layout" | "agents" | "workspaces";
type BinaryStatus = Record<AgentKind, string | null>;

export function SettingsPane({ open, onClose }: SettingsPaneProps) {
  const settings = useSessionStore((state) => state.settings);
  const sessions = useSessionStore((state) => state.sessions);
  const activeSessionId = useSessionStore((state) => state.activeSessionId);
  const workspaces = useSessionStore((state) => state.workspaces);
  const updateSettings = useSessionStore((state) => state.updateSettings);
  const updateAgentCommand = useSessionStore((state) => state.updateAgentCommand);
  const addSession = useSessionStore((state) => state.addSession);
  const addWorkspace = useSessionStore((state) => state.addWorkspace);
  const removeWorkspace = useSessionStore((state) => state.removeWorkspace);
  const [section, setSection] = useState<SettingsSection>("general");
  const [binaryStatus, setBinaryStatus] = useState<BinaryStatus>(() =>
    Object.fromEntries(AGENT_KINDS.map((agent) => [agent, null])) as BinaryStatus,
  );
  const [fontFamilies, setFontFamilies] = useState<string[]>([]);
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
  }, [open, settings.agentCommands]);

  useEffect(() => {
    if (!open) {
      return;
    }
    let cancelled = false;
    void listLocalFontFamilies()
      .then((families) => {
        if (!cancelled) {
          setFontFamilies(families);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setFontFamilies([]);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

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

  async function runInstall(agent: AgentKind, command: string) {
    if (!command.trim()) {
      return;
    }
    if (!window.confirm(`Run install command for ${AGENT_LABELS[agent]}?`)) {
      return;
    }
    const activeSession = sessions.find((session) => session.id === activeSessionId);
    const workspace =
      workspaces.find((item) => item.id === activeSession?.workspaceId) ??
      workspaces[0] ??
      null;
    if (!workspace) {
      setError("Add a workspace before running an install command.");
      setSection("workspaces");
      return;
    }
    const id = await ptySpawn({
      command: shellPath(),
      args: ["-lc", command],
      cwd: workspace.path,
      env: [],
      rows: 30,
      cols: 100,
    });
    addSession({
      id,
      agent: "shell",
      workspaceId: workspace.id,
      title: `Install ${AGENT_LABELS[agent]} · ${workspace.name}`,
      status: "running",
      createdAt: Date.now(),
      pendingApprovals: [],
    });
    onClose();
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
              customColorScheme={settings.customColorScheme}
              uiFontFamily={settings.uiFontFamily}
              terminalFontFamily={settings.terminalFontFamily}
              editorFontFamily={settings.editorFontFamily}
              fontFamilies={fontFamilies}
              uiFontSize={settings.uiFontSize}
              terminalFontSize={settings.terminalFontSize}
              editorFontSize={settings.editorFontSize}
              diffViewMode={settings.diffViewMode}
              webOpenMode={settings.webOpenMode}
              onTheme={(theme) => updateSettings({ theme })}
              onCustomColorScheme={(customColorScheme) =>
                updateSettings({ customColorScheme })
              }
              onUiFontFamily={(uiFontFamily) => updateSettings({ uiFontFamily })}
              onTerminalFontFamily={(terminalFontFamily) =>
                updateSettings({ terminalFontFamily })
              }
              onEditorFontFamily={(editorFontFamily) =>
                updateSettings({ editorFontFamily })
              }
              onUiFontSize={(uiFontSize) => updateSettings({ uiFontSize })}
              onTerminalFontSize={(terminalFontSize) =>
                updateSettings({ terminalFontSize })
              }
              onEditorFontSize={(editorFontSize) => updateSettings({ editorFontSize })}
              onDiffViewMode={(diffViewMode) => updateSettings({ diffViewMode })}
              onWebOpenMode={(webOpenMode) => updateSettings({ webOpenMode })}
            />
          ) : null}
          {section === "layout" ? (
            <LayoutSettings
              orientation={settings.tabBarOrientation}
              position={settings.tabBarPosition}
              showFileIcons={settings.showFileIcons}
              onOrientation={(orientation) =>
                updateSettings({
                  tabBarOrientation: orientation,
                  tabBarPosition: orientation === "horizontal" ? "top" : "left",
                })
              }
              onPosition={(tabBarPosition) => updateSettings({ tabBarPosition })}
              onShowFileIcons={(showFileIcons) => updateSettings({ showFileIcons })}
            />
          ) : null}
          {section === "agents" ? (
            <AgentSettings
              binaryStatus={binaryStatus}
              commands={settings.agentCommands}
              installCommands={settings.agentInstallCommands}
              onCommand={updateAgentCommand}
              onInstallCommand={(agent, command) =>
                updateSettings({
                  agentInstallCommands: {
                    ...settings.agentInstallCommands,
                    [agent]: command,
                  },
                })
              }
              onInstall={(agent, command) => void runInstall(agent, command)}
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
  customColorScheme: CustomColorScheme;
  uiFontFamily: string;
  terminalFontFamily: string;
  editorFontFamily: string;
  fontFamilies: string[];
  uiFontSize: number;
  terminalFontSize: number;
  editorFontSize: number;
  diffViewMode: DiffViewMode;
  webOpenMode: WebOpenMode;
  onTheme: (theme: ThemeMode) => void;
  onCustomColorScheme: (scheme: CustomColorScheme) => void;
  onUiFontFamily: (fontFamily: string) => void;
  onTerminalFontFamily: (fontFamily: string) => void;
  onEditorFontFamily: (fontFamily: string) => void;
  onUiFontSize: (fontSize: number) => void;
  onTerminalFontSize: (fontSize: number) => void;
  onEditorFontSize: (fontSize: number) => void;
  onDiffViewMode: (mode: DiffViewMode) => void;
  onWebOpenMode: (mode: WebOpenMode) => void;
}

function GeneralSettings({
  theme,
  customColorScheme,
  uiFontFamily,
  terminalFontFamily,
  editorFontFamily,
  fontFamilies,
  uiFontSize,
  terminalFontSize,
  editorFontSize,
  diffViewMode,
  webOpenMode,
  onTheme,
  onCustomColorScheme,
  onUiFontFamily,
  onTerminalFontFamily,
  onEditorFontFamily,
  onUiFontSize,
  onTerminalFontSize,
  onEditorFontSize,
  onDiffViewMode,
  onWebOpenMode,
}: GeneralSettingsProps) {
  function updateCustomLabel(label: string) {
    onCustomColorScheme({ ...customColorScheme, label });
  }

  function updateCustomColor(key: ColorSchemeColorKey, value: string) {
    onCustomColorScheme({
      ...customColorScheme,
      colors: {
        ...customColorScheme.colors,
        [key]: value,
      },
    });
  }

  return (
    <section className="settings-section" aria-label="General settings">
      <div className="settings-row">
        <label htmlFor="settings-color-scheme">Color scheme</label>
        <select
          id="settings-color-scheme"
          className="settings-select"
          value={theme}
          onChange={(event) => onTheme(event.target.value as ThemeMode)}
        >
          {COLOR_SCHEME_OPTIONS.map((option) => (
            <option key={option.id} value={option.id}>
              {option.label}
            </option>
          ))}
        </select>
      </div>
      {theme === "custom" ? (
        <div className="custom-scheme-editor">
          <label className="settings-row">
            <span>Custom name</span>
            <input
              className="settings-input"
              value={customColorScheme.label}
              onChange={(event) => updateCustomLabel(event.target.value)}
            />
          </label>
          <div className="custom-color-grid" aria-label="Custom color settings">
            {COLOR_SCHEME_COLOR_KEYS.map((key) => (
              <label className="color-setting-row" key={key}>
                <span>{COLOR_SCHEME_COLOR_LABELS[key]}</span>
                <span className="color-setting-control">
                  <input
                    className="color-picker"
                    type="color"
                    aria-label={`${COLOR_SCHEME_COLOR_LABELS[key]} color`}
                    value={customColorScheme.colors[key]}
                    onChange={(event) => updateCustomColor(key, event.target.value)}
                  />
                  <code>{customColorScheme.colors[key]}</code>
                </span>
              </label>
            ))}
          </div>
        </div>
      ) : null}
      <FontFamilyControl
        label="UI font"
        value={uiFontFamily}
        families={fontFamilies}
        onChange={onUiFontFamily}
      />
      <FontSizeControl
        label="UI font size"
        value={uiFontSize}
        onChange={onUiFontSize}
      />
      <FontFamilyControl
        label="Terminal font"
        value={terminalFontFamily}
        families={fontFamilies}
        onChange={onTerminalFontFamily}
      />
      <FontSizeControl
        label="Terminal font size"
        value={terminalFontSize}
        onChange={onTerminalFontSize}
      />
      <FontFamilyControl
        label="File content font"
        value={editorFontFamily}
        families={fontFamilies}
        onChange={onEditorFontFamily}
      />
      <FontSizeControl
        label="File content font size"
        value={editorFontSize}
        onChange={onEditorFontSize}
      />
      <label className="settings-row">
        <span>Diff view</span>
        <select
          className="settings-select"
          aria-label="Diff view"
          value={diffViewMode}
          onChange={(event) => onDiffViewMode(event.target.value as DiffViewMode)}
        >
          <option value="side-by-side">Side by side</option>
          <option value="unified">Unified</option>
        </select>
      </label>
      <label className="settings-row">
        <span>Web links</span>
        <select
          className="settings-select"
          aria-label="Web links"
          value={webOpenMode}
          onChange={(event) => onWebOpenMode(event.target.value as WebOpenMode)}
        >
          <option value="ask">Ask before opening in Onibi</option>
          <option value="in-app">Open in Onibi</option>
          <option value="off">Open externally</option>
        </select>
      </label>
    </section>
  );
}

interface FontFamilyControlProps {
  label: string;
  value: string;
  families: string[];
  onChange: (fontFamily: string) => void;
}

function FontFamilyControl({
  label,
  value,
  families,
  onChange,
}: FontFamilyControlProps) {
  const selectedFontIsInstalled = families.includes(value);
  return (
    <label className="settings-row">
      <span>{label}</span>
      <select
        className="settings-select"
        aria-label={`${label} installed font`}
        value={value}
        onChange={(event) => onChange(event.target.value)}
      >
        {!selectedFontIsInstalled ? (
          <option value={value}>{value || "Custom"}</option>
        ) : null}
        {families.map((family) => (
          <option key={family} value={family}>
            {family}
          </option>
        ))}
      </select>
    </label>
  );
}

function FontSizeControl({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (fontSize: number) => void;
}) {
  return (
    <label className="settings-row">
      <span>{label}</span>
      <input
        className="settings-input"
        aria-label={label}
        type="number"
        min={10}
        max={28}
        value={value}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </label>
  );
}

interface LayoutSettingsProps {
  orientation: TabBarOrientation;
  position: TabBarPosition;
  showFileIcons: boolean;
  onOrientation: (orientation: TabBarOrientation) => void;
  onPosition: (position: TabBarPosition) => void;
  onShowFileIcons: (showFileIcons: boolean) => void;
}

function LayoutSettings({
  orientation,
  position,
  showFileIcons,
  onOrientation,
  onPosition,
  onShowFileIcons,
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
      <label className="settings-row">
        <span>File tree icons</span>
        <span className="settings-check-row">
          <input
            type="checkbox"
            aria-label="Show file icons"
            checked={showFileIcons}
            onChange={(event) => onShowFileIcons(event.target.checked)}
          />
          Show file icons
        </span>
      </label>
    </section>
  );
}

interface AgentSettingsProps {
  commands: Record<AgentKind, string>;
  installCommands: Partial<Record<AgentKind, string>>;
  binaryStatus: BinaryStatus;
  onCommand: (agent: AgentKind, command: string) => void;
  onInstallCommand: (agent: AgentKind, command: string) => void;
  onInstall: (agent: AgentKind, command: string) => void;
}

function AgentSettings({
  commands,
  installCommands,
  binaryStatus,
  onCommand,
  onInstallCommand,
  onInstall,
}: AgentSettingsProps) {
  return (
    <section className="settings-section" aria-label="Agent settings">
      {AGENT_KINDS.map((agent) => {
        const status = agent === "shell" ? "system shell" : binaryStatus[agent] ?? "missing";
        const installCommand =
          installCommands[agent] ?? DEFAULT_AGENT_INSTALL_COMMANDS[agent] ?? "";
        return (
          <div className="agent-command-row" key={agent}>
            <span>{AGENT_LABELS[agent]}</span>
            <input
              className="settings-input"
              aria-label={`${AGENT_LABELS[agent]} launch command`}
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
            {agent !== "shell" ? (
              <>
                <span className="settings-note">Install</span>
                <input
                  className="settings-input"
                  aria-label={`${AGENT_LABELS[agent]} install command`}
                  value={installCommand}
                  onChange={(event) => onInstallCommand(agent, event.target.value)}
                />
                <button
                  type="button"
                  className="text-button"
                  disabled={!installCommand.trim()}
                  onClick={() => onInstall(agent, installCommand)}
                >
                  Run
                </button>
              </>
            ) : null}
          </div>
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
