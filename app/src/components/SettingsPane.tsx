import { useEffect, useRef, useState } from "react";
import {
  AGENT_KINDS,
  AGENT_LABELS,
  APP_KEYBINDING_ACTION_LABELS,
  COLOR_SCHEME_COLOR_KEYS,
  COLOR_SCHEME_COLOR_LABELS,
  COLOR_SCHEME_OPTIONS,
  DEFAULT_AGENT_COMMANDS,
  DEFAULT_AGENT_INSTALL_COMMANDS,
  DEFAULT_TERMINAL_TRIGGERS,
  type AgentKind,
  type AppKeybinding,
  type AppKeybindingAction,
  type ColorSchemeColorKey,
  type ColorSchemeColors,
  type CustomCommandKeybinding,
  type CustomColorScheme,
  type DiffViewMode,
  type EditorKeybindingMode,
  type NewPaneCwdMode,
  type TabBarOrientation,
  type TabBarPosition,
  type TerminalConfigCandidate,
  type TerminalConfigImport,
  type TerminalConfigSource,
  type TerminalKeybinding,
  type TerminalShellMode,
  type TerminalTrigger,
  type TerminalTriggerAction,
  type ThemeMode,
  type WebOpenMode,
  appKeybindingConflicts,
  applyOnibiConfig,
  detectTerminalConfigImports,
  displayKeyChord,
  listLocalFontFamilies,
  normalizeAppKeyChord,
  parseOnibiConfigText,
  parseTerminalConfigImport,
  readOnibiConfigTomlText,
  resolveAgentBinary,
  serializeOnibiConfigToml,
  useSessionStore,
  writeOnibiConfigTomlFile,
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
  | "workspaces"
  | "shell-integration"
  | "triggers"
  | "keybindings"
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
      setConfigJson(serializeOnibiConfigToml());
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
      agent: "shell",
      workspaceId: workspace.id,
      title: `Install ${AGENT_LABELS[agent]} · ${workspace.name}`,
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

  function selectSection(item: SettingsSection) {
    setSection(item);
    if (item === "config-json") {
      setConfigJson(serializeOnibiConfigToml());
      setConfigStatus(null);
    }
    if (item === "import-config" && terminalImports.length === 0) {
      void refreshTerminalConfigImports();
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
                "workspaces",
                "shell-integration",
                "triggers",
                "keybindings",
                "config-json",
                "import-config",
              ] as const
            ).map((item) => (
              <button
                key={item}
                type="button"
                className={`text-button ${section === item ? "primary" : ""}`}
                onClick={() => selectSection(item)}
              >
                {labelFor(item)}
              </button>
            ))}
          </nav>
          {section === "general" ? (
            <GeneralSettings
              theme={settings.theme}
              customColorScheme={settings.customColorScheme}
              defaultAgent={settings.defaultAgent}
              uiFontFamily={settings.uiFontFamily}
              terminalFontFamily={settings.terminalFontFamily}
              editorFontFamily={settings.editorFontFamily}
              fontFamilies={fontFamilies}
              uiFontSize={settings.uiFontSize}
              terminalFontSize={settings.terminalFontSize}
              terminalScrollbackLines={settings.terminalScrollbackLines}
              terminalConfirmClose={settings.terminalConfirmClose}
              terminalShellMode={settings.terminalShellMode}
              newPaneCwd={settings.newPaneCwd}
              editorFontSize={settings.editorFontSize}
              editorKeybindingMode={settings.editorKeybindingMode}
              editorVimRelativeLineNumbers={settings.editorVimRelativeLineNumbers}
              editorVimSystemClipboard={settings.editorVimSystemClipboard}
              editorEmacsSystemClipboard={settings.editorEmacsSystemClipboard}
              editorOpenLimit={settings.editorOpenLimit}
              closedBufferHistoryLimit={settings.closedBufferHistoryLimit}
              diffViewMode={settings.diffViewMode}
              webOpenMode={settings.webOpenMode}
              onTheme={(theme) => updateSettings({ theme })}
              onCustomColorScheme={(customColorScheme) =>
                updateSettings({ customColorScheme })
              }
              onDefaultAgent={(defaultAgent) => updateSettings({ defaultAgent })}
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
              onTerminalConfirmClose={(terminalConfirmClose) =>
                updateSettings({ terminalConfirmClose })
              }
              onTerminalShellMode={(terminalShellMode) =>
                updateSettings({ terminalShellMode })
              }
              onNewPaneCwd={(newPaneCwd) => updateSettings({ newPaneCwd })}
              onEditorFontSize={(editorFontSize) => updateSettings({ editorFontSize })}
              onEditorKeybindingMode={(editorKeybindingMode) =>
                updateSettings({ editorKeybindingMode })
              }
              onEditorVimRelativeLineNumbers={(editorVimRelativeLineNumbers) =>
                updateSettings({ editorVimRelativeLineNumbers })
              }
              onEditorVimSystemClipboard={(editorVimSystemClipboard) =>
                updateSettings({ editorVimSystemClipboard })
              }
              onEditorEmacsSystemClipboard={(editorEmacsSystemClipboard) =>
                updateSettings({ editorEmacsSystemClipboard })
              }
              onEditorOpenLimit={(editorOpenLimit) =>
                updateSettings({ editorOpenLimit })
              }
              onClosedBufferHistoryLimit={(closedBufferHistoryLimit) =>
                updateSettings({ closedBufferHistoryLimit })
              }
              onDiffViewMode={(diffViewMode) => updateSettings({ diffViewMode })}
              onWebOpenMode={(webOpenMode) => updateSettings({ webOpenMode })}
            />
          ) : null}
          {section === "layout" ? (
            <LayoutSettings
              orientation={settings.tabBarOrientation}
              position={settings.tabBarPosition}
              showFileIcons={settings.showFileIcons}
              showTerminalPaneAgentLabels={settings.showTerminalPaneAgentLabels}
              onOrientation={(orientation) =>
                updateSettings({
                  tabBarOrientation: orientation,
                  tabBarPosition: orientation === "horizontal" ? "top" : "left",
                })
              }
              onPosition={(tabBarPosition) => updateSettings({ tabBarPosition })}
              onShowFileIcons={(showFileIcons) => updateSettings({ showFileIcons })}
              onShowTerminalPaneAgentLabels={(showTerminalPaneAgentLabels) =>
                updateSettings({ showTerminalPaneAgentLabels })
              }
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
          {section === "shell-integration" ? (
            <ShellIntegrationSettings
              sessions={sessions}
              enabled={settings.terminalShellIntegration}
              onEnabled={(terminalShellIntegration) =>
                updateSettings({ terminalShellIntegration })
              }
            />
          ) : null}
          {section === "triggers" ? (
            <TriggersSettings
              triggers={settings.terminalTriggers}
              onTriggers={(terminalTriggers) => updateSettings({ terminalTriggers })}
            />
          ) : null}
          {section === "keybindings" ? (
            <KeybindingsSettings
              prefix={settings.keybindingPrefix}
              bindings={settings.appKeybindings}
              commandBindings={settings.customCommandKeybindings}
              onPrefix={(keybindingPrefix) => updateSettings({ keybindingPrefix })}
              onBindings={(appKeybindings) => updateSettings({ appKeybindings })}
              onCommandBindings={(customCommandKeybindings) =>
                updateSettings({ customCommandKeybindings })
              }
            />
          ) : null}
          {section === "config-json" ? (
            <ConfigJsonSettings
              value={configJson}
              status={configStatus}
              onValue={setConfigJson}
              onRefresh={() => {
                setConfigJson(serializeOnibiConfigToml());
                setConfigStatus("Refreshed from current settings.");
              }}
              onLoadFile={async () => {
                try {
                  const raw = await readOnibiConfigTomlText();
                  if (raw?.trim()) {
                    setConfigJson(raw);
                    setConfigStatus("Loaded ~/.config/onibi/config.toml.");
                  } else {
                    setConfigStatus("No ~/.config/onibi/config.toml found.");
                  }
                } catch (caught) {
                  setConfigStatus(caught instanceof Error ? caught.message : String(caught));
                }
              }}
              onSaveFile={async () => {
                try {
                  await writeOnibiConfigTomlFile(configJson);
                  setConfigStatus("Saved ~/.config/onibi/config.toml.");
                } catch (caught) {
                  setConfigStatus(caught instanceof Error ? caught.message : String(caught));
                }
              }}
              onApply={() => {
                try {
                  const config = parseOnibiConfigText(configJson);
                  applyOnibiConfig(config);
                  setConfigJson(serializeOnibiConfigToml());
                  setConfigStatus("Applied config.toml.");
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
    return "config.toml";
  }
  if (value === "import-config") {
    return "Import terminal settings";
  }
  if (value === "shell-integration") {
    return "Shell integration";
  }
  if (value === "triggers") {
    return "Triggers";
  }
  return value[0].toUpperCase() + value.slice(1);
}

interface GeneralSettingsProps {
  theme: ThemeMode;
  customColorScheme: CustomColorScheme;
  defaultAgent: AgentKind;
  uiFontFamily: string;
  terminalFontFamily: string;
  editorFontFamily: string;
  fontFamilies: string[];
  uiFontSize: number;
  terminalFontSize: number;
  terminalScrollbackLines: number;
  terminalConfirmClose: boolean;
  terminalShellMode: TerminalShellMode;
  newPaneCwd: NewPaneCwdMode;
  editorFontSize: number;
  editorKeybindingMode: EditorKeybindingMode;
  editorVimRelativeLineNumbers: boolean;
  editorVimSystemClipboard: boolean;
  editorEmacsSystemClipboard: boolean;
  editorOpenLimit: number;
  closedBufferHistoryLimit: number;
  diffViewMode: DiffViewMode;
  webOpenMode: WebOpenMode;
  onTheme: (theme: ThemeMode) => void;
  onCustomColorScheme: (scheme: CustomColorScheme) => void;
  onDefaultAgent: (agent: AgentKind) => void;
  onUiFontFamily: (fontFamily: string) => void;
  onTerminalFontFamily: (fontFamily: string) => void;
  onEditorFontFamily: (fontFamily: string) => void;
  onUiFontSize: (fontSize: number) => void;
  onTerminalFontSize: (fontSize: number) => void;
  onTerminalScrollbackLines: (lines: number) => void;
  onTerminalConfirmClose: (enabled: boolean) => void;
  onTerminalShellMode: (mode: TerminalShellMode) => void;
  onNewPaneCwd: (mode: NewPaneCwdMode) => void;
  onEditorFontSize: (fontSize: number) => void;
  onEditorKeybindingMode: (mode: EditorKeybindingMode) => void;
  onEditorVimRelativeLineNumbers: (enabled: boolean) => void;
  onEditorVimSystemClipboard: (enabled: boolean) => void;
  onEditorEmacsSystemClipboard: (enabled: boolean) => void;
  onEditorOpenLimit: (limit: number) => void;
  onClosedBufferHistoryLimit: (limit: number) => void;
  onDiffViewMode: (mode: DiffViewMode) => void;
  onWebOpenMode: (mode: WebOpenMode) => void;
}

function GeneralSettings({
  theme,
  customColorScheme,
  defaultAgent,
  uiFontFamily,
  terminalFontFamily,
  editorFontFamily,
  fontFamilies,
  uiFontSize,
  terminalFontSize,
  terminalScrollbackLines,
  terminalConfirmClose,
  terminalShellMode,
  newPaneCwd,
  editorFontSize,
  editorKeybindingMode,
  editorVimRelativeLineNumbers,
  editorVimSystemClipboard,
  editorEmacsSystemClipboard,
  editorOpenLimit,
  closedBufferHistoryLimit,
  diffViewMode,
  webOpenMode,
  onTheme,
  onCustomColorScheme,
  onDefaultAgent,
  onUiFontFamily,
  onTerminalFontFamily,
  onEditorFontFamily,
  onUiFontSize,
  onTerminalFontSize,
  onTerminalScrollbackLines,
  onTerminalConfirmClose,
  onTerminalShellMode,
  onNewPaneCwd,
  onEditorFontSize,
  onEditorKeybindingMode,
  onEditorVimRelativeLineNumbers,
  onEditorVimSystemClipboard,
  onEditorEmacsSystemClipboard,
  onEditorOpenLimit,
  onClosedBufferHistoryLimit,
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
      <label className="settings-row">
        <span>Default agent</span>
        <select
          className="settings-select"
          aria-label="Default agent"
          value={defaultAgent}
          onChange={(event) => onDefaultAgent(event.target.value as AgentKind)}
        >
          {AGENT_KINDS.map((agent) => (
            <option key={agent} value={agent}>
              {AGENT_LABELS[agent]}
            </option>
          ))}
        </select>
      </label>
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
      <label className="settings-row">
        <span>Shell mode</span>
        <select
          className="settings-select"
          aria-label="Shell mode"
          value={terminalShellMode}
          onChange={(event) => onTerminalShellMode(event.target.value as TerminalShellMode)}
        >
          <option value="auto">Auto</option>
          <option value="login">Login shell</option>
          <option value="non_login">Non-login shell</option>
        </select>
      </label>
      <NewPaneCwdControl value={newPaneCwd} onChange={onNewPaneCwd} />
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
        <span>Editor keybindings</span>
        <select
          className="settings-select"
          aria-label="Editor keybindings"
          value={editorKeybindingMode}
          onChange={(event) =>
            onEditorKeybindingMode(event.target.value as EditorKeybindingMode)
          }
        >
          <option value="standard">Standard (VS Code, Atom, Zed)</option>
          <option value="emacs">Emacs</option>
          <option value="vim">Vim</option>
        </select>
      </label>
      {editorKeybindingMode === "vim" ? (
        <>
          <label className="settings-row">
            <span>Vim line numbers</span>
            <span className="settings-check-row">
              <input
                type="checkbox"
                aria-label="Vim relative line numbers"
                checked={editorVimRelativeLineNumbers}
                onChange={(event) =>
                  onEditorVimRelativeLineNumbers(event.target.checked)
                }
              />
              Relative line numbers
            </span>
          </label>
          <label className="settings-row">
            <span>Vim clipboard</span>
            <span className="settings-check-row">
              <input
                type="checkbox"
                aria-label="Sync Vim register with system clipboard"
                checked={editorVimSystemClipboard}
                onChange={(event) =>
                  onEditorVimSystemClipboard(event.target.checked)
                }
              />
              Sync register with system clipboard
            </span>
          </label>
        </>
      ) : null}
      {editorKeybindingMode === "emacs" ? (
        <label className="settings-row">
          <span>Emacs clipboard</span>
          <span className="settings-check-row">
            <input
              type="checkbox"
              aria-label="Use system clipboard for Emacs kill and yank"
              checked={editorEmacsSystemClipboard}
              onChange={(event) =>
                onEditorEmacsSystemClipboard(event.target.checked)
              }
            />
            Use system clipboard for kill and yank
          </span>
        </label>
      ) : null}
      <label className="settings-row">
        <span>
          Editor tab limit
          <span className="settings-row-hint">
            Cap on simultaneously-mounted editor buffers. Tabs beyond this evict
            the least-recently-used non-dirty tab (kept in the reopen history).
            0 = unlimited.
          </span>
        </span>
        <input
          className="settings-input"
          type="number"
          min={0}
          max={100}
          step={1}
          aria-label="Editor tab limit"
          value={editorOpenLimit}
          onChange={(event) =>
            onEditorOpenLimit(Math.max(0, Math.min(100, Number(event.target.value) || 0)))
          }
        />
      </label>
      <label className="settings-row">
        <span>
          Closed editor history
          <span className="settings-row-hint">
            Number of recently-closed editors kept for Reopen Closed Editor
            (⌘⇧T / Ctrl+Shift+T). 0 = unlimited (capped at 500).
          </span>
        </span>
        <input
          className="settings-input"
          type="number"
          min={0}
          max={500}
          step={1}
          aria-label="Closed editor history limit"
          value={closedBufferHistoryLimit}
          onChange={(event) =>
            onClosedBufferHistoryLimit(
              Math.max(0, Math.min(500, Number(event.target.value) || 0)),
            )
          }
        />
      </label>
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

interface ShellIntegrationSessionStatus {
  id: string;
  title: string;
  cwd?: string;
  lastExitCode?: number | null;
  shellPromptMarkerSeen?: boolean;
  lastCommand?: { command: string; exitCode?: number | null } | null;
  preview?: { url: string } | null;
}

function TriggersSettings({
  triggers,
  onTriggers,
}: {
  triggers: TerminalTrigger[];
  onTriggers: (triggers: TerminalTrigger[]) => void;
}) {
  return (
    <section className="settings-section" aria-label="Terminal trigger settings">
      <TerminalTriggersControl triggers={triggers} onChange={onTriggers} />
    </section>
  );
}

function newPaneCwdSelectValue(
  value: NewPaneCwdMode,
): "active" | "follow" | "workspace" | "home" | "fixed" {
  return value.startsWith("fixed:")
    ? "fixed"
    : (value as "active" | "follow" | "workspace" | "home");
}

function newPaneCwdFixedPath(value: NewPaneCwdMode): string {
  return value.startsWith("fixed:") ? value.slice("fixed:".length) : "/";
}

function NewPaneCwdControl({
  value,
  onChange,
}: {
  value: NewPaneCwdMode;
  onChange: (mode: NewPaneCwdMode) => void;
}) {
  const fixedPath = newPaneCwdFixedPath(value);
  const selected = newPaneCwdSelectValue(value);
  return (
    <label className="settings-row settings-row-top">
      <span>New pane cwd</span>
      <span className="settings-stacked-control">
        <select
          className="settings-select"
          aria-label="New pane cwd"
          value={selected}
          onChange={(event) => {
            const next = event.target.value;
            if (next === "fixed") {
              onChange(`fixed:${fixedPath || "/"}`);
              return;
            }
            onChange(next as NewPaneCwdMode);
          }}
        >
          <option value="active">Active pane cwd</option>
          <option value="follow">Follow active pane cwd</option>
          <option value="workspace">Workspace root</option>
          <option value="home">Home directory</option>
          <option value="fixed">Fixed path</option>
        </select>
        {selected === "fixed" ? (
          <input
            className="settings-input"
            aria-label="Fixed new pane cwd"
            value={fixedPath}
            onChange={(event) => {
              const next = event.target.value.trim();
              if (next.startsWith("/") || /^[A-Za-z]:[\\/]/.test(next)) {
                onChange(`fixed:${next}`);
              }
            }}
          />
        ) : null}
      </span>
    </label>
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

const TRIGGER_ACTION_OPTIONS: Array<{ action: TerminalTriggerAction; label: string }> = [
  { action: "highlight", label: "Highlight" },
  { action: "badge", label: "Badge" },
  { action: "notify", label: "Notify" },
  { action: "attention", label: "Attention" },
  { action: "timeline", label: "Timeline" },
  { action: "open-preview", label: "Preview" },
  { action: "copy-line", label: "Copy line" },
];

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

  function toggleAction(
    trigger: TerminalTrigger,
    action: TerminalTriggerAction,
    enabled: boolean,
  ) {
    const actions = enabled
      ? [...new Set([...trigger.actions, action])]
      : trigger.actions.filter((item) => item !== action);
    updateTrigger(trigger.id, { actions });
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
              <span className="trigger-action-row">
                {TRIGGER_ACTION_OPTIONS.map(({ action, label }) => (
                  <span key={action}>
                    <input
                      type="checkbox"
                      aria-label={`${trigger.label} ${label} action`}
                      checked={trigger.actions.includes(action)}
                      onClick={(event) => event.stopPropagation()}
                      onChange={(event) =>
                        toggleAction(trigger, action, event.target.checked)
                      }
                    />
                    {label}
                  </span>
                ))}
              </span>
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
  showTerminalPaneAgentLabels: boolean;
  onOrientation: (orientation: TabBarOrientation) => void;
  onPosition: (position: TabBarPosition) => void;
  onShowFileIcons: (showFileIcons: boolean) => void;
  onShowTerminalPaneAgentLabels: (showTerminalPaneAgentLabels: boolean) => void;
}

function LayoutSettings({
  orientation,
  position,
  showFileIcons,
  showTerminalPaneAgentLabels,
  onOrientation,
  onPosition,
  onShowFileIcons,
  onShowTerminalPaneAgentLabels,
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
      <label className="settings-row">
        <span>Pane agent labels</span>
        <span className="settings-check-row">
          <input
            type="checkbox"
            aria-label="Show pane agent labels"
            checked={showTerminalPaneAgentLabels}
            onChange={(event) => onShowTerminalPaneAgentLabels(event.target.checked)}
          />
          Show pane agent labels
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

function ShellIntegrationSettings({
  enabled,
  sessions,
  onEnabled,
}: {
  enabled: boolean;
  sessions: ShellIntegrationSessionStatus[];
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

const APP_KEYBINDING_ACTIONS = Object.entries(
  APP_KEYBINDING_ACTION_LABELS,
) as Array<[AppKeybindingAction, string]>;

type CustomCommandDraft = {
  id: string;
  keys: string;
  description: string;
  command: string;
};

function commandDrafts(bindings: CustomCommandKeybinding[]): CustomCommandDraft[] {
  return bindings.map((binding, index) => ({
    id: `${index}:${binding.keys}:${binding.command}`,
    keys: binding.keys,
    description: binding.description ?? "",
    command: binding.command,
  }));
}

function normalizeCommandDrafts(
  drafts: CustomCommandDraft[],
): CustomCommandKeybinding[] {
  const seen = new Set<string>();
  const bindings: CustomCommandKeybinding[] = [];
  for (const draft of drafts) {
    const keys = normalizeAppKeyChord(draft.keys);
    const command = draft.command.trim();
    if (!keys || !command) {
      continue;
    }
    const description = draft.description.trim();
    const id = `${keys}:${command}:${description}`;
    if (seen.has(id)) {
      continue;
    }
    seen.add(id);
    bindings.push({
      keys,
      command,
      ...(description ? { description } : {}),
    });
  }
  return bindings;
}

function KeybindingsSettings({
  prefix,
  bindings,
  commandBindings,
  onPrefix,
  onBindings,
  onCommandBindings,
}: {
  prefix: string;
  bindings: AppKeybinding[];
  commandBindings: CustomCommandKeybinding[];
  onPrefix: (prefix: string) => void;
  onBindings: (bindings: AppKeybinding[]) => void;
  onCommandBindings: (bindings: CustomCommandKeybinding[]) => void;
}) {
  const [draftPrefix, setDraftPrefix] = useState(prefix);
  const [draftBindings, setDraftBindings] = useState<Record<string, string>>({});
  const [draftCommands, setDraftCommands] = useState<CustomCommandDraft[]>(() =>
    commandDrafts(commandBindings),
  );

  useEffect(() => {
    setDraftPrefix(prefix);
  }, [prefix]);

  useEffect(() => {
    setDraftBindings(
      Object.fromEntries(
        APP_KEYBINDING_ACTIONS.map(([action]) => [
          action,
          bindings
            .filter((binding) => binding.action === action)
            .map((binding) => binding.keys)
            .join(", "),
        ]),
      ),
    );
  }, [bindings]);

  useEffect(() => {
    setDraftCommands(commandDrafts(commandBindings));
  }, [commandBindings]);

  function commitPrefix() {
    const normalized = normalizeAppKeyChord(draftPrefix);
    if (normalized) {
      onPrefix(normalized);
      setDraftPrefix(normalized);
    } else {
      setDraftPrefix(prefix);
    }
  }

  function commitAction(action: AppKeybindingAction) {
    const nextBindings = (draftBindings[action] ?? "")
      .split(",")
      .map((item) => normalizeAppKeyChord(item))
      .filter(Boolean)
      .map((keys) => ({ keys, action }));
    const withoutAction = bindings.filter((binding) => binding.action !== action);
    onBindings([...withoutAction, ...nextBindings]);
    setDraftBindings((state) => ({
      ...state,
      [action]: nextBindings.map((binding) => binding.keys).join(", "),
    }));
  }

  function commitCommandDrafts(nextDrafts = draftCommands) {
    onCommandBindings(normalizeCommandDrafts(nextDrafts));
  }

  function updateCommandDraft(id: string, patch: Partial<CustomCommandDraft>) {
    setDraftCommands((state) =>
      state.map((draft) => (draft.id === id ? { ...draft, ...patch } : draft)),
    );
  }

  function addCommandDraft() {
    setDraftCommands((state) => [
      ...state,
      {
        id: `custom-command-${Date.now()}-${state.length}`,
        keys: "prefix+t",
        description: "",
        command: "",
      },
    ]);
  }

  function removeCommandDraft(id: string) {
    setDraftCommands((state) => {
      const next = state.filter((draft) => draft.id !== id);
      onCommandBindings(normalizeCommandDrafts(next));
      return next;
    });
  }

  const conflicts = appKeybindingConflicts(bindings, commandBindings);

  return (
    <section className="settings-section" aria-label="Keybinding settings">
      <label className="settings-row">
        <span>Prefix chord</span>
        <input
          className="settings-input"
          aria-label="Prefix chord"
          value={draftPrefix}
          onChange={(event) => setDraftPrefix(event.target.value)}
          onBlur={commitPrefix}
        />
      </label>
      <div className="keybinding-list">
        {APP_KEYBINDING_ACTIONS.map(([action, label]) => (
          <label key={action} className="keybinding-row">
            <span>
              <strong>{label}</strong>
              <code>{action}</code>
            </span>
            <input
              className="settings-input"
              aria-label={`${label} keybinding`}
              value={draftBindings[action] ?? ""}
              placeholder="prefix+x"
              onChange={(event) =>
                setDraftBindings((state) => ({
                  ...state,
                  [action]: event.target.value,
                }))
              }
              onBlur={() => commitAction(action)}
            />
            <kbd>
              {(draftBindings[action] ?? "")
                .split(",")
                .map((item) => displayKeyChord(item.trim()))
                .filter(Boolean)
                .join(", ")}
            </kbd>
          </label>
        ))}
      </div>
      <div className="settings-stacked-control">
        <div className="settings-section-heading">
          <strong>Custom commands</strong>
          <button type="button" className="text-button" onClick={addCommandDraft}>
            Add command
          </button>
        </div>
        <div className="custom-command-list">
          {draftCommands.map((binding) => (
            <div className="custom-command-row" key={binding.id}>
              <input
                className="settings-input"
                aria-label="Custom command keybinding"
                value={binding.keys}
                placeholder="prefix+t"
                onChange={(event) =>
                  updateCommandDraft(binding.id, { keys: event.target.value })
                }
                onBlur={() => commitCommandDrafts()}
              />
              <input
                className="settings-input"
                aria-label="Custom command description"
                value={binding.description}
                placeholder="Run tests"
                onChange={(event) =>
                  updateCommandDraft(binding.id, { description: event.target.value })
                }
                onBlur={() => commitCommandDrafts()}
              />
              <input
                className="settings-input"
                aria-label="Custom command shell command"
                value={binding.command}
                placeholder="pnpm test"
                onChange={(event) =>
                  updateCommandDraft(binding.id, { command: event.target.value })
                }
                onBlur={() => commitCommandDrafts()}
              />
              <kbd>{displayKeyChord(binding.keys)}</kbd>
              <button
                type="button"
                className="text-button"
                aria-label="Remove custom command"
                onClick={() => removeCommandDraft(binding.id)}
              >
                Remove
              </button>
            </div>
          ))}
          {draftCommands.length === 0 ? (
            <div className="settings-note">No custom command bindings.</div>
          ) : null}
        </div>
      </div>
      {conflicts.length > 0 ? (
        <div className="settings-note settings-warning">
          {conflicts.map((conflict) => (
            <div key={conflict.keys}>
              {displayKeyChord(conflict.keys)} conflicts across{" "}
              {[
                ...conflict.actions.map((action) => APP_KEYBINDING_ACTION_LABELS[action]),
                ...conflict.commands.map((command) => `Command: ${command}`),
              ].join(", ")}
            </div>
          ))}
        </div>
      ) : (
        <div className="settings-note">No app keybinding conflicts.</div>
      )}
    </section>
  );
}

function ConfigJsonSettings({
  value,
  status,
  onValue,
  onRefresh,
  onLoadFile,
  onSaveFile,
  onApply,
}: {
  value: string;
  status: string | null;
  onValue: (value: string) => void;
  onRefresh: () => void;
  onLoadFile: () => Promise<void>;
  onSaveFile: () => Promise<void>;
  onApply: () => void;
}) {
  const inputRef = useRef<HTMLInputElement | null>(null);

  function exportConfig() {
    const blob = new Blob([value], { type: "application/toml" });
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "onibi.config.toml";
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
    <section className="settings-section" aria-label="config.toml settings">
      <div className="config-json-actions">
        <button type="button" className="text-button" onClick={onRefresh}>
          Refresh
        </button>
        <button type="button" className="text-button primary" onClick={onApply}>
          Apply TOML
        </button>
        <button
          type="button"
          className="text-button"
          onClick={() => void onLoadFile()}
        >
          Load file
        </button>
        <button
          type="button"
          className="text-button"
          onClick={() => void onSaveFile()}
        >
          Save file
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
          accept=".toml,.json,application/json"
          className="visually-hidden"
          onChange={(event) => void importConfig(event.target.files?.[0])}
        />
      </div>
      <textarea
        className="settings-textarea config-json-editor"
        aria-label="Onibi config TOML"
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
  if (lower.includes("windows") || lower.includes("wt") || lower.includes("settings.json")) {
    return "windows-terminal";
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
  if (lower.includes("rio")) {
    return "rio";
  }
  if (lower.includes("tabby")) {
    return "tabby";
  }
  if (lower.includes("hyper") || lower === ".hyper.js") {
    return "hyper";
  }
  if (lower.includes("contour")) {
    return "contour";
  }
  if (lower.includes("foot.ini") || lower.includes("foot")) {
    return "foot";
  }
  if (lower.endsWith(".profile") || lower.includes("konsole")) {
    return "konsole";
  }
  if (lower.includes("terminalrc") || lower.includes("xfce")) {
    return "xfce-terminal";
  }
  if (lower.includes("muxy")) {
    return "muxy";
  }
  if (lower.includes("cmux")) {
    return "cmux";
  }
  return "ghostty";
}

interface TerminalImportSupport {
  source: TerminalConfigSource;
  label: string;
  capabilities: string[];
  detectHint: string;
  lowValue?: boolean;
}

const TERMINAL_IMPORT_SUPPORT: TerminalImportSupport[] = [
  {
    source: "ghostty",
    label: "Ghostty",
    capabilities: ["colors", "font family", "font size", "keybindings", "shaders"],
    detectHint: "~/.config/ghostty/config",
  },
  {
    source: "iterm2",
    label: "iTerm2",
    capabilities: ["colors", "font family", "font size", "theme name"],
    detectHint: "iTerm2 preferences, dynamic profiles, .itermcolors",
  },
  {
    source: "terminal-app",
    label: "Terminal.app",
    capabilities: ["colors", "font family", "font size", "theme name"],
    detectHint: "macOS Terminal preferences",
  },
  {
    source: "alacritty",
    label: "Alacritty",
    capabilities: ["colors", "font family", "font size", "keybindings"],
    detectHint: "~/.config/alacritty/alacritty.toml",
  },
  {
    source: "wezterm",
    label: "WezTerm",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.wezterm.lua, ~/.config/wezterm/wezterm.lua",
  },
  {
    source: "kitty",
    label: "kitty",
    capabilities: ["colors", "font family", "font size", "keybindings"],
    detectHint: "~/.config/kitty/kitty.conf",
  },
  {
    source: "warp",
    label: "Warp",
    capabilities: ["colors", "theme name"],
    detectHint: "~/.warp/themes/*.yaml",
  },
  {
    source: "windows-terminal",
    label: "Windows Terminal",
    capabilities: ["colors", "font family", "font size", "theme name"],
    detectHint: "Windows Terminal settings.json",
  },
  {
    source: "rio",
    label: "Rio",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.config/rio/config.toml",
  },
  {
    source: "tabby",
    label: "Tabby",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "Tabby config.yaml",
  },
  {
    source: "hyper",
    label: "Hyper",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.hyper.js",
  },
  {
    source: "contour",
    label: "Contour",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.config/contour/contour.yml",
  },
  {
    source: "foot",
    label: "foot",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.config/foot/foot.ini",
  },
  {
    source: "konsole",
    label: "Konsole",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.local/share/konsole/*.profile",
  },
  {
    source: "xfce-terminal",
    label: "Xfce Terminal",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.config/xfce4/terminal/terminalrc",
  },
  {
    source: "muxy",
    label: "muxy",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.config/muxy/config.toml",
  },
  {
    source: "cmux",
    label: "cmux",
    capabilities: ["colors", "font family", "font size", "keybindings", "theme name"],
    detectHint: "~/.config/cmux/config.toml",
  },
  {
    source: "tmux",
    label: "tmux",
    capabilities: ["keybindings"],
    detectHint: "~/.tmux.conf, ~/.config/tmux/tmux.conf",
    lowValue: true,
  },
  {
    source: "zellij",
    label: "Zellij",
    capabilities: ["keybindings"],
    detectHint: "~/.config/zellij/config.kdl",
    lowValue: true,
  },
];

function capabilitySummary(fields: string[]): string {
  return fields.length > 0 ? fields.join(", ") : "No importable settings";
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
  const manualSourceRef = useRef<TerminalConfigSource | null>(null);
  const [optionsById, setOptionsById] = useState<Record<string, ImportOptions>>({});
  const [importStatus, setImportStatus] = useState<string | null>(null);

  async function importFile(file: File | undefined, sourceOverride?: TerminalConfigSource | null) {
    if (!file) {
      return;
    }
    setImportStatus(null);
    const candidate: TerminalConfigCandidate = {
      source: sourceOverride ?? sourceForConfigFile(file),
      label: file.name,
      path: file.name,
      content: await file.text(),
    };
    const parsed = parseTerminalConfigImport(candidate);
    if (parsed.importedFields.length > 0) {
      onAddImport(parsed);
      setImportStatus(`Imported ${file.name}.`);
    } else {
      setImportStatus(`No supported settings found in ${file.name}.`);
    }
  }

  function chooseImportFile(source: TerminalConfigSource | null = null) {
    manualSourceRef.current = source;
    inputRef.current?.click();
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

  const importable = imports.filter((item) => item.importedFields.length > 0);
  const importablePrimary = importable.filter(
    (item) =>
      !TERMINAL_IMPORT_SUPPORT.find((support) => support.source === item.source)
        ?.lowValue,
  );
  const importableLowValue = importable.filter(
    (item) =>
      TERMINAL_IMPORT_SUPPORT.find((support) => support.source === item.source)
        ?.lowValue,
  );
  function sourceImports(source: TerminalConfigSource): TerminalConfigImport[] {
    return imports.filter((item) => item.source === source);
  }

  return (
    <section className="settings-section" aria-label="Import terminal settings">
      <div className="config-json-actions">
        <button type="button" className="text-button" onClick={onRefresh}>
          {loading ? "Detecting" : "Detect terminal settings"}
        </button>
        <button
          type="button"
          className="text-button"
          onClick={() => chooseImportFile()}
        >
          Import terminal settings file
        </button>
        <input
          ref={inputRef}
          type="file"
          accept=".conf,.config,.ini,.itermcolors,.js,.json,.kdl,.lua,.plist,.profile,.toml,.yaml,.yml"
          className="visually-hidden"
          onChange={(event) => {
            const source = manualSourceRef.current;
            manualSourceRef.current = null;
            void importFile(event.target.files?.[0], source);
            event.currentTarget.value = "";
          }}
        />
      </div>
      {error ? <div className="editor-error">{error}</div> : null}
      {importStatus ? <div className="settings-note">{importStatus}</div> : null}
      <div className="terminal-import-section">
        <div>
          <strong>Detected importable settings</strong>
          <div className="settings-note">
            These are local configs Onibi found and can apply now.
          </div>
        </div>
        {!loading && importablePrimary.length === 0 ? (
          <div className="settings-note">
            No importable terminal settings found. Use manual import below or
            review the supported-terminal matrix.
          </div>
        ) : null}
        {importablePrimary.map((item) => (
          <TerminalImportCard
            key={item.id}
            item={item}
            options={optionsFor(item)}
            onOption={(key, value) => updateOption(item, key, value)}
            onApply={() => onApply(item, optionsFor(item))}
          />
        ))}
      </div>
      {importableLowValue.length > 0 ? (
        <details className="terminal-low-value-imports">
          <summary>Low-value terminal/multiplexer configs</summary>
          <div className="settings-note">
            These only add useful settings when they expose keybindings or other
            Onibi-relevant fields.
          </div>
          {importableLowValue.map((item) => (
            <TerminalImportCard
              key={item.id}
              item={item}
              options={optionsFor(item)}
              onOption={(key, value) => updateOption(item, key, value)}
              onApply={() => onApply(item, optionsFor(item))}
            />
          ))}
        </details>
      ) : null}
      <div className="terminal-support-matrix">
        <div>
          <strong>Supported terminal imports</strong>
          <div className="settings-note">
            Found means Onibi detected a local config. Not found means the parser is
            available but no default-path config was present.
          </div>
        </div>
        {TERMINAL_IMPORT_SUPPORT.filter((support) => !support.lowValue).map(
          (support) => {
            const found = sourceImports(support.source);
            const canApply = found.some((item) => item.importedFields.length > 0);
            return (
              <div className="terminal-support-row" key={support.source}>
                <div>
                  <strong>{support.label}</strong>
                  <div className="settings-note">{support.detectHint}</div>
                </div>
                <span
                  className={`terminal-support-status ${
                    found.length > 0 ? "found" : "missing"
                  }`}
                >
                  {found.length > 0
                    ? canApply
                      ? "Found"
                      : "Found, no importable settings"
                    : "Not found"}
                </span>
                <div className="terminal-capability-list">
                  {support.capabilities.map((field) => (
                    <span key={field}>{field}</span>
                  ))}
                </div>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => chooseImportFile(support.source)}
                >
                  Manual import
                </button>
              </div>
            );
          },
        )}
      </div>
      <details className="terminal-low-value-imports">
        <summary>Terminal multiplexers and low-signal configs</summary>
        <div className="settings-note">
          Onibi demotes these because they usually do not carry visual terminal
          settings. They become useful only when their config exposes supported
          keybindings.
        </div>
        {TERMINAL_IMPORT_SUPPORT.filter((support) => support.lowValue).map(
          (support) => {
            const found = sourceImports(support.source);
            const canApply = found.some((item) => item.importedFields.length > 0);
            const importableFields = found.flatMap((item) => item.importedFields);
            return (
              <div className="terminal-support-row" key={support.source}>
                <div>
                  <strong>{support.label}</strong>
                  <div className="settings-note">{support.detectHint}</div>
                </div>
                <span
                  className={`terminal-support-status ${
                    canApply ? "found" : "missing"
                  }`}
                >
                  {canApply ? "Importable" : found.length > 0 ? "Found, demoted" : "Not found"}
                </span>
                <div className="terminal-capability-list">
                  {(importableFields.length > 0
                    ? [...new Set(importableFields)]
                    : support.capabilities
                  ).map((field) => (
                    <span key={field}>{field}</span>
                  ))}
                </div>
                <button
                  type="button"
                  className="text-button"
                  onClick={() => chooseImportFile(support.source)}
                >
                  Manual import
                </button>
              </div>
            );
          },
        )}
      </details>
    </section>
  );
}

function TerminalImportCard({
  item,
  options,
  onOption,
  onApply,
}: {
  item: TerminalConfigImport;
  options: ImportOptions;
  onOption: (key: keyof ImportOptions, value: boolean) => void;
  onApply: () => void;
}) {
  const support = TERMINAL_IMPORT_SUPPORT.find(
    (candidate) => candidate.source === item.source,
  );
  const hasSelectedOption = Object.values(options).some(Boolean);
  return (
    <div className="terminal-import-card">
      <div>
        <strong>{item.label}</strong>
        <div className="settings-note">{item.path}</div>
        <div className="settings-note">
          Source: {support?.label ?? item.source}
        </div>
        {item.colorSchemeName ? (
          <div className="settings-note">Theme: {item.colorSchemeName}</div>
        ) : null}
        <div className="settings-note">
          Importable: {capabilitySummary(item.importedFields)}
        </div>
      </div>
      <label className="settings-check-row">
        <input
          type="checkbox"
          checked={options.colors}
          disabled={Object.keys(item.colors).length === 0}
          onChange={(event) => onOption("colors", event.target.checked)}
        />
        Colors
      </label>
      <label className="settings-check-row">
        <input
          type="checkbox"
          checked={options.fontFamily}
          disabled={!item.fontFamily}
          onChange={(event) => onOption("fontFamily", event.target.checked)}
        />
        Font family{item.fontFamily ? `: ${item.fontFamily}` : ""}
      </label>
      <label className="settings-check-row">
        <input
          type="checkbox"
          checked={options.fontSize}
          disabled={!item.fontSize}
          onChange={(event) => onOption("fontSize", event.target.checked)}
        />
        Font size{item.fontSize ? `: ${item.fontSize}` : ""}
      </label>
      <label className="settings-check-row">
        <input
          type="checkbox"
          checked={options.keybindings}
          disabled={item.keybindings.length === 0}
          onChange={(event) => onOption("keybindings", event.target.checked)}
        />
        Keybindings{item.keybindings.length > 0 ? `: ${item.keybindings.length}` : ""}
      </label>
      <label className="settings-check-row">
        <input
          type="checkbox"
          checked={options.shaders}
          disabled={item.shaderPaths.length === 0}
          onChange={(event) => onOption("shaders", event.target.checked)}
        />
        Shaders{item.shaderPaths.length > 0 ? `: ${item.shaderPaths.length}` : ""}
      </label>
      <button
        type="button"
        className="text-button primary"
        disabled={!hasSelectedOption}
        onClick={onApply}
      >
        Apply selected
      </button>
    </div>
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
