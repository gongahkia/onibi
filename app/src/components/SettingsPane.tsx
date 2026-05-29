import { useEffect, useRef, useState } from "react";
import {
  AGENT_KINDS,
  AGENT_LABELS,
  COLOR_SCHEME_COLOR_KEYS,
  COLOR_SCHEME_COLOR_LABELS,
  COLOR_SCHEME_OPTIONS,
  DEFAULT_AGENT_COMMANDS,
  DEFAULT_AGENT_INSTALL_COMMANDS,
  DEFAULT_TERMINAL_PROFILES,
  DEFAULT_TERMINAL_TRIGGERS,
  type AgentKind,
  type ColorSchemeColorKey,
  type ColorSchemeColors,
  type CustomColorScheme,
  type DiffViewMode,
  type TabBarOrientation,
  type TabBarPosition,
  type TerminalConfigCandidate,
  type TerminalConfigImport,
  type TerminalConfigSource,
  type TerminalKeybinding,
  type TerminalProfile,
  type TerminalTrigger,
  type ThemeMode,
  type WebOpenMode,
  applyOnibiConfig,
  detectTerminalConfigImports,
  listLocalFontFamilies,
  parseOnibiConfigJson,
  parseTerminalConfigImport,
  resolveAgentBinary,
  serializeOnibiConfig,
  useSessionStore,
  workspaceFromPath,
} from "../lib/sessions";
import { ptySpawn, shellPath } from "../lib/tauri-bridge";

export interface SettingsPaneProps {
  open: boolean;
  onClose: () => void;
}

type SettingsSection =
  | "general"
  | "layout"
  | "agents"
  | "profiles"
  | "shell-integration"
  | "workspaces"
  | "config-json"
  | "import-config";
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
  const [configJson, setConfigJson] = useState("");
  const [configStatus, setConfigStatus] = useState<string | null>(null);
  const [terminalImports, setTerminalImports] = useState<TerminalConfigImport[]>([]);
  const [terminalImportError, setTerminalImportError] = useState<string | null>(null);
  const [detectingTerminalConfigs, setDetectingTerminalConfigs] = useState(false);

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

  useEffect(() => {
    if (open) {
      setConfigJson(serializeOnibiConfig());
      setConfigStatus(null);
    }
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

  async function refreshTerminalConfigImports() {
    setDetectingTerminalConfigs(true);
    setTerminalImportError(null);
    try {
      setTerminalImports(await detectTerminalConfigImports());
    } catch (caught) {
      setTerminalImports([]);
      setTerminalImportError(caught instanceof Error ? caught.message : String(caught));
    } finally {
      setDetectingTerminalConfigs(false);
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
            {(
              [
                "general",
                "layout",
                "agents",
                "profiles",
                "shell-integration",
                "workspaces",
                "config-json",
                "import-config",
              ] as const
            ).map((item) => (
              <button
                key={item}
                type="button"
                className={`text-button ${section === item ? "primary" : ""}`}
                onClick={() => {
                  setSection(item);
                  if (item === "config-json") {
                    setConfigJson(serializeOnibiConfig());
                    setConfigStatus(null);
                  }
                  if (item === "import-config" && terminalImports.length === 0) {
                    void refreshTerminalConfigImports();
                  }
                }}
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
              terminalScrollbackLines={settings.terminalScrollbackLines}
              terminalShellIntegration={settings.terminalShellIntegration}
              terminalConfirmClose={settings.terminalConfirmClose}
              terminalTriggers={settings.terminalTriggers}
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
              onTerminalScrollbackLines={(terminalScrollbackLines) =>
                updateSettings({ terminalScrollbackLines })
              }
              onTerminalShellIntegration={(terminalShellIntegration) =>
                updateSettings({ terminalShellIntegration })
              }
              onTerminalConfirmClose={(terminalConfirmClose) =>
                updateSettings({ terminalConfirmClose })
              }
              onTerminalTriggers={(terminalTriggers) =>
                updateSettings({ terminalTriggers })
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
          {section === "profiles" ? (
            <ProfileSettings
              profiles={settings.terminalProfiles}
              defaultProfileId={settings.defaultTerminalProfileId}
              onProfiles={(terminalProfiles) => updateSettings({ terminalProfiles })}
              onDefaultProfile={(defaultTerminalProfileId) =>
                updateSettings({ defaultTerminalProfileId })
              }
            />
          ) : null}
          {section === "shell-integration" ? (
            <ShellIntegrationSettings
              enabled={settings.terminalShellIntegration}
              sessions={sessions}
              onEnabled={(terminalShellIntegration) =>
                updateSettings({ terminalShellIntegration })
              }
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
          {section === "config-json" ? (
            <ConfigJsonSettings
              value={configJson}
              status={configStatus}
              onValue={setConfigJson}
              onRefresh={() => {
                setConfigJson(serializeOnibiConfig());
                setConfigStatus("Refreshed from current settings.");
              }}
              onApply={() => {
                try {
                  const config = parseOnibiConfigJson(configJson);
                  applyOnibiConfig(config);
                  setConfigJson(serializeOnibiConfig());
                  setConfigStatus("Applied config.json.");
                } catch (caught) {
                  setConfigStatus(caught instanceof Error ? caught.message : String(caught));
                }
              }}
            />
          ) : null}
          {section === "import-config" ? (
            <ImportConfigSettings
              imports={terminalImports}
              loading={detectingTerminalConfigs}
              error={terminalImportError}
              onRefresh={() => void refreshTerminalConfigImports()}
              onAddImport={(item) =>
                setTerminalImports((state) => [
                  item,
                  ...state.filter((existing) => existing.id !== item.id),
                ])
              }
              onApply={(item, options) => {
                const nextCustomColors: ColorSchemeColors = {
                  ...settings.customColorScheme.colors,
                  ...item.colors,
                };
                updateSettings({
                  theme: options.colors ? "custom" : settings.theme,
                  customColorScheme: options.colors
                    ? {
                        label: item.colorSchemeName ?? item.label,
                        colors: nextCustomColors,
                      }
                    : settings.customColorScheme,
                  terminalFontFamily:
                    options.fontFamily && item.fontFamily
                      ? item.fontFamily
                      : settings.terminalFontFamily,
                  editorFontFamily:
                    options.fontFamily && item.fontFamily
                      ? item.fontFamily
                      : settings.editorFontFamily,
                  terminalFontSize:
                    options.fontSize && item.fontSize
                      ? item.fontSize
                      : settings.terminalFontSize,
                  terminalKeybindings: options.keybindings
                    ? mergeTerminalKeybindings(
                        settings.terminalKeybindings,
                        item.keybindings,
                      )
                    : settings.terminalKeybindings,
                  terminalShaderPaths: options.shaders
                    ? mergeStringList(settings.terminalShaderPaths, item.shaderPaths)
                    : settings.terminalShaderPaths,
                });
              }}
            />
          ) : null}
        </div>
      </section>
    </div>
  );
}

function labelFor(value: string): string {
  if (value === "config-json") {
    return "config.json";
  }
  if (value === "import-config") {
    return "Import config from ...";
  }
  if (value === "shell-integration") {
    return "Shell integration";
  }
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
  terminalScrollbackLines: number;
  terminalShellIntegration: boolean;
  terminalConfirmClose: boolean;
  terminalTriggers: TerminalTrigger[];
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
  onTerminalScrollbackLines: (lines: number) => void;
  onTerminalShellIntegration: (enabled: boolean) => void;
  onTerminalConfirmClose: (enabled: boolean) => void;
  onTerminalTriggers: (triggers: TerminalTrigger[]) => void;
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
  terminalScrollbackLines,
  terminalShellIntegration,
  terminalConfirmClose,
  terminalTriggers,
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
  onTerminalScrollbackLines,
  onTerminalShellIntegration,
  onTerminalConfirmClose,
  onTerminalTriggers,
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
      <ScrollbackControl
        value={terminalScrollbackLines}
        onChange={onTerminalScrollbackLines}
      />
      <label className="settings-row">
        <span>Shell integration</span>
        <span className="settings-check-row">
          <input
            type="checkbox"
            aria-label="Enable shell completions and autosuggestions"
            checked={terminalShellIntegration}
            onChange={(event) => onTerminalShellIntegration(event.target.checked)}
          />
          Track current directory, prompt boundaries, and shell completions
        </span>
      </label>
      <label className="settings-row">
        <span>Close confirmation</span>
        <span className="settings-check-row">
          <input
            type="checkbox"
            aria-label="Confirm before closing running terminals"
            checked={terminalConfirmClose}
            onChange={(event) => onTerminalConfirmClose(event.target.checked)}
          />
          Confirm before closing running terminals
        </span>
      </label>
      <TerminalTriggersControl
        triggers={terminalTriggers}
        onChange={onTerminalTriggers}
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

function ScrollbackControl({
  value,
  onChange,
}: {
  value: number;
  onChange: (lines: number) => void;
}) {
  return (
    <label className="settings-row">
      <span>Terminal scrollback</span>
      <span className="settings-stacked-control">
        <input
          className="settings-input"
          aria-label="Terminal scrollback lines"
          type="number"
          min={0}
          max={1000000}
          step={1000}
          value={value}
          onChange={(event) => onChange(Number(event.target.value))}
        />
        <span className="settings-note">0 keeps up to 1,000,000 lines.</span>
      </span>
    </label>
  );
}

function TerminalTriggersControl({
  triggers,
  onChange,
}: {
  triggers: TerminalTrigger[];
  onChange: (triggers: TerminalTrigger[]) => void;
}) {
  function updateTrigger(id: string, patch: Partial<TerminalTrigger>) {
    onChange(
      triggers.map((trigger) =>
        trigger.id === id ? { ...trigger, ...patch } : trigger,
      ),
    );
  }

  return (
    <div className="settings-row settings-row-top">
      <span>Terminal triggers</span>
      <div className="trigger-list">
        {triggers.map((trigger) => (
          <label className="trigger-row" key={trigger.id}>
            <input
              type="checkbox"
              aria-label={`${trigger.label} trigger`}
              checked={trigger.enabled}
              onChange={(event) =>
                updateTrigger(trigger.id, { enabled: event.target.checked })
              }
            />
            <span className="trigger-row-main">
              <strong>{trigger.label}</strong>
              <code>{trigger.pattern}</code>
            </span>
          </label>
        ))}
        <button
          type="button"
          className="text-button"
          onClick={() => onChange(DEFAULT_TERMINAL_TRIGGERS)}
        >
          Reset trigger presets
        </button>
      </div>
    </div>
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

function envToText(env: Array<[string, string]>): string {
  return env.map(([key, value]) => `${key}=${value}`).join("\n");
}

function textToEnv(value: string): Array<[string, string]> {
  return value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line): [string, string] => {
      const separator = line.indexOf("=");
      return separator < 0
        ? [line, ""]
        : [line.slice(0, separator).trim(), line.slice(separator + 1)];
    })
    .filter(([key]) => key.length > 0);
}

function makeProfile(): TerminalProfile {
  return {
    ...DEFAULT_TERMINAL_PROFILES[0],
    id: `profile:${crypto.randomUUID?.() ?? Date.now().toString(36)}`,
    name: "New profile",
  };
}

function ProfileSettings({
  profiles,
  defaultProfileId,
  onProfiles,
  onDefaultProfile,
}: {
  profiles: TerminalProfile[];
  defaultProfileId: string | null;
  onProfiles: (profiles: TerminalProfile[]) => void;
  onDefaultProfile: (profileId: string | null) => void;
}) {
  const [selectedId, setSelectedId] = useState(
    defaultProfileId ?? profiles[0]?.id ?? "",
  );
  const selected = profiles.find((profile) => profile.id === selectedId) ?? profiles[0];

  useEffect(() => {
    if (!selected && profiles[0]) {
      setSelectedId(profiles[0].id);
    }
  }, [profiles, selected]);

  function updateProfile(id: string, patch: Partial<TerminalProfile>) {
    onProfiles(
      profiles.map((profile) =>
        profile.id === id ? { ...profile, ...patch } : profile,
      ),
    );
  }

  function addProfile() {
    const profile = makeProfile();
    onProfiles([profile, ...profiles]);
    setSelectedId(profile.id);
  }

  function deleteProfile() {
    if (!selected || profiles.length <= 1) {
      return;
    }
    const nextProfiles = profiles.filter((profile) => profile.id !== selected.id);
    onProfiles(nextProfiles);
    if (defaultProfileId === selected.id) {
      onDefaultProfile(nextProfiles[0]?.id ?? null);
    }
    setSelectedId(nextProfiles[0]?.id ?? "");
  }

  if (!selected) {
    return (
      <section className="settings-section" aria-label="Profile settings">
        <button type="button" className="text-button primary" onClick={addProfile}>
          Add profile
        </button>
      </section>
    );
  }

  return (
    <section className="settings-section" aria-label="Profile settings">
      <div className="profile-settings-grid">
        <div className="profile-list" role="listbox" aria-label="Profiles">
          {profiles.map((profile) => (
            <button
              key={profile.id}
              type="button"
              className={`profile-row ${profile.id === selected.id ? "active" : ""}`}
              onClick={() => setSelectedId(profile.id)}
            >
              <strong>{profile.name}</strong>
              <span>{AGENT_LABELS[profile.agent]}</span>
            </button>
          ))}
          <button type="button" className="text-button" onClick={addProfile}>
            Add profile
          </button>
        </div>
        <div className="profile-editor">
          <label className="settings-row">
            <span>Name</span>
            <input
              className="settings-input"
              aria-label="Profile name"
              value={selected.name}
              onChange={(event) =>
                updateProfile(selected.id, { name: event.target.value })
              }
            />
          </label>
          <label className="settings-row">
            <span>Agent</span>
            <select
              className="settings-select"
              aria-label="Profile agent"
              value={selected.agent}
              onChange={(event) =>
                updateProfile(selected.id, {
                  agent: event.target.value as AgentKind,
                  command:
                    event.target.value === "shell"
                      ? ""
                      : DEFAULT_AGENT_COMMANDS[event.target.value as AgentKind],
                })
              }
            >
              {AGENT_KINDS.map((agent) => (
                <option key={agent} value={agent}>
                  {AGENT_LABELS[agent]}
                </option>
              ))}
            </select>
          </label>
          <label className="settings-row">
            <span>Command</span>
            <input
              className="settings-input"
              aria-label="Profile command"
              value={selected.command}
              placeholder={
                selected.agent === "shell"
                  ? "System shell"
                  : DEFAULT_AGENT_COMMANDS[selected.agent]
              }
              onChange={(event) =>
                updateProfile(selected.id, { command: event.target.value })
              }
            />
          </label>
          <label className="settings-row settings-row-top">
            <span>Args</span>
            <textarea
              className="settings-textarea settings-small-textarea"
              aria-label="Profile args"
              value={selected.args.join("\n")}
              onChange={(event) =>
                updateProfile(selected.id, {
                  args: event.target.value.split(/\r?\n/).filter(Boolean),
                })
              }
            />
          </label>
          <label className="settings-row settings-row-top">
            <span>Env</span>
            <textarea
              className="settings-textarea settings-small-textarea"
              aria-label="Profile environment"
              value={envToText(selected.env)}
              onChange={(event) =>
                updateProfile(selected.id, { env: textToEnv(event.target.value) })
              }
            />
          </label>
          <label className="settings-row">
            <span>Cwd policy</span>
            <select
              className="settings-select"
              aria-label="Profile cwd policy"
              value={selected.cwdPolicy}
              onChange={(event) =>
                updateProfile(selected.id, {
                  cwdPolicy: event.target.value as TerminalProfile["cwdPolicy"],
                })
              }
            >
              <option value="active-pane">Active pane cwd</option>
              <option value="workspace">Workspace root</option>
              <option value="custom">Custom path</option>
            </select>
          </label>
          {selected.cwdPolicy === "custom" ? (
            <label className="settings-row">
              <span>Custom cwd</span>
              <input
                className="settings-input"
                aria-label="Profile custom cwd"
                value={selected.customCwd}
                onChange={(event) =>
                  updateProfile(selected.id, { customCwd: event.target.value })
                }
              />
            </label>
          ) : null}
          <label className="settings-row settings-row-top">
            <span>Initial prompt</span>
            <textarea
              className="settings-textarea settings-small-textarea"
              aria-label="Profile initial prompt"
              value={selected.initialPrompt}
              onChange={(event) =>
                updateProfile(selected.id, { initialPrompt: event.target.value })
              }
            />
          </label>
          <label className="settings-row">
            <span>Default</span>
            <span className="settings-check-row">
              <input
                type="checkbox"
                aria-label="Use as default profile"
                checked={defaultProfileId === selected.id}
                onChange={(event) =>
                  onDefaultProfile(event.target.checked ? selected.id : null)
                }
              />
              Use as default profile
            </span>
          </label>
          <div className="settings-inline-actions">
            <button
              type="button"
              className="text-button danger"
              disabled={profiles.length <= 1}
              onClick={deleteProfile}
            >
              Delete profile
            </button>
            <button
              type="button"
              className="text-button"
              onClick={() => {
                onProfiles(DEFAULT_TERMINAL_PROFILES);
                onDefaultProfile("profile:shell");
                setSelectedId("profile:shell");
              }}
            >
              Reset profiles
            </button>
          </div>
        </div>
      </div>
    </section>
  );
}

function ShellIntegrationSettings({
  enabled,
  sessions,
  onEnabled,
}: {
  enabled: boolean;
  sessions: Array<{
    id: string;
    title: string;
    cwd?: string;
    lastExitCode?: number | null;
    shellPromptMarkerSeen?: boolean;
    lastCommand?: { command: string; exitCode?: number | null } | null;
    preview?: { url: string } | null;
  }>;
  onEnabled: (enabled: boolean) => void;
}) {
  const snippet = [
    "ONIBI_SHELL_INTEGRATION=1",
    "Supported shells: zsh, bash, fish",
    "Runtime markers: OSC 7 for cwd and OSC 133 for prompt/exit status",
  ].join("\n");
  return (
    <section className="settings-section" aria-label="Shell integration settings">
      <label className="settings-row">
        <span>Shell integration</span>
        <span className="settings-check-row">
          <input
            type="checkbox"
            aria-label="Enable shell integration"
            checked={enabled}
            onChange={(event) => onEnabled(event.target.checked)}
          />
          Enable shell integration for new shell sessions
        </span>
      </label>
      <div className="settings-row settings-row-top">
        <span>Status</span>
        <div className="shell-status-list">
          {sessions.length === 0 ? (
            <div className="settings-note">No active sessions.</div>
          ) : (
            sessions.map((session) => (
              <div className="shell-status-row" key={session.id}>
                <strong>{session.title}</strong>
                <span>{session.cwd ? `cwd: ${session.cwd}` : "cwd marker not seen"}</span>
                <span>
                  {session.lastExitCode === null || session.lastExitCode === undefined
                    ? "exit marker not seen"
                    : `last exit: ${session.lastExitCode}`}
                </span>
                <span>
                  {session.shellPromptMarkerSeen
                    ? "prompt markers seen"
                    : "prompt marker not seen"}
                </span>
                {session.lastCommand?.command ? (
                  <span>{`last command: ${session.lastCommand.command}`}</span>
                ) : null}
                {session.preview ? <span>{`preview: ${session.preview.url}`}</span> : null}
              </div>
            ))
          )}
        </div>
      </div>
      <div className="settings-row settings-row-top">
        <span>Debug snippet</span>
        <div className="settings-stacked-control">
          <pre className="settings-code-block">{snippet}</pre>
          <button
            type="button"
            className="text-button"
            onClick={() => void navigator.clipboard?.writeText(snippet)}
          >
            Copy snippet
          </button>
        </div>
      </div>
    </section>
  );
}

function ConfigJsonSettings({
  value,
  status,
  onValue,
  onRefresh,
  onApply,
}: {
  value: string;
  status: string | null;
  onValue: (value: string) => void;
  onRefresh: () => void;
  onApply: () => void;
}) {
  const inputRef = useRef<HTMLInputElement | null>(null);

  function exportConfig() {
    const blob = new Blob([value], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "onibi.config.json";
    link.click();
    URL.revokeObjectURL(url);
  }

  async function importConfig(file: File | undefined) {
    if (!file) {
      return;
    }
    onValue(await file.text());
  }

  return (
    <section className="settings-section" aria-label="config.json settings">
      <div className="config-json-actions">
        <button type="button" className="text-button" onClick={onRefresh}>
          Refresh
        </button>
        <button type="button" className="text-button primary" onClick={onApply}>
          Apply JSON
        </button>
        <button type="button" className="text-button" onClick={exportConfig}>
          Export
        </button>
        <button
          type="button"
          className="text-button"
          onClick={() => inputRef.current?.click()}
        >
          Import
        </button>
        <input
          ref={inputRef}
          type="file"
          accept="application/json,.json"
          className="visually-hidden"
          onChange={(event) => void importConfig(event.target.files?.[0])}
        />
      </div>
      <textarea
        className="settings-textarea config-json-editor"
        aria-label="Onibi config JSON"
        spellCheck={false}
        value={value}
        onChange={(event) => onValue(event.target.value)}
      />
      {status ? <div className="settings-note">{status}</div> : null}
    </section>
  );
}

interface ImportOptions {
  colors: boolean;
  fontFamily: boolean;
  fontSize: boolean;
  keybindings: boolean;
  shaders: boolean;
}

function mergeTerminalKeybindings(
  current: TerminalKeybinding[],
  imported: TerminalKeybinding[],
): TerminalKeybinding[] {
  const existing = new Set(current.map((binding) => `${binding.keys}:${binding.action}`));
  return [
    ...current,
    ...imported.filter((binding) => {
      const id = `${binding.keys}:${binding.action}`;
      if (existing.has(id)) {
        return false;
      }
      existing.add(id);
      return true;
    }),
  ];
}

function mergeStringList(current: string[], imported: string[]): string[] {
  const existing = new Set(current);
  return [
    ...current,
    ...imported.filter((item) => {
      if (existing.has(item)) {
        return false;
      }
      existing.add(item);
      return true;
    }),
  ];
}

function sourceForConfigFile(file: File): TerminalConfigSource {
  const lower = file.name.toLowerCase();
  if (lower.endsWith(".itermcolors")) {
    return "iterm2";
  }
  if (lower.includes("iterm")) {
    return "iterm2";
  }
  if (lower.includes("terminal")) {
    return "terminal-app";
  }
  if (lower.includes("alacritty")) {
    return "alacritty";
  }
  if (lower.includes("alacrity")) {
    return "alacritty";
  }
  if (lower.includes("wezterm") || lower.endsWith(".lua")) {
    return "wezterm";
  }
  if (lower.includes("kitty")) {
    return "kitty";
  }
  if (lower.includes("tmux")) {
    return "tmux";
  }
  if (lower.includes("zellij")) {
    return "zellij";
  }
  if (lower.includes("warp")) {
    return "warp";
  }
  if (lower.includes("muxy")) {
    return "muxy";
  }
  if (lower.includes("cmux")) {
    return "cmux";
  }
  if (lower.includes("settings.json")) {
    return "windows-terminal";
  }
  return "ghostty";
}

function ImportConfigSettings({
  imports,
  loading,
  error,
  onRefresh,
  onAddImport,
  onApply,
}: {
  imports: TerminalConfigImport[];
  loading: boolean;
  error: string | null;
  onRefresh: () => void;
  onAddImport: (item: TerminalConfigImport) => void;
  onApply: (item: TerminalConfigImport, options: ImportOptions) => void;
}) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const [optionsById, setOptionsById] = useState<Record<string, ImportOptions>>({});

  async function importFile(file: File | undefined) {
    if (!file) {
      return;
    }
    const candidate: TerminalConfigCandidate = {
      source: sourceForConfigFile(file),
      label: file.name,
      path: file.name,
      content: await file.text(),
    };
    const parsed = parseTerminalConfigImport(candidate);
    if (parsed.importedFields.length > 0) {
      onAddImport(parsed);
    }
  }

  function optionsFor(item: TerminalConfigImport): ImportOptions {
    return (
      optionsById[item.id] ?? {
        colors: Object.keys(item.colors).length > 0,
        fontFamily: Boolean(item.fontFamily),
        fontSize: Boolean(item.fontSize),
        keybindings: item.keybindings.length > 0,
        shaders: item.shaderPaths.length > 0,
      }
    );
  }

  function updateOption(
    item: TerminalConfigImport,
    key: keyof ImportOptions,
    value: boolean,
  ) {
    setOptionsById((state) => ({
      ...state,
      [item.id]: { ...optionsFor(item), [key]: value },
    }));
  }

  return (
    <section className="settings-section" aria-label="Import terminal config">
      <div className="config-json-actions">
        <button type="button" className="text-button" onClick={onRefresh}>
          {loading ? "Detecting" : "Detect configs"}
        </button>
        <button
          type="button"
          className="text-button"
          onClick={() => inputRef.current?.click()}
        >
          Import config file
        </button>
        <input
          ref={inputRef}
          type="file"
          accept=".conf,.config,.itermcolors,.json,.kdl,.lua,.plist,.toml,.yaml,.yml"
          className="visually-hidden"
          onChange={(event) => void importFile(event.target.files?.[0])}
        />
      </div>
      {error ? <div className="editor-error">{error}</div> : null}
      {!loading && imports.length === 0 ? (
        <div className="settings-note">No importable terminal configs found.</div>
      ) : null}
      {imports.map((item) => {
        const options = optionsFor(item);
        return (
          <div className="terminal-import-card" key={item.id}>
            <div>
              <strong>{item.label}</strong>
              <div className="settings-note">{item.path}</div>
              {item.colorSchemeName ? (
                <div className="settings-note">Theme: {item.colorSchemeName}</div>
              ) : null}
              {item.importedFields.length > 0 ? (
                <div className="settings-note">
                  Importable: {item.importedFields.join(", ")}
                </div>
              ) : null}
            </div>
            <label className="settings-check-row">
              <input
                type="checkbox"
                checked={options.colors}
                disabled={Object.keys(item.colors).length === 0}
                onChange={(event) =>
                  updateOption(item, "colors", event.target.checked)
                }
              />
              Colors
            </label>
            <label className="settings-check-row">
              <input
                type="checkbox"
                checked={options.fontFamily}
                disabled={!item.fontFamily}
                onChange={(event) =>
                  updateOption(item, "fontFamily", event.target.checked)
                }
              />
              Font family{item.fontFamily ? `: ${item.fontFamily}` : ""}
            </label>
            <label className="settings-check-row">
              <input
                type="checkbox"
                checked={options.fontSize}
                disabled={!item.fontSize}
                onChange={(event) =>
                  updateOption(item, "fontSize", event.target.checked)
                }
              />
              Font size{item.fontSize ? `: ${item.fontSize}` : ""}
            </label>
            <label className="settings-check-row">
              <input
                type="checkbox"
                checked={options.keybindings}
                disabled={item.keybindings.length === 0}
                onChange={(event) =>
                  updateOption(item, "keybindings", event.target.checked)
                }
              />
              Keybindings
              {item.keybindings.length > 0 ? `: ${item.keybindings.length}` : ""}
            </label>
            <label className="settings-check-row">
              <input
                type="checkbox"
                checked={options.shaders}
                disabled={item.shaderPaths.length === 0}
                onChange={(event) => updateOption(item, "shaders", event.target.checked)}
              />
              Shaders{item.shaderPaths.length > 0 ? `: ${item.shaderPaths.length}` : ""}
            </label>
            <button
              type="button"
              className="text-button primary"
              onClick={() => onApply(item, options)}
            >
              Apply selected
            </button>
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
