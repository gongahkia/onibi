import { invoke } from "@tauri-apps/api/core";
import { load, type Store } from "@tauri-apps/plugin-store";
import { create } from "zustand";
import {
  ptyKill,
  ptyList,
  ptySessions,
  ptySpawn,
  sessionAttach,
  shellPath,
  type PtySessionMetadata,
  type PtyId,
  type PtyShellMode,
  type PtySpawnRequest,
} from "./tauri-bridge";
import { startAgentReview, stopAgentReview } from "./agent-review";

export const AGENT_KINDS = [
  "claude-code",
  "codex",
  "opencode",
  "gemini",
  "aider",
  "cursor",
  "goose",
  "copilot",
  "amp",
  "pi",
  "cline",
  "grok",
  "kimi",
  "kiro",
  "droid",
  "antigravity",
  "kilo",
  "crush",
  "shell",
] as const;

export type AgentKind = (typeof AGENT_KINDS)[number];

export type SessionStatus =
  | "idle"
  | "running"
  | "awaiting-approval"
  | "completed"
  | "error"
  | "stale";

export interface Session {
  id: string;
  name?: string;
  agent: AgentKind;
  workspaceId: string;
  title: string;
  status: SessionStatus;
  createdAt: number;
  pendingApprovals: string[];
  cwd?: string;
  lastExitCode?: number | null;
  lastTrigger?: TerminalTriggerMatch | null;
  restart?: SessionRestartMetadata | null;
  attentionDismissedAt?: number | null;
  preview?: SessionPreview | null;
  lastCommand?: SessionCommandMarker | null;
  lastCommandBlockId?: string | null;
  transcript?: SessionTranscript | null;
  shellPromptMarkerSeen?: boolean;
  provider?: import("./tauri-bridge").PtyProviderSession | null;
}

export type TerminalSplitDirection = "vertical" | "horizontal";

export interface TerminalLeafPane {
  type: "leaf";
  paneId: string;
  sessionId: string;
  sessionIds?: string[];
}

export interface TerminalSplitPane {
  type: "split";
  paneId: string;
  direction: TerminalSplitDirection;
  sizes?: number[];
  children: TerminalPaneNode[];
}

export type TerminalPaneNode = TerminalLeafPane | TerminalSplitPane;

export interface TerminalPanePlacement {
  type: "split" | "tab";
  targetPaneId: string;
  direction?: TerminalSplitDirection;
}

export type TerminalPaneDropPosition = "before" | "after";
export type TerminalLayoutPreset =
  | "even-horizontal"
  | "even-vertical"
  | "main-horizontal"
  | "main-vertical"
  | "tiled";

export function leafSessionIds(leaf: TerminalLeafPane): string[] {
  if (leaf.sessionIds && leaf.sessionIds.length > 0) {
    return leaf.sessionIds;
  }
  return [leaf.sessionId];
}

export type SessionEventType =
  | "session-started"
  | "session-stopped"
  | "session-split"
  | "arrangement-saved"
  | "arrangement-restored"
  | "terminal-trigger"
  | "command-block"
  | "file-opened"
  | "web-opened";

export interface SessionEvent {
  id: string;
  timestamp: number;
  type: SessionEventType;
  workspaceId?: string;
  sessionId?: string;
  agent?: AgentKind;
  summary: string;
  metadata?: Record<string, string | number | boolean | null>;
}

export interface Workspace {
  id: string;
  path: string;
  name: string;
  collapsed?: boolean;
}

export interface WorkspaceTab {
  id: string;
  workspaceId: string;
  title: string;
  terminalLayout: TerminalPaneNode | null;
  activeTerminalPaneId: string | null;
  maximizedTerminalPaneId: string | null;
  createdAt: number;
  updatedAt: number;
}

export interface SessionRestartMetadata {
  command: string;
  args: string[];
  cwd: string | null;
  env: Array<[string, string]>;
  shellMode?: TerminalShellMode;
}

export interface TerminalLaunchSpec extends SessionRestartMetadata {
  agent: AgentKind;
  workspaceId: string;
  title: string;
  initialPrompt?: string;
}

export type SessionAttentionState =
  | "idle"
  | "running"
  | "needs-approval"
  | "triggered"
  | "failed"
  | "exited"
  | "stale";

export interface SessionPreview {
  url: string;
  port: number | null;
  label: string;
  lastSeenAt: number;
}

export interface SessionCommandMarker {
  command: string;
  output: string;
  startedAt: number;
  endedAt?: number | null;
  exitCode?: number | null;
}

export type CommandBlockStatus = "running" | "succeeded" | "failed" | "aborted";
export type CommandBlockSource = "shell-integration" | "manual" | "trigger";
export type EditorKeybindingMode = "standard" | "emacs" | "vim";
export type NewPaneCwdMode =
  | "workspace"
  | "active"
  | "home"
  | "follow"
  | `fixed:${string}`;
export type TerminalShellMode = PtyShellMode;

export const KEYBINDING_INDEXES = [1, 2, 3, 4, 5, 6, 7, 8, 9] as const;
export type KeybindingIndex = (typeof KEYBINDING_INDEXES)[number];

export type BaseAppKeybindingAction =
  | "commandPalette.open"
  | "session.new"
  | "session.closeActive"
  | "session.navigator.open"
  | "editor.reopenClosed"
  | "terminal.splitRight"
  | "terminal.splitDown"
  | "terminal.focusNextPane"
  | "terminal.focusPreviousPane"
  | "terminal.toggleMaximize"
  | "terminal.copyMode.enter"
  | "keybindings.help.open"
  | "workspace.next"
  | "workspace.previous"
  | "workspace.navigator.open"
  | "workspace.tab.next"
  | "workspace.tab.previous";

export type WorkspaceIndexAppKeybindingAction =
  `workspace.focusIndex${KeybindingIndex}`;
export type WorkspaceTabIndexAppKeybindingAction =
  `workspace.tab.focusIndex${KeybindingIndex}`;
export type TerminalPaneIndexAppKeybindingAction =
  `terminal.focusPaneIndex${KeybindingIndex}`;

export type AppKeybindingAction =
  | BaseAppKeybindingAction
  | WorkspaceIndexAppKeybindingAction
  | WorkspaceTabIndexAppKeybindingAction
  | TerminalPaneIndexAppKeybindingAction;

export interface AppKeybinding {
  keys: string;
  action: AppKeybindingAction;
}

export interface CustomCommandKeybinding {
  keys: string;
  command: string;
  description?: string;
}

export interface CommandBlock {
  id: string;
  sessionId: string;
  workspaceId: string;
  agent: AgentKind;
  command: string;
  cwd: string;
  startedAt: number;
  endedAt?: number | null;
  exitCode?: number | null;
  status: CommandBlockStatus;
  outputPreview: string;
  previewUrl?: string | null;
  changedFiles: string[];
  attention?: SessionAttentionState | null;
  source: CommandBlockSource;
}

export interface SessionTranscript {
  text: string;
  updatedAt: number;
}

export type WorkspaceSidebarView =
  | "files"
  | "search"
  | "source-control"
  | "approvals";

export interface ArrangementSession {
  sessionId: string;
  title: string;
  agent: AgentKind;
  workspaceId: string;
  launch: SessionRestartMetadata;
}

export interface Arrangement {
  id: string;
  name: string;
  createdAt: number;
  updatedAt: number;
  workspaceId?: string;
  terminalLayout: TerminalPaneNode | null;
  activeTerminalPaneId: string | null;
  maximizedTerminalPaneId: string | null;
  selectedFile: MainSelection | null;
  openBuffers?: MainSelection[];
  activeBufferKey?: string | null;
  activeSidebarView: WorkspaceSidebarView;
  sessions: ArrangementSession[];
}

export interface FsEntry {
  name: string;
  path: string;
  kind: "file" | "dir";
  size: number;
}

export interface SelectedFile {
  type?: "file";
  workspaceId: string;
  workspaceRoot: string;
  path: string;
  name: string;
  size: number;
}

export interface SelectedGitDiff {
  type: "git-diff";
  workspaceId: string;
  workspaceRoot: string;
  path: string;
  name: string;
  stage: "staged" | "working";
}

export interface SelectedAgentReview {
  type: "agent-review";
  workspaceId: string;
  workspaceRoot: string;
  path: string;
  name: string;
  reviewId: string;
}

export interface SelectedWebView {
  type: "web";
  workspaceId: string;
  workspaceRoot: string;
  path: string;
  url: string;
  name: string;
}

export type MainSelection =
  | SelectedFile
  | SelectedGitDiff
  | SelectedAgentReview
  | SelectedWebView;

function capClosedStack(
  stack: MainSelection[],
  limit: number,
): MainSelection[] {
  // 0 = unlimited but apply a hard ceiling of 500 to avoid runaway memory.
  if (limit <= 0) return stack.slice(0, 500);
  return stack.slice(0, limit);
}

/** Apply LRU eviction so openBuffers fits under editorOpenLimit. Active and
 * dirty buffers are pinned (never evicted). Closed buffers go onto the stack.
 * Returns the new openBuffers list AND the updated closedBufferStack. */
function applyEditorOpenLimit(
  openBuffers: MainSelection[],
  activeKey: string | null,
  dirtyKeys: string[],
  bufferAccessOrder: string[],
  closedBufferStack: MainSelection[],
  editorOpenLimit: number,
  closedBufferHistoryLimit: number,
): { openBuffers: MainSelection[]; closedBufferStack: MainSelection[] } {
  if (editorOpenLimit <= 0 || openBuffers.length <= editorOpenLimit) {
    return { openBuffers, closedBufferStack };
  }
  const dirtySet = new Set(dirtyKeys);
  // Sort eviction candidates by access-order ascending (oldest first).
  // bufferAccessOrder is most-recently-used last.
  const accessRank = new Map<string, number>();
  bufferAccessOrder.forEach((key, idx) => accessRank.set(key, idx));
  const evictable = openBuffers
    .map((buffer) => ({ buffer, key: bufferKey(buffer) }))
    .filter(({ key }) => key !== activeKey && !dirtySet.has(key))
    .sort((a, b) => {
      const aRank = accessRank.get(a.key) ?? -1;
      const bRank = accessRank.get(b.key) ?? -1;
      return aRank - bRank;
    });
  const toEvictCount = openBuffers.length - editorOpenLimit;
  const evictedKeys = new Set<string>();
  const evicted: MainSelection[] = [];
  for (const candidate of evictable) {
    if (evicted.length >= toEvictCount) break;
    evictedKeys.add(candidate.key);
    evicted.push(candidate.buffer);
  }
  const nextOpenBuffers = openBuffers.filter(
    (buffer) => !evictedKeys.has(bufferKey(buffer)),
  );
  const nextStack = capClosedStack(
    [
      ...evicted.reverse(),
      ...closedBufferStack.filter(
        (buffer) => !evictedKeys.has(bufferKey(buffer)),
      ),
    ],
    closedBufferHistoryLimit,
  );
  return { openBuffers: nextOpenBuffers, closedBufferStack: nextStack };
}

function touchAccessOrder(order: string[], key: string): string[] {
  return [...order.filter((k) => k !== key), key];
}

export function bufferKey(selection: MainSelection): string {
  const wid = selection.workspaceId;
  if (selection.type === "git-diff") {
    return `git-diff:${wid}:${selection.stage}:${selection.path}`;
  }
  if (selection.type === "agent-review") {
    return `agent-review:${wid}:${selection.reviewId}:${selection.path}`;
  }
  if (selection.type === "web") {
    return `web:${wid}:${selection.url}`;
  }
  return `file:${wid}:${selection.path}`;
}

export function bufferLabel(selection: MainSelection): string {
  if (selection.type === "git-diff") {
    return selection.name || selection.path.split("/").pop() || selection.path;
  }
  if (selection.type === "agent-review") {
    return selection.name || selection.path.split("/").pop() || selection.path;
  }
  if (selection.type === "web") {
    return selection.name || selection.url;
  }
  return selection.name || selection.path.split("/").pop() || selection.path;
}

export type ThemeMode =
  | "system"
  | "vscode-dark-plus"
  | "vscode-light-plus"
  | "github-dark"
  | "github-light"
  | "one-dark-pro"
  | "dracula"
  | "catppuccin-mocha"
  | "catppuccin-latte"
  | "tokyo-night"
  | "night-owl"
  | "material-darker"
  | "material-lighter"
  | "monokai-pro"
  | "gruvbox-dark"
  | "nord"
  | "ayu-dark"
  | "ayu-light"
  | "palenight"
  | "cobalt2"
  | "shades-of-purple"
  | "synthwave-84"
  | "solarized-dark"
  | "terminal"
  | "onibi-flame"
  | "custom";
export type TabBarOrientation = "vertical" | "horizontal";
export type TabBarPosition = "left" | "right" | "top" | "bottom";
export type BuiltInThemeMode = Exclude<ThemeMode, "system" | "custom">;
export type ColorSchemeColorKey =
  | "bg0"
  | "bg1"
  | "bg2"
  | "bg3"
  | "fg0"
  | "fg1"
  | "fg2"
  | "accent"
  | "accent2"
  | "danger"
  | "flash"
  | "border"
  | "terminalBackground"
  | "terminalForeground"
  | "terminalCursor"
  | "terminalSelection";

export type ColorSchemeColors = Record<ColorSchemeColorKey, string>;

export interface ColorScheme {
  id: BuiltInThemeMode;
  label: string;
  colors: ColorSchemeColors;
}

export interface CustomColorScheme {
  label: string;
  colors: ColorSchemeColors;
}

export interface ResolvedColorScheme {
  id: Exclude<ThemeMode, "system">;
  label: string;
  colors: ColorSchemeColors;
}

export interface TerminalTheme {
  background: string;
  foreground: string;
  cursor: string;
  selectionBackground: string;
}

export interface GhosttyTheme {
  fontFamily?: string;
  fontSize?: number;
  background?: string;
  foreground?: string;
  palette: Record<number, string>;
}

export type DiffViewMode = "unified" | "side-by-side";
export type WebOpenMode = "off" | "ask" | "in-app";
export type TerminalConfigSource =
  | "ghostty"
  | "alacritty"
  | "wezterm"
  | "kitty"
  | "iterm2"
  | "terminal-app"
  | "tmux"
  | "zellij"
  | "warp"
  | "muxy"
  | "cmux"
  | "rio"
  | "tabby"
  | "hyper"
  | "contour"
  | "foot"
  | "konsole"
  | "xfce-terminal"
  | "windows-terminal";

export type TerminalKeybindingAction =
  | "copy"
  | "paste"
  | "clear"
  | "select-all"
  | "find";

export type TerminalCopyFormat = "plain" | "ansi" | "html";
export type TerminalInlineImageMode = "off" | "sixel" | "iterm" | "auto";
export type NotificationDelivery = "in_app" | "terminal" | "system";

export interface TerminalKeybinding {
  keys: string;
  action: TerminalKeybindingAction;
  source?: TerminalConfigSource;
}

export type TerminalTriggerAction =
  | "highlight"
  | "badge"
  | "notify"
  | "attention"
  | "timeline"
  | "open-preview"
  | "copy-line";
export type TerminalTriggerSource = "builtin" | "user";

export interface TerminalTrigger {
  id: string;
  label: string;
  pattern: string;
  enabled: boolean;
  actions: TerminalTriggerAction[];
  source: TerminalTriggerSource;
}

export interface TerminalTriggerMatch {
  id: string;
  label: string;
  pattern: string;
  line: string;
  actions: TerminalTriggerAction[];
  timestamp: number;
}

export interface TerminalConfigCandidate {
  source: TerminalConfigSource;
  label: string;
  path: string;
  content: string;
}

export interface TerminalConfigImport {
  id: string;
  source: TerminalConfigSource;
  label: string;
  path: string;
  colors: Partial<ColorSchemeColors>;
  fontFamily?: string;
  fontSize?: number;
  colorSchemeName?: string;
  keybindings: TerminalKeybinding[];
  shaderPaths: string[];
  importedFields: string[];
  unsupportedFields: string[];
}

export interface AppSettings {
  theme: ThemeMode;
  fontFamily: string;
  uiFontFamily: string;
  terminalFontFamily: string;
  editorFontFamily: string;
  uiFontSize: number;
  terminalFontSize: number;
  terminalScrollbackLines: number;
  terminalShellIntegration: boolean;
  terminalShellMode: TerminalShellMode;
  terminalKeybindings: TerminalKeybinding[];
  terminalShaderPaths: string[];
  terminalScreenReaderMode: boolean;
  terminalCopyFormat: TerminalCopyFormat;
  terminalOsc52Clipboard: boolean;
  terminalTransparentBackground: boolean;
  terminalInlineImages: TerminalInlineImageMode;
  terminalConfirmClose: boolean;
  terminalTriggers: TerminalTrigger[];
  notificationDelivery: NotificationDelivery;
  soundAlertsEnabled: boolean;
  soundCompletionPath: string;
  soundRequestPath: string;
  soundAgents: Partial<Record<AgentKind, boolean>>;
  suppressForegroundTabNotifications: boolean;
  keybindingPrefix: string;
  appKeybindings: AppKeybinding[];
  customCommandKeybindings: CustomCommandKeybinding[];
  newPaneCwd: NewPaneCwdMode;
  editorFontSize: number;
  editorKeybindingMode: EditorKeybindingMode;
  editorVimRelativeLineNumbers: boolean;
  editorVimSystemClipboard: boolean;
  editorEmacsSystemClipboard: boolean;
  /** Max simultaneously-mounted editor buffers. Tabs beyond this evict the LRU
   * inactive tab. 0 = unlimited (uses memory but preserves all undo history). */
  editorOpenLimit: number;
  /** Max entries kept in the reopen-closed stack. 0 = unlimited (within reason). */
  closedBufferHistoryLimit: number;
  diffViewMode: DiffViewMode;
  tabBarOrientation: TabBarOrientation;
  tabBarPosition: TabBarPosition;
  showHiddenFiles: boolean;
  showFileIcons: boolean;
  showTerminalPaneAgentLabels: boolean;
  webOpenMode: WebOpenMode;
  preferredBrowser: string;
  defaultAgent: AgentKind;
  agentLabelOverrides: Partial<Record<AgentKind, string>>;
  agentCommands: Record<AgentKind, string>;
  agentInstallCommands: Partial<Record<AgentKind, string>>;
  customColorScheme: CustomColorScheme;
  ghosttyTheme: GhosttyTheme | null;
}

export interface OnibiConfigExport {
  version: 1;
  settings: AppSettings;
  workspaces: Workspace[];
}

type PersistedState = {
  sessions?: Session[];
  workspaces?: Workspace[];
  workspaceTabs?: WorkspaceTab[];
  activeWorkspaceId?: string | null;
  activeWorkspaceTabId?: string | null;
  terminalLayout?: TerminalPaneNode | null;
  activeTerminalPaneId?: string | null;
  maximizedTerminalPaneId?: string | null;
  arrangements?: Arrangement[];
  activeSidebarView?: WorkspaceSidebarView;
  sessionEvents?: SessionEvent[];
  openBuffers?: MainSelection[];
  activeBufferKey?: string | null;
  bufferAccessOrder?: string[];
  closedBufferStack?: MainSelection[];
  settings?: Partial<AppSettings> & { fontSize?: number };
};

type SessionStore = {
  hydrated: boolean;
  sessions: Session[];
  activeSessionId: string | null;
  terminalLayout: TerminalPaneNode | null;
  activeTerminalPaneId: string | null;
  maximizedTerminalPaneId: string | null;
  workspaceTabs: WorkspaceTab[];
  activeWorkspaceId: string | null;
  activeWorkspaceTabId: string | null;
  arrangements: Arrangement[];
  activeSidebarView: WorkspaceSidebarView;
  workspaces: Workspace[];
  selectedFile: MainSelection | null;
  openBuffers: MainSelection[];
  activeBufferKey: string | null;
  bufferAccessOrder: string[];
  closedBufferStack: MainSelection[];
  dirtyBufferKeys: string[];
  sessionEvents: SessionEvent[];
  commandBlocks: CommandBlock[];
  activeCommandBlocks: Record<string, CommandBlock>;
  settings: AppSettings;
  setHydrated: (hydrated: boolean) => void;
  setActiveSession: (id: string | null) => void;
  setActiveTerminalPane: (paneId: string | null) => void;
  setMaximizedTerminalPane: (paneId: string | null) => void;
  toggleMaximizedTerminalPane: (paneId?: string | null) => void;
  focusRelativeTerminalPane: (delta: number) => void;
  focusTerminalPaneIndex: (index: number) => void;
  focusRelativeAttentionSession: (delta: number) => void;
  setActiveWorkspace: (workspaceId: string | null) => void;
  setActiveWorkspaceTab: (workspaceId: string, tabId: string) => void;
  createWorkspaceTab: (workspaceId: string, title?: string) => string;
  reorderWorkspaces: (fromId: string, toId: string, before: boolean) => void;
  toggleWorkspaceCollapsed: (workspaceId: string) => void;
  reorderWorkspaceTab: (
    workspaceId: string,
    fromTabId: string,
    toTabId: string,
    before: boolean,
  ) => void;
  focusRelativeWorkspace: (delta: number) => void;
  focusWorkspaceIndex: (index: number) => void;
  focusRelativeWorkspaceTab: (delta: number) => void;
  focusWorkspaceTabIndex: (index: number) => void;
  updateTerminalSplitSizes: (splitPaneId: string, sizes: number[]) => void;
  moveTerminalPane: (
    sourcePaneId: string,
    targetPaneId: string,
    position: TerminalPaneDropPosition,
  ) => void;
  applyTerminalLayoutPreset: (preset: TerminalLayoutPreset) => void;
  setActiveSidebarView: (view: WorkspaceSidebarView) => void;
  setActivePaneSession: (paneId: string, sessionId: string) => void;
  addSession: (session: Session, placement?: TerminalPanePlacement | null) => void;
  updateSession: (id: string, patch: Partial<Session>) => void;
  replaceSession: (id: string, session: Session) => void;
  removeSession: (id: string) => void;
  clearSessionAttention: (id: string) => void;
  appendSessionTranscript: (id: string, text: string) => void;
  startCommandBlock: (block: CommandBlock) => void;
  finishCommandBlock: (block: CommandBlock) => void;
  setCommandBlocks: (blocks: CommandBlock[]) => void;
  saveCurrentArrangement: (name?: string) => string | null;
  deleteArrangement: (id: string) => void;
  appendSessionEvent: (event: Omit<SessionEvent, "id" | "timestamp">) => void;
  openWebUrl: (url: string, sessionId?: string) => void;
  setWorkspaces: (workspaces: Workspace[]) => void;
  addWorkspace: (workspace: Workspace) => void;
  removeWorkspace: (id: string) => void;
  selectFile: (file: MainSelection | null) => void;
  setActiveBuffer: (key: string) => void;
  closeBuffer: (key: string) => void;
  closeOtherBuffers: (key: string) => void;
  closeAllBuffers: () => void;
  reopenClosedBuffer: () => boolean;
  setBufferDirty: (key: string, dirty: boolean) => void;
  reorderBuffer: (fromKey: string, toKey: string, before: boolean) => void;
  updateSettings: (patch: Partial<AppSettings>) => void;
  updateAgentCommand: (agent: AgentKind, command: string) => void;
};

const STORE_PATH = "settings.json";
const SESSION_TRANSCRIPT_LIMIT = 80_000;

export const AGENT_LABELS: Record<AgentKind, string> = {
  "claude-code": "Claude Code",
  codex: "Codex",
  opencode: "OpenCode",
  gemini: "Gemini",
  aider: "Aider",
  cursor: "Cursor",
  goose: "Goose",
  copilot: "Copilot CLI",
  amp: "Amp",
  pi: "Pi",
  cline: "Cline",
  grok: "Grok",
  kimi: "Kimi",
  kiro: "Kiro",
  droid: "Droid",
  antigravity: "Antigravity",
  kilo: "Kilo",
  crush: "Crush",
  shell: "Plain shell",
};

export const DEFAULT_AGENT_COMMANDS: Record<AgentKind, string> = {
  "claude-code": "claude",
  codex: "codex",
  opencode: "opencode",
  gemini: "gemini",
  aider: "aider",
  cursor: "cursor-agent",
  goose: "goose session",
  copilot: "copilot",
  amp: "amp",
  pi: "pi",
  cline: "cline",
  grok: "grok",
  kimi: "kimi",
  kiro: "kiro",
  droid: "droid",
  antigravity: "antigravity",
  kilo: "kilo",
  crush: "crush",
  shell: "",
};
const LEGACY_CLAUDE_CODE_AGENT_COMMAND = "claude code";

export const DEFAULT_AGENT_INSTALL_COMMANDS: Partial<Record<AgentKind, string>> = {
  "claude-code": "npm install -g @anthropic-ai/claude-code",
  codex: "npm install -g @openai/codex",
  opencode: "curl -fsSL https://opencode.ai/install | bash",
  gemini: "npm install -g @google/gemini-cli",
  aider: "python3 -m pip install --user aider-chat",
  goose: "curl -fsSL https://github.com/block/goose/releases/latest/download/download_cli.sh | bash",
  copilot: "npm install -g @github/copilot",
  amp: "npm install -g @ampcode/cli",
  pi: "npm install -g @earendil-works/pi-coding-agent",
  cline: "npm install -g cline",
  crush: "go install github.com/charmbracelet/crush@latest",
};

export const COLOR_SCHEME_COLOR_KEYS = [
  "bg0",
  "bg1",
  "bg2",
  "bg3",
  "fg0",
  "fg1",
  "fg2",
  "accent",
  "accent2",
  "danger",
  "flash",
  "border",
  "terminalBackground",
  "terminalForeground",
  "terminalCursor",
  "terminalSelection",
] as const satisfies readonly ColorSchemeColorKey[];

export const COLOR_SCHEME_COLOR_LABELS: Record<ColorSchemeColorKey, string> = {
  bg0: "App background",
  bg1: "Panel background",
  bg2: "Raised background",
  bg3: "Hover background",
  fg0: "Primary text",
  fg1: "Secondary text",
  fg2: "Muted text",
  accent: "Accent",
  accent2: "Action accent",
  danger: "Danger",
  flash: "Alert flash",
  border: "Border",
  terminalBackground: "Terminal background",
  terminalForeground: "Terminal text",
  terminalCursor: "Terminal cursor",
  terminalSelection: "Terminal selection",
};

const COLOR_SCHEME_CSS_VARIABLES: Record<ColorSchemeColorKey, string> = {
  bg0: "--bg-0",
  bg1: "--bg-1",
  bg2: "--bg-2",
  bg3: "--bg-3",
  fg0: "--fg-0",
  fg1: "--fg-1",
  fg2: "--fg-2",
  accent: "--accent",
  accent2: "--accent-2",
  danger: "--danger",
  flash: "--flash",
  border: "--border",
  terminalBackground: "--terminal-bg",
  terminalForeground: "--terminal-fg",
  terminalCursor: "--terminal-cursor",
  terminalSelection: "--terminal-selection",
};

export const BUILT_IN_COLOR_SCHEMES: ColorScheme[] = [
  {
    id: "terminal",
    label: "Terminal",
    colors: {
      bg0: "#1e1e1e",
      bg1: "#252526",
      bg2: "#2d2d30",
      bg3: "#3c3c3c",
      fg0: "#d4d4d4",
      fg1: "#969696",
      fg2: "#6e6e6e",
      accent: "#007acc",
      accent2: "#0098ff",
      danger: "#f48771",
      flash: "#cca700",
      border: "#3e3e42",
      terminalBackground: "#1e1e1e",
      terminalForeground: "#d4d4d4",
      terminalCursor: "#aeafad",
      terminalSelection: "#264f78",
    },
  },
  {
    id: "vscode-dark-plus",
    label: "VSCode Dark+",
    colors: {
      bg0: "#1e1e1e",
      bg1: "#252526",
      bg2: "#2d2d30",
      bg3: "#3c3c3c",
      fg0: "#cccccc",
      fg1: "#969696",
      fg2: "#6e6e6e",
      accent: "#007acc",
      accent2: "#0098ff",
      danger: "#f48771",
      flash: "#cca700",
      border: "#3e3e42",
      terminalBackground: "#1e1e1e",
      terminalForeground: "#d4d4d4",
      terminalCursor: "#aeafad",
      terminalSelection: "#264f78",
    },
  },
  {
    id: "vscode-light-plus",
    label: "VSCode Light+",
    colors: {
      bg0: "#ffffff",
      bg1: "#f3f3f3",
      bg2: "#ececec",
      bg3: "#e8e8e8",
      fg0: "#333333",
      fg1: "#616161",
      fg2: "#8c8c8c",
      accent: "#0078d4",
      accent2: "#005a9e",
      danger: "#a1260d",
      flash: "#bf8803",
      border: "#e5e5e5",
      terminalBackground: "#ffffff",
      terminalForeground: "#333333",
      terminalCursor: "#005a9e",
      terminalSelection: "#add6ff",
    },
  },
  {
    id: "onibi-flame",
    label: "Onibi Flame",
    colors: {
      bg0: "#0b0f12",
      bg1: "#11181c",
      bg2: "#172127",
      bg3: "#20303a",
      fg0: "#edf2f4",
      fg1: "#b9c4ca",
      fg2: "#7f8f98",
      accent: "#f2c14e",
      accent2: "#2fb7a5",
      danger: "#e85d5d",
      flash: "#f2c14e",
      border: "#26343c",
      terminalBackground: "#0b0e14",
      terminalForeground: "#e6edf3",
      terminalCursor: "#f4d35e",
      terminalSelection: "#315f7d",
    },
  },
  {
    id: "github-dark",
    label: "GitHub Dark Default",
    colors: {
      bg0: "#0d1117",
      bg1: "#161b22",
      bg2: "#21262d",
      bg3: "#30363d",
      fg0: "#f0f6fc",
      fg1: "#c9d1d9",
      fg2: "#8b949e",
      accent: "#58a6ff",
      accent2: "#3fb950",
      danger: "#f85149",
      flash: "#d29922",
      border: "#30363d",
      terminalBackground: "#0d1117",
      terminalForeground: "#c9d1d9",
      terminalCursor: "#58a6ff",
      terminalSelection: "#264f78",
    },
  },
  {
    id: "github-light",
    label: "GitHub Light Default",
    colors: {
      bg0: "#ffffff",
      bg1: "#f6f8fa",
      bg2: "#ffffff",
      bg3: "#eaeef2",
      fg0: "#24292f",
      fg1: "#57606a",
      fg2: "#6e7781",
      accent: "#0969da",
      accent2: "#1a7f37",
      danger: "#cf222e",
      flash: "#bf8700",
      border: "#d0d7de",
      terminalBackground: "#ffffff",
      terminalForeground: "#24292f",
      terminalCursor: "#0969da",
      terminalSelection: "#b6d7ff",
    },
  },
  {
    id: "one-dark-pro",
    label: "One Dark Pro",
    colors: {
      bg0: "#282c34",
      bg1: "#21252b",
      bg2: "#2c313a",
      bg3: "#3a3f4b",
      fg0: "#abb2bf",
      fg1: "#c8ccd4",
      fg2: "#7f848e",
      accent: "#61afef",
      accent2: "#98c379",
      danger: "#e06c75",
      flash: "#e5c07b",
      border: "#3e4451",
      terminalBackground: "#282c34",
      terminalForeground: "#abb2bf",
      terminalCursor: "#528bff",
      terminalSelection: "#3e4451",
    },
  },
  {
    id: "dracula",
    label: "Dracula",
    colors: {
      bg0: "#282a36",
      bg1: "#21222c",
      bg2: "#343746",
      bg3: "#44475a",
      fg0: "#f8f8f2",
      fg1: "#d6d6d0",
      fg2: "#8f94a8",
      accent: "#bd93f9",
      accent2: "#50fa7b",
      danger: "#ff5555",
      flash: "#f1fa8c",
      border: "#44475a",
      terminalBackground: "#282a36",
      terminalForeground: "#f8f8f2",
      terminalCursor: "#f8f8f2",
      terminalSelection: "#44475a",
    },
  },
  {
    id: "catppuccin-mocha",
    label: "Catppuccin Mocha",
    colors: {
      bg0: "#1e1e2e",
      bg1: "#181825",
      bg2: "#313244",
      bg3: "#45475a",
      fg0: "#cdd6f4",
      fg1: "#bac2de",
      fg2: "#7f849c",
      accent: "#cba6f7",
      accent2: "#94e2d5",
      danger: "#f38ba8",
      flash: "#f9e2af",
      border: "#45475a",
      terminalBackground: "#1e1e2e",
      terminalForeground: "#cdd6f4",
      terminalCursor: "#f5e0dc",
      terminalSelection: "#45475a",
    },
  },
  {
    id: "catppuccin-latte",
    label: "Catppuccin Latte",
    colors: {
      bg0: "#eff1f5",
      bg1: "#e6e9ef",
      bg2: "#ffffff",
      bg3: "#ccd0da",
      fg0: "#4c4f69",
      fg1: "#5c5f77",
      fg2: "#8c8fa1",
      accent: "#8839ef",
      accent2: "#179299",
      danger: "#d20f39",
      flash: "#df8e1d",
      border: "#bcc0cc",
      terminalBackground: "#eff1f5",
      terminalForeground: "#4c4f69",
      terminalCursor: "#dc8a78",
      terminalSelection: "#ccd0da",
    },
  },
  {
    id: "tokyo-night",
    label: "Tokyo Night",
    colors: {
      bg0: "#1a1b26",
      bg1: "#16161e",
      bg2: "#24283b",
      bg3: "#292e42",
      fg0: "#c0caf5",
      fg1: "#a9b1d6",
      fg2: "#565f89",
      accent: "#7aa2f7",
      accent2: "#9ece6a",
      danger: "#f7768e",
      flash: "#e0af68",
      border: "#414868",
      terminalBackground: "#1a1b26",
      terminalForeground: "#c0caf5",
      terminalCursor: "#c0caf5",
      terminalSelection: "#33467c",
    },
  },
  {
    id: "night-owl",
    label: "Night Owl",
    colors: {
      bg0: "#011627",
      bg1: "#0b2942",
      bg2: "#0e344f",
      bg3: "#133f5f",
      fg0: "#d6deeb",
      fg1: "#c5d3e0",
      fg2: "#637777",
      accent: "#82aaff",
      accent2: "#7fdbca",
      danger: "#ef5350",
      flash: "#ecc48d",
      border: "#1d3b53",
      terminalBackground: "#011627",
      terminalForeground: "#d6deeb",
      terminalCursor: "#80a4c2",
      terminalSelection: "#1d3b53",
    },
  },
  {
    id: "material-darker",
    label: "Material Theme Darker",
    colors: {
      bg0: "#212121",
      bg1: "#1b1b1b",
      bg2: "#2b2b2b",
      bg3: "#353535",
      fg0: "#eeffff",
      fg1: "#c3d6d6",
      fg2: "#717cb4",
      accent: "#82aaff",
      accent2: "#c3e88d",
      danger: "#f07178",
      flash: "#ffcb6b",
      border: "#3a3a3a",
      terminalBackground: "#212121",
      terminalForeground: "#eeffff",
      terminalCursor: "#ffcc00",
      terminalSelection: "#353535",
    },
  },
  {
    id: "material-lighter",
    label: "Material Theme Lighter",
    colors: {
      bg0: "#fafafa",
      bg1: "#f2f2f2",
      bg2: "#ffffff",
      bg3: "#e0e0e0",
      fg0: "#546e7a",
      fg1: "#455a64",
      fg2: "#90a4ae",
      accent: "#39adb5",
      accent2: "#91b859",
      danger: "#e53935",
      flash: "#f6a434",
      border: "#d6d6d6",
      terminalBackground: "#fafafa",
      terminalForeground: "#546e7a",
      terminalCursor: "#39adb5",
      terminalSelection: "#d7eff2",
    },
  },
  {
    id: "monokai-pro",
    label: "Monokai Pro",
    colors: {
      bg0: "#2d2a2e",
      bg1: "#221f22",
      bg2: "#403e41",
      bg3: "#5b595c",
      fg0: "#fcfcfa",
      fg1: "#d8d8d2",
      fg2: "#939293",
      accent: "#ff6188",
      accent2: "#a9dc76",
      danger: "#ff6188",
      flash: "#ffd866",
      border: "#5b595c",
      terminalBackground: "#2d2a2e",
      terminalForeground: "#fcfcfa",
      terminalCursor: "#fc9867",
      terminalSelection: "#5b595c",
    },
  },
  {
    id: "gruvbox-dark",
    label: "Gruvbox Dark",
    colors: {
      bg0: "#282828",
      bg1: "#1d2021",
      bg2: "#32302f",
      bg3: "#3c3836",
      fg0: "#fbf1c7",
      fg1: "#ebdbb2",
      fg2: "#928374",
      accent: "#fabd2f",
      accent2: "#8ec07c",
      danger: "#fb4934",
      flash: "#fe8019",
      border: "#504945",
      terminalBackground: "#282828",
      terminalForeground: "#ebdbb2",
      terminalCursor: "#fabd2f",
      terminalSelection: "#504945",
    },
  },
  {
    id: "nord",
    label: "Nord",
    colors: {
      bg0: "#2e3440",
      bg1: "#242933",
      bg2: "#3b4252",
      bg3: "#434c5e",
      fg0: "#eceff4",
      fg1: "#d8dee9",
      fg2: "#8fbcbb",
      accent: "#88c0d0",
      accent2: "#a3be8c",
      danger: "#bf616a",
      flash: "#ebcb8b",
      border: "#4c566a",
      terminalBackground: "#2e3440",
      terminalForeground: "#d8dee9",
      terminalCursor: "#88c0d0",
      terminalSelection: "#4c566a",
    },
  },
  {
    id: "ayu-dark",
    label: "Ayu Dark",
    colors: {
      bg0: "#0f1419",
      bg1: "#111820",
      bg2: "#1f2430",
      bg3: "#293340",
      fg0: "#e6e1cf",
      fg1: "#bfbab0",
      fg2: "#5c6773",
      accent: "#ffb454",
      accent2: "#95e6cb",
      danger: "#f07178",
      flash: "#ffb454",
      border: "#2d3640",
      terminalBackground: "#0f1419",
      terminalForeground: "#e6e1cf",
      terminalCursor: "#f29718",
      terminalSelection: "#253340",
    },
  },
  {
    id: "ayu-light",
    label: "Ayu Light",
    colors: {
      bg0: "#fafafa",
      bg1: "#f0f0f0",
      bg2: "#ffffff",
      bg3: "#e3e8ed",
      fg0: "#5c6773",
      fg1: "#37474f",
      fg2: "#abb0b6",
      accent: "#ff9940",
      accent2: "#55b4d4",
      danger: "#f07178",
      flash: "#f2ae49",
      border: "#d5d9de",
      terminalBackground: "#fafafa",
      terminalForeground: "#5c6773",
      terminalCursor: "#ffaa33",
      terminalSelection: "#e3e8ed",
    },
  },
  {
    id: "palenight",
    label: "Palenight",
    colors: {
      bg0: "#292d3e",
      bg1: "#202331",
      bg2: "#343a4f",
      bg3: "#444267",
      fg0: "#a6accd",
      fg1: "#c3cee3",
      fg2: "#676e95",
      accent: "#c792ea",
      accent2: "#c3e88d",
      danger: "#ff5370",
      flash: "#ffcb6b",
      border: "#444267",
      terminalBackground: "#292d3e",
      terminalForeground: "#a6accd",
      terminalCursor: "#ffcc00",
      terminalSelection: "#444267",
    },
  },
  {
    id: "cobalt2",
    label: "Cobalt2",
    colors: {
      bg0: "#193549",
      bg1: "#122738",
      bg2: "#21445c",
      bg3: "#2a526c",
      fg0: "#ffffff",
      fg1: "#d9e6f2",
      fg2: "#9eb2c4",
      accent: "#ffc600",
      accent2: "#2afd72",
      danger: "#ff628c",
      flash: "#ffc600",
      border: "#2a526c",
      terminalBackground: "#193549",
      terminalForeground: "#ffffff",
      terminalCursor: "#ffc600",
      terminalSelection: "#2a526c",
    },
  },
  {
    id: "shades-of-purple",
    label: "Shades of Purple",
    colors: {
      bg0: "#2d2b55",
      bg1: "#1e1e3f",
      bg2: "#3b386b",
      bg3: "#4d4985",
      fg0: "#ffffff",
      fg1: "#d7d4ff",
      fg2: "#a599e9",
      accent: "#fad000",
      accent2: "#a5ff90",
      danger: "#ff628c",
      flash: "#ff9d00",
      border: "#4d4985",
      terminalBackground: "#2d2b55",
      terminalForeground: "#ffffff",
      terminalCursor: "#fad000",
      terminalSelection: "#4d4985",
    },
  },
  {
    id: "synthwave-84",
    label: "SynthWave '84",
    colors: {
      bg0: "#262335",
      bg1: "#1f1b2d",
      bg2: "#2f2a43",
      bg3: "#3a3355",
      fg0: "#f92aad",
      fg1: "#f7f1ff",
      fg2: "#848bbd",
      accent: "#fede5d",
      accent2: "#72f1b8",
      danger: "#fe4450",
      flash: "#fede5d",
      border: "#495495",
      terminalBackground: "#262335",
      terminalForeground: "#f7f1ff",
      terminalCursor: "#fede5d",
      terminalSelection: "#495495",
    },
  },
  {
    id: "solarized-dark",
    label: "Solarized Dark",
    colors: {
      bg0: "#002b36",
      bg1: "#073642",
      bg2: "#0d4652",
      bg3: "#145663",
      fg0: "#fdf6e3",
      fg1: "#93a1a1",
      fg2: "#657b83",
      accent: "#b58900",
      accent2: "#2aa198",
      danger: "#dc322f",
      flash: "#b58900",
      border: "#164b56",
      terminalBackground: "#002b36",
      terminalForeground: "#eee8d5",
      terminalCursor: "#b58900",
      terminalSelection: "#073642",
    },
  },
];

export const COLOR_SCHEME_OPTIONS: Array<{ id: ThemeMode; label: string }> = [
  { id: "system", label: "System" },
  ...BUILT_IN_COLOR_SCHEMES.map(({ id, label }) => ({ id, label })),
  { id: "custom", label: "Custom" },
];

export const DEFAULT_CUSTOM_COLOR_SCHEME: CustomColorScheme = {
  label: "Custom",
  colors: { ...BUILT_IN_COLOR_SCHEMES[0].colors },
};

export const TERMINAL_EFFECTIVE_UNLIMITED_SCROLLBACK_LINES = 1_000_000;

export const DEFAULT_TERMINAL_TRIGGERS: TerminalTrigger[] = [
  {
    id: "approval-needed",
    label: "Approval needed",
    pattern: "\\b(approval|permission|confirm|allow|deny)\\b",
    enabled: false,
    actions: ["highlight", "badge", "notify", "attention", "timeline"],
    source: "builtin",
  },
  {
    id: "tests-failed",
    label: "Tests failed",
    pattern: "\\b(test|tests)\\b.*\\b(fail|failed|failure|failing)\\b|\\bFAIL\\b",
    enabled: false,
    actions: ["highlight", "badge", "attention", "timeline"],
    source: "builtin",
  },
  {
    id: "build-failed",
    label: "Build failed",
    pattern: "\\b(build|compile|compilation)\\b.*\\b(fail|failed|error)\\b",
    enabled: false,
    actions: ["highlight", "badge", "attention", "timeline"],
    source: "builtin",
  },
  {
    id: "dev-server-ready",
    label: "Dev server ready",
    pattern:
      "\\b(Local|ready|listening|server)\\b.*\\b(https?://|localhost|127\\.0\\.0\\.1|0\\.0\\.0\\.0)",
    enabled: false,
    actions: ["highlight", "timeline", "open-preview"],
    source: "builtin",
  },
  {
    id: "migration-prompt",
    label: "Migration or destructive prompt",
    pattern:
      "\\b(migration|migrate|drop|delete|overwrite|destructive)\\b.*\\b(confirm|continue|proceed|y/N|yes/no)\\b",
    enabled: false,
    actions: ["highlight", "badge", "attention", "notify", "timeline"],
    source: "builtin",
  },
  {
    id: "dangerous-command",
    label: "Dangerous command",
    pattern: "\\b(rm\\s+-rf|git\\s+clean\\s+-[a-z]*x|drop\\s+database|truncate\\s+table)\\b",
    enabled: false,
    actions: ["highlight", "badge", "notify", "attention", "timeline"],
    source: "builtin",
  },
];

const BASE_APP_KEYBINDING_ACTION_LABELS: Record<BaseAppKeybindingAction, string> = {
  "commandPalette.open": "Open command palette",
  "session.new": "New session",
  "session.closeActive": "Close active session",
  "session.navigator.open": "Open session navigator",
  "editor.reopenClosed": "Reopen closed editor",
  "terminal.splitRight": "Split terminal right",
  "terminal.splitDown": "Split terminal down",
  "terminal.focusNextPane": "Focus next terminal pane",
  "terminal.focusPreviousPane": "Focus previous terminal pane",
  "terminal.toggleMaximize": "Maximize or restore terminal pane",
  "terminal.copyMode.enter": "Enter terminal copy mode",
  "keybindings.help.open": "Open keybinding help",
  "workspace.next": "Focus next workspace",
  "workspace.previous": "Focus previous workspace",
  "workspace.navigator.open": "Open workspace navigator",
  "workspace.tab.next": "Focus next workspace terminal tab",
  "workspace.tab.previous": "Focus previous workspace terminal tab",
};

const WORKSPACE_INDEX_ACTION_LABELS = Object.fromEntries(
  KEYBINDING_INDEXES.map((index) => [
    `workspace.focusIndex${index}`,
    `Focus workspace ${index}`,
  ]),
) as Record<WorkspaceIndexAppKeybindingAction, string>;

const WORKSPACE_TAB_INDEX_ACTION_LABELS = Object.fromEntries(
  KEYBINDING_INDEXES.map((index) => [
    `workspace.tab.focusIndex${index}`,
    `Focus workspace terminal tab ${index}`,
  ]),
) as Record<WorkspaceTabIndexAppKeybindingAction, string>;

const TERMINAL_PANE_INDEX_ACTION_LABELS = Object.fromEntries(
  KEYBINDING_INDEXES.map((index) => [
    `terminal.focusPaneIndex${index}`,
    `Focus terminal pane ${index}`,
  ]),
) as Record<TerminalPaneIndexAppKeybindingAction, string>;

export const APP_KEYBINDING_ACTION_LABELS: Record<AppKeybindingAction, string> = {
  ...BASE_APP_KEYBINDING_ACTION_LABELS,
  ...WORKSPACE_INDEX_ACTION_LABELS,
  ...WORKSPACE_TAB_INDEX_ACTION_LABELS,
  ...TERMINAL_PANE_INDEX_ACTION_LABELS,
};

export const DEFAULT_KEYBINDING_PREFIX = "ctrl+b";

export const DEFAULT_APP_KEYBINDINGS: AppKeybinding[] = [
  { keys: "cmd+p", action: "commandPalette.open" },
  { keys: "ctrl+p", action: "commandPalette.open" },
  { keys: "cmd+n", action: "session.new" },
  { keys: "ctrl+n", action: "session.new" },
  { keys: "cmd+shift+t", action: "editor.reopenClosed" },
  { keys: "ctrl+shift+t", action: "editor.reopenClosed" },
  { keys: "prefix+v", action: "terminal.splitRight" },
  { keys: "prefix+s", action: "terminal.splitDown" },
  { keys: "prefix+z", action: "terminal.toggleMaximize" },
  { keys: "prefix+n", action: "terminal.focusNextPane" },
  { keys: "prefix+p", action: "terminal.focusPreviousPane" },
  { keys: "prefix+[", action: "terminal.copyMode.enter" },
  { keys: "prefix+x", action: "session.closeActive" },
  { keys: "prefix+w", action: "workspace.navigator.open" },
  { keys: "prefix+g", action: "session.navigator.open" },
  { keys: "prefix+?", action: "keybindings.help.open" },
  { keys: "prefix+right", action: "workspace.next" },
  { keys: "prefix+left", action: "workspace.previous" },
  { keys: "prefix+shift+right", action: "workspace.tab.next" },
  { keys: "prefix+shift+left", action: "workspace.tab.previous" },
  ...KEYBINDING_INDEXES.map((index) => ({
    keys: `prefix+${index}`,
    action: `workspace.tab.focusIndex${index}` as WorkspaceTabIndexAppKeybindingAction,
  })),
];

export const DEFAULT_CUSTOM_COMMAND_KEYBINDINGS: CustomCommandKeybinding[] = [];

export const DEFAULT_SETTINGS: AppSettings = {
  theme: "vscode-dark-plus",
  fontFamily: "Menlo, Monaco, monospace",
  uiFontFamily:
    'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  terminalFontFamily: "Menlo, Monaco, monospace",
  editorFontFamily: "Menlo, Monaco, monospace",
  uiFontSize: 15,
  terminalFontSize: 13,
  terminalScrollbackLines: 0,
  terminalShellIntegration: true,
  terminalShellMode: "auto",
  terminalKeybindings: [],
  terminalShaderPaths: [],
  terminalScreenReaderMode: false,
  terminalCopyFormat: "plain",
  terminalOsc52Clipboard: false,
  terminalTransparentBackground: false,
  terminalInlineImages: "off",
  terminalConfirmClose: true,
  terminalTriggers: DEFAULT_TERMINAL_TRIGGERS,
  notificationDelivery: "in_app",
  soundAlertsEnabled: false,
  soundCompletionPath: "",
  soundRequestPath: "",
  soundAgents: {},
  suppressForegroundTabNotifications: true,
  keybindingPrefix: DEFAULT_KEYBINDING_PREFIX,
  appKeybindings: DEFAULT_APP_KEYBINDINGS,
  customCommandKeybindings: DEFAULT_CUSTOM_COMMAND_KEYBINDINGS,
  newPaneCwd: "active",
  editorFontSize: 13,
  editorKeybindingMode: "standard",
  editorVimRelativeLineNumbers: false,
  editorVimSystemClipboard: false,
  editorEmacsSystemClipboard: false,
  editorOpenLimit: 10,
  closedBufferHistoryLimit: 20,
  diffViewMode: "side-by-side",
  tabBarOrientation: "vertical",
  tabBarPosition: "left",
  showHiddenFiles: false,
  showFileIcons: true,
  showTerminalPaneAgentLabels: true,
  webOpenMode: "ask",
  preferredBrowser: "onibi",
  defaultAgent: "claude-code",
  agentLabelOverrides: {},
  agentCommands: DEFAULT_AGENT_COMMANDS,
  agentInstallCommands: DEFAULT_AGENT_INSTALL_COMMANDS,
  customColorScheme: DEFAULT_CUSTOM_COLOR_SCHEME,
  ghosttyTheme: null,
};

let storePromise: Promise<Store> | null = null;
let persistTimer: number | undefined;

function getStore(): Promise<Store> {
  storePromise ??= load(STORE_PATH, {
    defaults: {
      sessions: [],
      workspaces: [],
      workspaceTabs: [],
      activeWorkspaceId: null,
      activeWorkspaceTabId: null,
      terminalLayout: null,
      activeTerminalPaneId: null,
      maximizedTerminalPaneId: null,
      arrangements: [],
      activeSidebarView: "files",
      sessionEvents: [],
      settings: DEFAULT_SETTINGS,
    },
    autoSave: false,
    overrideDefaults: true,
  });
  return storePromise;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isThemeMode(value: unknown): value is ThemeMode {
  return (
    typeof value === "string" &&
    COLOR_SCHEME_OPTIONS.some((option) => option.id === value)
  );
}

const LEGACY_THEME_MAP: Record<string, ThemeMode> = {
  dark: "github-dark",
  light: "github-light",
  graphite: "github-dark",
  ember: "gruvbox-dark",
  forest: "nord",
  ocean: "night-owl",
  violet: "shades-of-purple",
  "solarized-light": "github-light",
  paper: "github-light",
  "high-contrast": "github-dark",
};

function normalizeThemeMode(value: unknown): ThemeMode {
  if (isThemeMode(value)) {
    return value;
  }
  if (typeof value === "string" && LEGACY_THEME_MAP[value]) {
    return LEGACY_THEME_MAP[value];
  }
  return DEFAULT_SETTINGS.theme;
}

function isTabBarOrientation(value: unknown): value is TabBarOrientation {
  return value === "vertical" || value === "horizontal";
}

function isTabBarPosition(value: unknown): value is TabBarPosition {
  return value === "left" || value === "right" || value === "top" || value === "bottom";
}

function normalizeHexColor(value: unknown, fallback: string): string {
  if (typeof value !== "string") {
    return fallback;
  }
  const trimmed = value.trim();
  return /^#[0-9a-fA-F]{6}$/.test(trimmed) ? trimmed : fallback;
}

function normalizeColorSchemeColors(
  value: unknown,
  fallback: ColorSchemeColors,
): ColorSchemeColors {
  const source = isRecord(value) ? value : {};
  return Object.fromEntries(
    COLOR_SCHEME_COLOR_KEYS.map((key) => [
      key,
      normalizeHexColor(source[key], fallback[key]),
    ]),
  ) as ColorSchemeColors;
}

function normalizeCustomColorScheme(value: unknown): CustomColorScheme {
  const source = isRecord(value) ? value : {};
  const label =
    typeof source.label === "string" && source.label.trim()
      ? source.label.trim().slice(0, 48)
      : DEFAULT_CUSTOM_COLOR_SCHEME.label;
  return {
    label,
    colors: normalizeColorSchemeColors(
      source.colors,
      DEFAULT_CUSTOM_COLOR_SCHEME.colors,
    ),
  };
}

function normalizeAgentLabelOverrides(
  value: unknown,
): Partial<Record<AgentKind, string>> {
  if (!isRecord(value)) {
    return {};
  }
  const overrides: Partial<Record<AgentKind, string>> = {};
  for (const agent of AGENT_KINDS) {
    const raw = value[agent];
    if (typeof raw !== "string") {
      continue;
    }
    const label = raw.trim().replace(/\s+/g, " ").slice(0, 40);
    if (label && label !== AGENT_LABELS[agent]) {
      overrides[agent] = label;
    }
  }
  return overrides;
}

export function agentDisplayLabel(
  agent: AgentKind,
  settingsOrOverrides?:
    | Pick<AppSettings, "agentLabelOverrides">
    | Partial<Record<AgentKind, string>>
    | null,
): string {
  const overrides =
    settingsOrOverrides && "agentLabelOverrides" in settingsOrOverrides
      ? settingsOrOverrides.agentLabelOverrides
      : settingsOrOverrides;
  const label = overrides?.[agent];
  return typeof label === "string" && label.trim()
    ? label.trim()
    : AGENT_LABELS[agent];
}

function normalizeFontFamily(value: unknown, fallback: string): string {
  return typeof value === "string" && value.trim() ? value : fallback;
}

function normalizeFontSize(value: unknown, fallback: number): number {
  const parsed = typeof value === "number" ? value : Number(value);
  return Number.isFinite(parsed) && parsed > 0
    ? Math.min(Math.max(parsed, 10), 28)
    : fallback;
}

function normalizeScrollbackLines(value: unknown, fallback: number): number {
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return fallback;
  }
  return Math.min(Math.round(parsed), TERMINAL_EFFECTIVE_UNLIMITED_SCROLLBACK_LINES);
}

function normalizeBufferLimit(
  value: unknown,
  fallback: number,
  max: number,
): number {
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(parsed) || parsed < 0) {
    return fallback;
  }
  return Math.min(Math.round(parsed), max);
}

function normalizeTerminalKeybindingAction(
  value: unknown,
): TerminalKeybindingAction | null {
  return value === "copy" ||
    value === "paste" ||
    value === "clear" ||
    value === "select-all" ||
    value === "find"
    ? value
    : null;
}

function normalizeTerminalCopyFormat(value: unknown): TerminalCopyFormat {
  return value === "ansi" || value === "html" || value === "plain"
    ? value
    : DEFAULT_SETTINGS.terminalCopyFormat;
}

function normalizeTerminalInlineImageMode(value: unknown): TerminalInlineImageMode {
  return value === "off" || value === "sixel" || value === "iterm" || value === "auto"
    ? value
    : DEFAULT_SETTINGS.terminalInlineImages;
}

function normalizeNotificationDelivery(value: unknown): NotificationDelivery {
  return value === "in_app" || value === "terminal" || value === "system"
    ? value
    : DEFAULT_SETTINGS.notificationDelivery;
}

function normalizeSoundAgents(value: unknown): Partial<Record<AgentKind, boolean>> {
  if (!isRecord(value)) {
    return {};
  }
  const result: Partial<Record<AgentKind, boolean>> = {};
  for (const agent of AGENT_KINDS) {
    const enabled = value[agent];
    if (typeof enabled === "boolean") {
      result[agent] = enabled;
    }
  }
  return result;
}

function normalizeTerminalKeybindings(value: unknown): TerminalKeybinding[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const seen = new Set<string>();
  const keybindings: TerminalKeybinding[] = [];
  for (const item of value) {
    if (!isRecord(item) || typeof item.keys !== "string") {
      continue;
    }
    const action = normalizeTerminalKeybindingAction(item.action);
    const keys = normalizeKeyCombo(item.keys);
    if (!keys || !action) {
      continue;
    }
    const id = `${keys}:${action}`;
    if (seen.has(id)) {
      continue;
    }
    seen.add(id);
    keybindings.push({
      keys,
      action,
      source:
        typeof item.source === "string"
          ? (item.source as TerminalConfigSource)
          : undefined,
    });
  }
  return keybindings;
}

function normalizeKeyboardKeyName(value: string): string {
  const key = value.trim().toLowerCase();
  if (!key) {
    return "";
  }
  if (key === "return") {
    return "enter";
  }
  if (key === "escape") {
    return "esc";
  }
  if (key === "spacebar" || key === " ") {
    return "space";
  }
  if (key === "arrowright") {
    return "right";
  }
  if (key === "arrowleft") {
    return "left";
  }
  if (key === "arrowup") {
    return "up";
  }
  if (key === "arrowdown") {
    return "down";
  }
  if (key === "del") {
    return "delete";
  }
  return key.length === 1 ? key : key;
}

export function normalizeAppKeyChord(value: string): string {
  const parts = value
    .trim()
    .replace(/-/g, "+")
    .split("+")
    .map((part) => part.trim().toLowerCase())
    .filter(Boolean);
  const modifiers = new Set<string>();
  let prefix = false;
  let key = "";
  for (const part of parts) {
    if (part === "prefix") {
      prefix = true;
    } else if (["cmd", "command", "meta", "super"].includes(part)) {
      modifiers.add("cmd");
    } else if (["ctrl", "control"].includes(part)) {
      modifiers.add("ctrl");
    } else if (["alt", "option", "opt"].includes(part)) {
      modifiers.add("alt");
    } else if (part === "shift") {
      modifiers.add("shift");
    } else {
      key = normalizeKeyboardKeyName(part);
    }
  }
  if (!key) {
    return "";
  }
  return [
    ...(prefix ? ["prefix"] : []),
    ...(["cmd", "ctrl", "alt", "shift"] as const).filter((modifier) =>
      modifiers.has(modifier),
    ),
    key,
  ].join("+");
}

function normalizeAppKeybindingAction(value: unknown): AppKeybindingAction | null {
  return typeof value === "string" &&
    Object.prototype.hasOwnProperty.call(APP_KEYBINDING_ACTION_LABELS, value)
    ? (value as AppKeybindingAction)
    : null;
}

function normalizeAppKeybindings(value: unknown): AppKeybinding[] {
  const source = Array.isArray(value) ? value : DEFAULT_APP_KEYBINDINGS;
  const seen = new Set<string>();
  const bindings: AppKeybinding[] = [];
  for (const item of source) {
    if (!isRecord(item) || typeof item.keys !== "string") {
      continue;
    }
    const action = normalizeAppKeybindingAction(item.action);
    const keys = normalizeAppKeyChord(item.keys);
    if (!keys || !action) {
      continue;
    }
    const id = `${keys}:${action}`;
    if (seen.has(id)) {
      continue;
    }
    seen.add(id);
    bindings.push({ keys, action });
  }
  return bindings.length > 0 ? bindings : DEFAULT_APP_KEYBINDINGS;
}

function normalizeCustomCommandKeybindings(value: unknown): CustomCommandKeybinding[] {
  if (!Array.isArray(value)) {
    return DEFAULT_CUSTOM_COMMAND_KEYBINDINGS;
  }
  const seen = new Set<string>();
  const bindings: CustomCommandKeybinding[] = [];
  for (const item of value) {
    if (!isRecord(item) || typeof item.keys !== "string") {
      continue;
    }
    const keys = normalizeAppKeyChord(item.keys);
    const command =
      typeof item.command === "string" ? item.command.trim().slice(0, 4096) : "";
    if (!keys || !command) {
      continue;
    }
    const description =
      typeof item.description === "string" && item.description.trim()
        ? item.description.trim().slice(0, 120)
        : undefined;
    const id = `${keys}:${command}:${description ?? ""}`;
    if (seen.has(id)) {
      continue;
    }
    seen.add(id);
    bindings.push({ keys, command, ...(description ? { description } : {}) });
  }
  return bindings;
}

export interface AppKeybindingConflict {
  keys: string;
  actions: AppKeybindingAction[];
  commands: string[];
}

export function appKeybindingConflicts(
  bindings: AppKeybinding[],
  customCommands: CustomCommandKeybinding[] = [],
): AppKeybindingConflict[] {
  const byKeys = new Map<
    string,
    { actions: AppKeybindingAction[]; commands: string[] }
  >();
  for (const binding of normalizeAppKeybindings(bindings)) {
    const entry = byKeys.get(binding.keys) ?? { actions: [], commands: [] };
    if (!entry.actions.includes(binding.action)) {
      entry.actions.push(binding.action);
    }
    byKeys.set(binding.keys, entry);
  }
  for (const binding of normalizeCustomCommandKeybindings(customCommands)) {
    const entry = byKeys.get(binding.keys) ?? { actions: [], commands: [] };
    const label = binding.description || binding.command;
    if (!entry.commands.includes(label)) {
      entry.commands.push(label);
    }
    byKeys.set(binding.keys, entry);
  }
  return [...byKeys.entries()]
    .filter(([, entry]) => entry.actions.length + entry.commands.length > 1)
    .map(([keys, entry]) => ({ keys, ...entry }));
}

export function keyChordFromKeyboardEvent(event: KeyboardEvent): string {
  const key = normalizeKeyboardKeyName(event.key);
  if (!key || ["meta", "control", "ctrl", "alt", "shift"].includes(key)) {
    return "";
  }
  const shiftProducesKey = key === "?";
  return [
    ...(event.metaKey ? ["cmd"] : []),
    ...(event.ctrlKey ? ["ctrl"] : []),
    ...(event.altKey ? ["alt"] : []),
    ...(event.shiftKey && !shiftProducesKey ? ["shift"] : []),
    key,
  ].join("+");
}

export function displayKeyChord(value: string): string {
  const normalized = normalizeAppKeyChord(value);
  return normalized
    .split("+")
    .filter(Boolean)
    .map((part) => {
      if (part === "cmd") return "Cmd";
      if (part === "ctrl") return "Ctrl";
      if (part === "alt") return "Alt";
      if (part === "shift") return "Shift";
      if (part === "prefix") return "Prefix";
      if (part.length === 1) return part.toUpperCase();
      return part[0].toUpperCase() + part.slice(1);
    })
    .join("+");
}

export function isAbsoluteCwdPath(value: string | null | undefined): value is string {
  if (!value) {
    return false;
  }
  return value.startsWith("/") || /^[A-Za-z]:[\\/]/.test(value);
}

export function normalizeNewPaneCwdMode(value: unknown): NewPaneCwdMode {
  if (typeof value !== "string") {
    return DEFAULT_SETTINGS.newPaneCwd;
  }
  const trimmed = value.trim();
  if (
    trimmed === "workspace" ||
    trimmed === "home" ||
    trimmed === "active" ||
    trimmed === "follow"
  ) {
    return trimmed;
  }
  if (trimmed.startsWith("fixed:")) {
    const path = trimmed.slice("fixed:".length).trim();
    if (isAbsoluteCwdPath(path)) {
      return `fixed:${path}`;
    }
  }
  return DEFAULT_SETTINGS.newPaneCwd;
}

export function fixedNewPaneCwdPath(value: NewPaneCwdMode): string | null {
  return value.startsWith("fixed:") ? value.slice("fixed:".length) : null;
}

export function resolveNewPaneCwd(
  settings: AppSettings,
  activeSession: Session | null | undefined,
  activeWorkspace: Workspace | null | undefined,
): string | null {
  const mode = normalizeNewPaneCwdMode(settings.newPaneCwd);
  const fixedPath = fixedNewPaneCwdPath(mode);
  if (fixedPath) {
    return fixedPath;
  }
  if (mode === "home") {
    return null;
  }
  if (mode === "workspace") {
    return activeWorkspace?.path ?? null;
  }
  return activeSession?.cwd ?? activeWorkspace?.path ?? null;
}

function normalizeStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const seen = new Set<string>();
  const result: string[] = [];
  for (const item of value) {
    if (typeof item !== "string") {
      continue;
    }
    const trimmed = item.trim();
    if (!trimmed || seen.has(trimmed)) {
      continue;
    }
    seen.add(trimmed);
    result.push(trimmed);
  }
  return result;
}

function normalizeTerminalTriggerAction(value: unknown): TerminalTriggerAction | null {
  return value === "highlight" ||
    value === "badge" ||
    value === "notify" ||
    value === "attention" ||
    value === "timeline" ||
    value === "open-preview" ||
    value === "copy-line"
    ? value
    : null;
}

function normalizeTerminalTriggers(value: unknown): TerminalTrigger[] {
  const sourceItems = Array.isArray(value) ? value : DEFAULT_TERMINAL_TRIGGERS;
  const defaultsById = new Map(DEFAULT_TERMINAL_TRIGGERS.map((item) => [item.id, item]));
  const seen = new Set<string>();
  const normalized: TerminalTrigger[] = [];

  for (const item of sourceItems) {
    if (!isRecord(item)) {
      continue;
    }
    const id =
      typeof item.id === "string" && item.id.trim()
        ? item.id.trim().slice(0, 64)
        : "";
    const fallback = id ? defaultsById.get(id) : undefined;
    const label =
      typeof item.label === "string" && item.label.trim()
        ? item.label.trim().slice(0, 80)
        : fallback?.label ?? id;
    const pattern =
      typeof item.pattern === "string" && item.pattern.trim()
        ? item.pattern.trim()
        : fallback?.pattern ?? "";
    if (!id || !label || !pattern || seen.has(id)) {
      continue;
    }
    seen.add(id);
    const actions = Array.isArray(item.actions)
      ? item.actions
          .map(normalizeTerminalTriggerAction)
          .filter((action): action is TerminalTriggerAction => action !== null)
      : fallback?.actions ?? ["highlight", "badge"];
    normalized.push({
      id,
      label,
      pattern,
      enabled: typeof item.enabled === "boolean" ? item.enabled : fallback?.enabled ?? false,
      actions: actions.length > 0 ? actions : fallback?.actions ?? ["highlight", "badge"],
      source: item.source === "user" ? "user" : fallback?.source ?? "user",
    });
  }

  for (const item of DEFAULT_TERMINAL_TRIGGERS) {
    if (!seen.has(item.id)) {
      normalized.push(item);
    }
  }

  return normalized;
}

function normalizeEnvPairs(value: unknown): Array<[string, string]> {
  if (!Array.isArray(value)) {
    return [];
  }
  const pairs: Array<[string, string]> = [];
  for (const item of value) {
    if (
      Array.isArray(item) &&
      item.length >= 2 &&
      typeof item[0] === "string" &&
      typeof item[1] === "string" &&
      item[0].trim()
    ) {
      pairs.push([item[0].trim(), item[1]]);
    }
  }
  return pairs;
}

function normalizeStringList(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return [];
  }
  return value
    .filter((item): item is string => typeof item === "string")
    .map((item) => item.trim())
    .filter(Boolean);
}

function normalizeAgentKind(value: unknown, fallback: AgentKind): AgentKind {
  return typeof value === "string" && (AGENT_KINDS as readonly string[]).includes(value)
    ? (value as AgentKind)
    : fallback;
}

function normalizeWorkspaceSidebarView(value: unknown): WorkspaceSidebarView {
  return value === "files" ||
    value === "search" ||
    value === "source-control" ||
    value === "approvals"
    ? value
    : "files";
}

function normalizeDiffViewMode(value: unknown): DiffViewMode {
  return value === "unified" || value === "side-by-side"
    ? value
    : DEFAULT_SETTINGS.diffViewMode;
}

function normalizeWebOpenMode(value: unknown): WebOpenMode {
  return value === "off" || value === "ask" || value === "in-app"
    ? value
    : DEFAULT_SETTINGS.webOpenMode;
}

function normalizeEditorKeybindingMode(value: unknown): EditorKeybindingMode {
  return value === "standard" || value === "emacs" || value === "vim"
    ? value
    : DEFAULT_SETTINGS.editorKeybindingMode;
}

export function normalizeTerminalShellMode(value: unknown): TerminalShellMode {
  return value === "auto" || value === "login" || value === "non_login"
    ? value
    : DEFAULT_SETTINGS.terminalShellMode;
}

function normalizeAgentCommands(
  settings: Partial<AppSettings> | undefined,
): Record<AgentKind, string> {
  const agentCommands = isRecord(settings?.agentCommands)
    ? {
        ...DEFAULT_AGENT_COMMANDS,
        ...(settings?.agentCommands as Partial<Record<AgentKind, string>>),
      }
    : { ...DEFAULT_AGENT_COMMANDS };
  const claudeCodeCommand = agentCommands["claude-code"];
  if (
    typeof claudeCodeCommand === "string" &&
    claudeCodeCommand.trim().replace(/\s+/g, " ") ===
      LEGACY_CLAUDE_CODE_AGENT_COMMAND
  ) {
    agentCommands["claude-code"] = DEFAULT_AGENT_COMMANDS["claude-code"];
  }
  return agentCommands;
}

function mergeSettings(settings: Partial<AppSettings> | undefined): AppSettings {
  const agentCommands = normalizeAgentCommands(settings);
  const agentInstallCommands = isRecord(settings?.agentInstallCommands)
    ? {
        ...DEFAULT_AGENT_INSTALL_COMMANDS,
        ...(settings?.agentInstallCommands as Partial<Record<AgentKind, string>>),
      }
    : { ...DEFAULT_AGENT_INSTALL_COMMANDS };
  const merged = {
    ...DEFAULT_SETTINGS,
    ...settings,
  };
  const normalizedBase = { ...merged } as AppSettings & Record<string, unknown>;
  delete normalizedBase["terminalProfiles"];
  delete normalizedBase["defaultTerminalProfileId"];
  const legacyFontFamily = normalizeFontFamily(
    settings?.fontFamily,
    DEFAULT_SETTINGS.fontFamily,
  );
  const terminalFontFamily = normalizeFontFamily(
    merged.terminalFontFamily,
    legacyFontFamily,
  );
  const legacyFontSize = normalizeFontSize(
    (settings as (Partial<AppSettings> & { fontSize?: number }) | undefined)?.fontSize,
    DEFAULT_SETTINGS.terminalFontSize,
  );
  return {
    ...normalizedBase,
    theme: normalizeThemeMode(merged.theme),
    fontFamily: terminalFontFamily,
    uiFontFamily: normalizeFontFamily(
      merged.uiFontFamily,
      DEFAULT_SETTINGS.uiFontFamily,
    ),
    terminalFontFamily,
    editorFontFamily: normalizeFontFamily(
      merged.editorFontFamily,
      legacyFontFamily,
    ),
    uiFontSize: normalizeFontSize(merged.uiFontSize, legacyFontSize),
    terminalFontSize: normalizeFontSize(merged.terminalFontSize, legacyFontSize),
    terminalScrollbackLines: normalizeScrollbackLines(
      merged.terminalScrollbackLines,
      DEFAULT_SETTINGS.terminalScrollbackLines,
    ),
    terminalShellIntegration:
      typeof merged.terminalShellIntegration === "boolean"
        ? merged.terminalShellIntegration
        : DEFAULT_SETTINGS.terminalShellIntegration,
    terminalShellMode: normalizeTerminalShellMode(merged.terminalShellMode),
    terminalKeybindings: normalizeTerminalKeybindings(merged.terminalKeybindings),
    terminalShaderPaths: normalizeStringArray(merged.terminalShaderPaths),
    terminalScreenReaderMode:
      typeof merged.terminalScreenReaderMode === "boolean"
        ? merged.terminalScreenReaderMode
        : DEFAULT_SETTINGS.terminalScreenReaderMode,
    terminalCopyFormat: normalizeTerminalCopyFormat(merged.terminalCopyFormat),
    terminalOsc52Clipboard:
      typeof merged.terminalOsc52Clipboard === "boolean"
        ? merged.terminalOsc52Clipboard
        : DEFAULT_SETTINGS.terminalOsc52Clipboard,
    terminalTransparentBackground:
      typeof merged.terminalTransparentBackground === "boolean"
        ? merged.terminalTransparentBackground
        : DEFAULT_SETTINGS.terminalTransparentBackground,
    terminalInlineImages: normalizeTerminalInlineImageMode(
      merged.terminalInlineImages,
    ),
    terminalConfirmClose:
      typeof merged.terminalConfirmClose === "boolean"
        ? merged.terminalConfirmClose
        : DEFAULT_SETTINGS.terminalConfirmClose,
    terminalTriggers: normalizeTerminalTriggers(merged.terminalTriggers),
    notificationDelivery: normalizeNotificationDelivery(merged.notificationDelivery),
    soundAlertsEnabled:
      typeof merged.soundAlertsEnabled === "boolean"
        ? merged.soundAlertsEnabled
        : DEFAULT_SETTINGS.soundAlertsEnabled,
    soundCompletionPath:
      typeof merged.soundCompletionPath === "string"
        ? merged.soundCompletionPath
        : DEFAULT_SETTINGS.soundCompletionPath,
    soundRequestPath:
      typeof merged.soundRequestPath === "string"
        ? merged.soundRequestPath
        : DEFAULT_SETTINGS.soundRequestPath,
    soundAgents: normalizeSoundAgents(merged.soundAgents),
    suppressForegroundTabNotifications:
      typeof merged.suppressForegroundTabNotifications === "boolean"
        ? merged.suppressForegroundTabNotifications
        : DEFAULT_SETTINGS.suppressForegroundTabNotifications,
    keybindingPrefix:
      normalizeAppKeyChord(merged.keybindingPrefix) || DEFAULT_SETTINGS.keybindingPrefix,
    appKeybindings: normalizeAppKeybindings(merged.appKeybindings),
    customCommandKeybindings: normalizeCustomCommandKeybindings(
      merged.customCommandKeybindings,
    ),
    newPaneCwd: normalizeNewPaneCwdMode(merged.newPaneCwd),
    editorFontSize: normalizeFontSize(merged.editorFontSize, legacyFontSize),
    editorKeybindingMode: normalizeEditorKeybindingMode(merged.editorKeybindingMode),
    editorVimRelativeLineNumbers:
      typeof merged.editorVimRelativeLineNumbers === "boolean"
        ? merged.editorVimRelativeLineNumbers
        : DEFAULT_SETTINGS.editorVimRelativeLineNumbers,
    editorVimSystemClipboard:
      typeof merged.editorVimSystemClipboard === "boolean"
        ? merged.editorVimSystemClipboard
        : DEFAULT_SETTINGS.editorVimSystemClipboard,
    editorEmacsSystemClipboard:
      typeof merged.editorEmacsSystemClipboard === "boolean"
        ? merged.editorEmacsSystemClipboard
        : DEFAULT_SETTINGS.editorEmacsSystemClipboard,
    editorOpenLimit: normalizeBufferLimit(
      merged.editorOpenLimit,
      DEFAULT_SETTINGS.editorOpenLimit,
      100,
    ),
    closedBufferHistoryLimit: normalizeBufferLimit(
      merged.closedBufferHistoryLimit,
      DEFAULT_SETTINGS.closedBufferHistoryLimit,
      500,
    ),
    diffViewMode: normalizeDiffViewMode(merged.diffViewMode),
    tabBarOrientation: isTabBarOrientation(merged.tabBarOrientation)
      ? merged.tabBarOrientation
      : DEFAULT_SETTINGS.tabBarOrientation,
    tabBarPosition: isTabBarPosition(merged.tabBarPosition)
      ? merged.tabBarPosition
      : DEFAULT_SETTINGS.tabBarPosition,
    showHiddenFiles: Boolean(merged.showHiddenFiles),
    showFileIcons: Boolean(merged.showFileIcons),
    showTerminalPaneAgentLabels:
      typeof merged.showTerminalPaneAgentLabels === "boolean"
        ? merged.showTerminalPaneAgentLabels
        : DEFAULT_SETTINGS.showTerminalPaneAgentLabels,
    webOpenMode: normalizeWebOpenMode(merged.webOpenMode),
    preferredBrowser: normalizeFontFamily(
      merged.preferredBrowser,
      DEFAULT_SETTINGS.preferredBrowser,
    ),
    defaultAgent: normalizeAgentKind(merged.defaultAgent, DEFAULT_SETTINGS.defaultAgent),
    agentLabelOverrides: normalizeAgentLabelOverrides(merged.agentLabelOverrides),
    agentCommands,
    agentInstallCommands,
    customColorScheme: normalizeCustomColorScheme(merged.customColorScheme),
    ghosttyTheme: settings?.ghosttyTheme ?? null,
  };
}

function makeId(prefix: string): string {
  return `${prefix}-${crypto.randomUUID?.() ?? Math.random().toString(36).slice(2)}`;
}

export function newCommandBlockId(): string {
  return makeId("cmd");
}

function makeTerminalLeaf(sessionId: string): TerminalLeafPane {
  return { type: "leaf", paneId: makeId("pane"), sessionId, sessionIds: [sessionId] };
}

function makeWorkspaceTab(
  workspaceId: string,
  title = "Terminal",
  terminalLayout: TerminalPaneNode | null = null,
  activeTerminalPaneId: string | null = null,
  maximizedTerminalPaneId: string | null = null,
): WorkspaceTab {
  const now = Date.now();
  return {
    id: makeId("workspace-tab"),
    workspaceId,
    title,
    terminalLayout,
    activeTerminalPaneId:
      activeTerminalPaneId && paneContainsPane(terminalLayout, activeTerminalPaneId)
        ? activeTerminalPaneId
        : firstLeaf(terminalLayout)?.paneId ?? null,
    maximizedTerminalPaneId:
      maximizedTerminalPaneId && paneContainsPane(terminalLayout, maximizedTerminalPaneId)
        ? maximizedTerminalPaneId
        : null,
    createdAt: now,
    updatedAt: now,
  };
}

function workspaceTabSessionId(tab: WorkspaceTab | null | undefined): string | null {
  if (!tab?.terminalLayout || !tab.activeTerminalPaneId) {
    return firstLeaf(tab?.terminalLayout ?? null)?.sessionId ?? null;
  }
  return (
    sessionIdForPane(tab.terminalLayout, tab.activeTerminalPaneId) ??
    firstLeaf(tab.terminalLayout)?.sessionId ??
    null
  );
}

function sessionIdsInTerminalLayout(node: TerminalPaneNode | null): Set<string> {
  const ids = new Set<string>();
  for (const leaf of terminalLeafOrder(node)) {
    for (const id of leafSessionIds(leaf)) {
      ids.add(id);
    }
  }
  return ids;
}

function tabContainsSession(tab: WorkspaceTab, sessionId: string): boolean {
  return sessionIdsInTerminalLayout(tab.terminalLayout).has(sessionId);
}

function activeWorkspaceTab(state: SessionStore): WorkspaceTab | null {
  const active =
    state.workspaceTabs.find(
      (tab) =>
        tab.id === state.activeWorkspaceTabId &&
        (!state.activeWorkspaceId || tab.workspaceId === state.activeWorkspaceId),
    ) ?? null;
  if (active) {
    return active;
  }
  return (
    state.workspaceTabs.find((tab) => tab.workspaceId === state.activeWorkspaceId) ??
    state.workspaceTabs[0] ??
    null
  );
}

function tabForSession(state: SessionStore, sessionId: string): WorkspaceTab | null {
  return state.workspaceTabs.find((tab) => tabContainsSession(tab, sessionId)) ?? null;
}

function mirrorWorkspaceTab(tab: WorkspaceTab | null): {
  terminalLayout: TerminalPaneNode | null;
  activeTerminalPaneId: string | null;
  maximizedTerminalPaneId: string | null;
  activeWorkspaceId: string | null;
  activeWorkspaceTabId: string | null;
  activeSessionId: string | null;
} {
  const activeSessionId = workspaceTabSessionId(tab);
  return {
    terminalLayout: tab?.terminalLayout ?? null,
    activeTerminalPaneId: tab?.activeTerminalPaneId ?? null,
    maximizedTerminalPaneId: tab?.maximizedTerminalPaneId ?? null,
    activeWorkspaceId: tab?.workspaceId ?? null,
    activeWorkspaceTabId: tab?.id ?? null,
    activeSessionId,
  };
}

function workspaceIdsForSessions(sessions: Session[]): Set<string> {
  return new Set(sessions.map((session) => session.workspaceId));
}

function pruneWorkspacesForSessions(
  workspaces: Workspace[],
  sessions: Session[],
): Workspace[] {
  const workspaceIds = workspaceIdsForSessions(sessions);
  return workspaces.filter((workspace) => workspaceIds.has(workspace.id));
}

function pruneWorkspaceScopedData(
  state: SessionStore,
  workspaceIds: Set<string>,
): Pick<
  SessionStore,
  | "selectedFile"
  | "openBuffers"
  | "activeBufferKey"
  | "closedBufferStack"
  | "dirtyBufferKeys"
  | "bufferAccessOrder"
> {
  const openBuffers = state.openBuffers.filter((buffer) =>
    workspaceIds.has(buffer.workspaceId),
  );
  const activeBufferKey =
    state.activeBufferKey &&
    openBuffers.some((buffer) => bufferKey(buffer) === state.activeBufferKey)
      ? state.activeBufferKey
      : null;
  const selectedFile =
    state.selectedFile && workspaceIds.has(state.selectedFile.workspaceId)
      ? state.selectedFile
      : activeBufferKey
        ? openBuffers.find((buffer) => bufferKey(buffer) === activeBufferKey) ?? null
        : null;

  return {
    selectedFile,
    openBuffers,
    activeBufferKey,
    closedBufferStack: state.closedBufferStack.filter((buffer) =>
      workspaceIds.has(buffer.workspaceId),
    ),
    dirtyBufferKeys: state.dirtyBufferKeys.filter((key) =>
      openBuffers.some((buffer) => bufferKey(buffer) === key),
    ),
    bufferAccessOrder: state.bufferAccessOrder.filter((key) =>
      openBuffers.some((buffer) => bufferKey(buffer) === key),
    ),
  };
}

function arrangementInWorkspaceIds(
  arrangement: Arrangement,
  workspaceIds: Set<string>,
): boolean {
  return Boolean(
    arrangement.workspaceId && workspaceIds.has(arrangement.workspaceId),
  );
}

function updateWorkspaceTab(
  tabs: WorkspaceTab[],
  tabId: string,
  patch: Partial<WorkspaceTab>,
): WorkspaceTab[] {
  const now = Date.now();
  return tabs.map((tab) =>
    tab.id === tabId
      ? {
          ...tab,
          ...patch,
          id: tab.id,
          workspaceId: patch.workspaceId ?? tab.workspaceId,
          updatedAt: now,
        }
      : tab,
  );
}

function reorderItemsById<T extends { id: string }>(
  items: T[],
  fromId: string,
  toId: string,
  before: boolean,
): T[] {
  if (fromId === toId) {
    return items;
  }
  const fromIndex = items.findIndex((item) => item.id === fromId);
  const toIndex = items.findIndex((item) => item.id === toId);
  if (fromIndex < 0 || toIndex < 0) {
    return items;
  }
  const next = [...items];
  const [moved] = next.splice(fromIndex, 1);
  const targetIndex = next.findIndex((item) => item.id === toId);
  next.splice(before ? targetIndex : targetIndex + 1, 0, moved);
  return next;
}

function normalizedSplitSizes(sizes: number[], childCount: number): number[] | undefined {
  if (sizes.length !== childCount) {
    return undefined;
  }
  const parsed = sizes.map((size) => Number(size));
  if (parsed.some((size) => !Number.isFinite(size) || size <= 0)) {
    return undefined;
  }
  const total = parsed.reduce((sum, size) => sum + size, 0);
  if (total <= 0) {
    return undefined;
  }
  return parsed.map((size) => Math.round((size / total) * 10_000) / 100);
}

function updateTerminalSplitSizesInLayout(
  node: TerminalPaneNode | null,
  splitPaneId: string,
  sizes: number[],
): TerminalPaneNode | null {
  if (!node) {
    return null;
  }
  if (node.type === "leaf") {
    return node;
  }
  if (node.paneId === splitPaneId) {
    const nextSizes = normalizedSplitSizes(sizes, node.children.length);
    return nextSizes ? { ...node, sizes: nextSizes } : node;
  }
  return {
    ...node,
    children: node.children.map((child) =>
      updateTerminalSplitSizesInLayout(child, splitPaneId, sizes),
    ) as TerminalPaneNode[],
  };
}

function replaceTerminalLeavesInOrder(
  node: TerminalPaneNode,
  leaves: TerminalLeafPane[],
  index: { value: number },
): TerminalPaneNode {
  if (node.type === "leaf") {
    return leaves[index.value++] ?? node;
  }
  return {
    ...node,
    children: node.children.map((child) =>
      replaceTerminalLeavesInOrder(child, leaves, index),
    ),
  };
}

function moveTerminalPaneInLayout(
  node: TerminalPaneNode | null,
  sourcePaneId: string,
  targetPaneId: string,
  position: TerminalPaneDropPosition,
): TerminalPaneNode | null {
  if (!node || sourcePaneId === targetPaneId) {
    return node;
  }
  const leaves = terminalLeafOrder(node);
  const source = leaves.find((leaf) => leaf.paneId === sourcePaneId);
  if (!source || !leaves.some((leaf) => leaf.paneId === targetPaneId)) {
    return node;
  }
  const reordered = leaves.filter((leaf) => leaf.paneId !== sourcePaneId);
  const targetIndex = reordered.findIndex((leaf) => leaf.paneId === targetPaneId);
  if (targetIndex < 0) {
    return node;
  }
  reordered.splice(position === "before" ? targetIndex : targetIndex + 1, 0, source);
  return replaceTerminalLeavesInOrder(node, reordered, { value: 0 });
}

function evenSplitSizes(count: number): number[] {
  if (count <= 0) {
    return [];
  }
  const raw = Math.round((100 / count) * 100) / 100;
  const sizes = Array.from({ length: count }, () => raw);
  sizes[sizes.length - 1] =
    Math.round((100 - raw * (count - 1)) * 100) / 100;
  return sizes;
}

function linearTerminalLayout(
  leaves: TerminalLeafPane[],
  direction: TerminalSplitDirection,
): TerminalPaneNode | null {
  if (leaves.length === 0) {
    return null;
  }
  if (leaves.length === 1) {
    return leaves[0];
  }
  return {
    type: "split",
    paneId: makeId("split"),
    direction,
    sizes: evenSplitSizes(leaves.length),
    children: leaves,
  };
}

function mainTerminalLayout(
  leaves: TerminalLeafPane[],
  direction: TerminalSplitDirection,
): TerminalPaneNode | null {
  if (leaves.length <= 2) {
    return linearTerminalLayout(leaves, direction);
  }
  const secondary = linearTerminalLayout(leaves.slice(1), direction);
  if (!secondary) {
    return leaves[0] ?? null;
  }
  return {
    type: "split",
    paneId: makeId("split"),
    direction,
    sizes: [55, 45],
    children: [leaves[0], secondary],
  };
}

function tiledTerminalLayout(
  leaves: TerminalLeafPane[],
  depth = 0,
): TerminalPaneNode | null {
  if (leaves.length === 0) {
    return null;
  }
  if (leaves.length === 1) {
    return leaves[0];
  }
  const midpoint = Math.ceil(leaves.length / 2);
  const left = tiledTerminalLayout(leaves.slice(0, midpoint), depth + 1);
  const right = tiledTerminalLayout(leaves.slice(midpoint), depth + 1);
  if (!left || !right) {
    return left ?? right;
  }
  return {
    type: "split",
    paneId: makeId("split"),
    direction: depth % 2 === 0 ? "vertical" : "horizontal",
    sizes: [50, 50],
    children: [left, right],
  };
}

function applyTerminalLayoutPresetToNode(
  node: TerminalPaneNode | null,
  preset: TerminalLayoutPreset,
): TerminalPaneNode | null {
  const leaves = terminalLeafOrder(node);
  if (leaves.length <= 1) {
    return node;
  }
  switch (preset) {
    case "even-horizontal":
      return linearTerminalLayout(leaves, "vertical");
    case "even-vertical":
      return linearTerminalLayout(leaves, "horizontal");
    case "main-horizontal":
      return mainTerminalLayout(leaves, "vertical");
    case "main-vertical":
      return mainTerminalLayout(leaves, "horizontal");
    case "tiled":
      return tiledTerminalLayout(leaves);
  }
}

function ensureWorkspaceHasTab(
  state: SessionStore,
  workspaceId: string,
): { tabs: WorkspaceTab[]; tab: WorkspaceTab } {
  const existing =
    state.workspaceTabs.find((tab) => tab.id === state.activeWorkspaceTabId && tab.workspaceId === workspaceId) ??
    state.workspaceTabs.find((tab) => tab.workspaceId === workspaceId);
  if (existing) {
    return { tabs: state.workspaceTabs, tab: existing };
  }
  const tab = makeWorkspaceTab(workspaceId);
  return { tabs: [...state.workspaceTabs, tab], tab };
}

function activeTabOrWorkspaceTabForSession(
  state: SessionStore,
  session: Session,
): { tabs: WorkspaceTab[]; tab: WorkspaceTab } {
  const existing = tabForSession(state, session.id);
  if (existing) {
    return { tabs: state.workspaceTabs, tab: existing };
  }
  if (state.workspaceTabs.length === 0 && state.terminalLayout) {
    const tab = makeWorkspaceTab(
      session.workspaceId,
      "Terminal",
      state.terminalLayout,
      state.activeTerminalPaneId,
      state.maximizedTerminalPaneId,
    );
    return { tabs: [tab], tab };
  }
  const active = activeWorkspaceTab(state);
  if (active?.workspaceId === session.workspaceId) {
    return { tabs: state.workspaceTabs, tab: active };
  }
  return ensureWorkspaceHasTab(state, session.workspaceId);
}

function pruneWorkspaceTabs(
  tabs: WorkspaceTab[],
  liveSessionIds: Set<string>,
): WorkspaceTab[] {
  return tabs.map((tab) => {
    const terminalLayout = pruneTerminalLayout(tab.terminalLayout, liveSessionIds);
    return {
      ...tab,
      terminalLayout,
      activeTerminalPaneId:
        tab.activeTerminalPaneId && paneContainsPane(terminalLayout, tab.activeTerminalPaneId)
          ? tab.activeTerminalPaneId
          : firstLeaf(terminalLayout)?.paneId ?? null,
      maximizedTerminalPaneId:
        tab.maximizedTerminalPaneId &&
        paneContainsPane(terminalLayout, tab.maximizedTerminalPaneId)
          ? tab.maximizedTerminalPaneId
          : null,
    };
  });
}

function synthesizeWorkspaceTabs(
  workspaces: Workspace[],
  sessions: Session[],
  legacyLayout: TerminalPaneNode | null,
  legacyActivePaneId: string | null,
  legacyMaximizedPaneId: string | null,
): WorkspaceTab[] {
  const tabs: WorkspaceTab[] = [];
  for (const workspace of workspaces) {
    const workspaceSessionIds = new Set(
      sessions
        .filter(
          (session) =>
            session.workspaceId === workspace.id && sessionHasRestorableTerminal(session),
        )
        .map((session) => session.id),
    );
    if (workspaceSessionIds.size === 0) {
      continue;
    }
    const terminalLayout =
      pruneTerminalLayout(legacyLayout, workspaceSessionIds) ??
      makeTerminalLeaf([...workspaceSessionIds][0]);
    tabs.push(
      makeWorkspaceTab(
        workspace.id,
        "Terminal",
        terminalLayout,
        legacyActivePaneId,
        legacyMaximizedPaneId,
      ),
    );
  }
  return tabs;
}

function normalizeWorkspaceTabs(
  value: unknown,
  workspaceIds: Set<string>,
): WorkspaceTab[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const tabs: WorkspaceTab[] = [];
  const seen = new Set<string>();
  for (const item of value) {
    if (!isRecord(item)) {
      continue;
    }
    const workspaceId =
      typeof item.workspaceId === "string" ? item.workspaceId.trim() : "";
    if (!workspaceId || !workspaceIds.has(workspaceId)) {
      continue;
    }
    const id =
      typeof item.id === "string" && item.id.trim()
        ? item.id.trim()
        : makeId("workspace-tab");
    if (seen.has(id)) {
      continue;
    }
    seen.add(id);
    const title =
      typeof item.title === "string" && item.title.trim()
        ? item.title.trim().slice(0, 80)
        : "Terminal";
    const terminalLayout = isRecord(item.terminalLayout)
      ? (item.terminalLayout as unknown as TerminalPaneNode)
      : null;
    const activeTerminalPaneId =
      typeof item.activeTerminalPaneId === "string"
        ? item.activeTerminalPaneId
        : null;
    const maximizedTerminalPaneId =
      typeof item.maximizedTerminalPaneId === "string"
        ? item.maximizedTerminalPaneId
        : null;
    const tab = makeWorkspaceTab(
      workspaceId,
      title,
      terminalLayout,
      activeTerminalPaneId,
      maximizedTerminalPaneId,
    );
    tabs.push({
      ...tab,
      id,
      createdAt: typeof item.createdAt === "number" ? item.createdAt : tab.createdAt,
      updatedAt: typeof item.updatedAt === "number" ? item.updatedAt : tab.updatedAt,
    });
  }
  return tabs;
}

function paneIdForSession(
  node: TerminalPaneNode | null,
  sessionId: string,
): string | null {
  if (!node) {
    return null;
  }
  if (node.type === "leaf") {
    return leafSessionIds(node).includes(sessionId) ? node.paneId : null;
  }
  for (const child of node.children) {
    const paneId = paneIdForSession(child, sessionId);
    if (paneId) {
      return paneId;
    }
  }
  return null;
}

function paneContainsPane(node: TerminalPaneNode | null, paneId: string): boolean {
  if (!node) {
    return false;
  }
  if (node.paneId === paneId) {
    return true;
  }
  return node.type === "split"
    ? node.children.some((child) => paneContainsPane(child, paneId))
    : false;
}

function sessionIdForPane(node: TerminalPaneNode | null, paneId: string): string | null {
  if (!node) {
    return null;
  }
  if (node.type === "leaf") {
    return node.paneId === paneId ? node.sessionId : null;
  }
  for (const child of node.children) {
    const sessionId = sessionIdForPane(child, paneId);
    if (sessionId) {
      return sessionId;
    }
  }
  return null;
}

function firstLeaf(node: TerminalPaneNode | null): TerminalLeafPane | null {
  if (!node) {
    return null;
  }
  if (node.type === "leaf") {
    return node;
  }
  for (const child of node.children) {
    const leaf = firstLeaf(child);
    if (leaf) {
      return leaf;
    }
  }
  return null;
}

function findPaneNode(
  node: TerminalPaneNode | null,
  paneId: string | null,
): TerminalPaneNode | null {
  if (!node || !paneId) {
    return null;
  }
  if (node.paneId === paneId) {
    return node;
  }
  if (node.type === "split") {
    for (const child of node.children) {
      const found = findPaneNode(child, paneId);
      if (found) {
        return found;
      }
    }
  }
  return null;
}

function terminalLeafOrder(node: TerminalPaneNode | null): TerminalLeafPane[] {
  if (!node) {
    return [];
  }
  if (node.type === "leaf") {
    return [node];
  }
  return node.children.flatMap(terminalLeafOrder);
}

export function terminalPaneNodeForId(
  node: TerminalPaneNode | null,
  paneId: string | null,
): TerminalPaneNode | null {
  return findPaneNode(node, paneId);
}

function insertTerminalSplit(
  node: TerminalPaneNode,
  targetPaneId: string,
  direction: TerminalSplitDirection,
  newLeaf: TerminalLeafPane,
): TerminalPaneNode {
  if (node.type === "leaf") {
    if (node.paneId !== targetPaneId) {
      return node;
    }
    return {
      type: "split",
      paneId: makeId("split"),
      direction,
      sizes: [50, 50],
      children: [node, newLeaf],
    };
  }
  return {
    ...node,
    children: node.children.map((child) =>
      insertTerminalSplit(child, targetPaneId, direction, newLeaf),
    ),
  };
}

function replacePaneSession(
  node: TerminalPaneNode | null,
  oldSessionId: string,
  newSessionId: string,
): TerminalPaneNode | null {
  if (!node) {
    return null;
  }
  if (node.type === "leaf") {
    const ids = leafSessionIds(node);
    if (!ids.includes(oldSessionId)) {
      return node;
    }
    const nextIds = ids.map((id) => (id === oldSessionId ? newSessionId : id));
    const sessionId = node.sessionId === oldSessionId ? newSessionId : node.sessionId;
    return { ...node, sessionId, sessionIds: nextIds };
  }
  return {
    ...node,
    children: node.children.map((child) =>
      replacePaneSession(child, oldSessionId, newSessionId),
    ) as TerminalPaneNode[],
  };
}

function removePaneSession(
  node: TerminalPaneNode | null,
  sessionId: string,
): TerminalPaneNode | null {
  if (!node) {
    return null;
  }
  if (node.type === "leaf") {
    const ids = leafSessionIds(node);
    if (!ids.includes(sessionId)) {
      return node;
    }
    const remaining = ids.filter((id) => id !== sessionId);
    if (remaining.length === 0) {
      return null;
    }
    const active = node.sessionId === sessionId ? remaining[0] : node.sessionId;
    return { ...node, sessionId: active, sessionIds: remaining };
  }
  const children = node.children
    .map((child) => removePaneSession(child, sessionId))
    .filter(Boolean) as TerminalPaneNode[];
  if (children.length === 0) {
    return null;
  }
  if (children.length === 1) {
    return children[0];
  }
  return { ...node, children };
}

export function pruneTerminalLayout(
  node: TerminalPaneNode | null | undefined,
  liveSessionIds: Set<string>,
): TerminalPaneNode | null {
  if (!node) {
    return null;
  }
  if (node.type === "leaf") {
    const ids = leafSessionIds(node).filter((id) => liveSessionIds.has(id));
    if (ids.length === 0) {
      return null;
    }
    const active = liveSessionIds.has(node.sessionId) ? node.sessionId : ids[0];
    return { ...node, sessionId: active, sessionIds: ids };
  }
  const children = node.children
    .map((child) => pruneTerminalLayout(child, liveSessionIds))
    .filter(Boolean) as TerminalPaneNode[];
  if (children.length === 0) {
    return null;
  }
  if (children.length === 1) {
    return children[0];
  }
  return { ...node, children };
}

function mapTerminalLayoutSessions(
  node: TerminalPaneNode | null,
  sessionIds: Map<string, string>,
): TerminalPaneNode | null {
  if (!node) {
    return null;
  }
  if (node.type === "leaf") {
    const mappedIds = leafSessionIds(node)
      .map((id) => sessionIds.get(id))
      .filter((id): id is string => Boolean(id));
    if (mappedIds.length === 0) {
      return null;
    }
    const activeMapped = sessionIds.get(node.sessionId) ?? mappedIds[0];
    return { ...node, sessionId: activeMapped, sessionIds: mappedIds };
  }
  const children = node.children
    .map((child) => mapTerminalLayoutSessions(child, sessionIds))
    .filter(Boolean) as TerminalPaneNode[];
  if (children.length === 0) {
    return null;
  }
  if (children.length === 1) {
    return children[0];
  }
  return { ...node, children };
}

function attachTabToPane(
  node: TerminalPaneNode,
  targetPaneId: string,
  sessionId: string,
): TerminalPaneNode {
  if (node.type === "leaf") {
    if (node.paneId !== targetPaneId) {
      return node;
    }
    const ids = leafSessionIds(node);
    if (ids.includes(sessionId)) {
      return { ...node, sessionId };
    }
    return { ...node, sessionId, sessionIds: [...ids, sessionId] };
  }
  return {
    ...node,
    children: node.children.map((child) =>
      attachTabToPane(child, targetPaneId, sessionId),
    ),
  };
}

function setActiveTabInPane(
  node: TerminalPaneNode,
  paneId: string,
  sessionId: string,
): TerminalPaneNode {
  if (node.type === "leaf") {
    if (node.paneId !== paneId) {
      return node;
    }
    const ids = leafSessionIds(node);
    if (!ids.includes(sessionId)) {
      return node;
    }
    return { ...node, sessionId };
  }
  return {
    ...node,
    children: node.children.map((child) =>
      setActiveTabInPane(child, paneId, sessionId),
    ),
  };
}

function normalizeArrangementSessions(value: unknown): ArrangementSession[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const sessions: ArrangementSession[] = [];
  for (const item of value) {
    if (!isRecord(item) || !isRecord(item.launch)) {
      continue;
    }
    const agent = normalizeAgentKind(item.agent, "shell");
    const command =
      typeof item.launch.command === "string" ? item.launch.command : "";
    const cwd =
      typeof item.launch.cwd === "string" && item.launch.cwd.trim()
        ? item.launch.cwd
        : null;
    const shellMode =
      agent === "shell" ? normalizeTerminalShellMode(item.launch.shellMode) : undefined;
    sessions.push({
      sessionId:
        typeof item.sessionId === "string" && item.sessionId.trim()
          ? item.sessionId
          : makeId("arrangement-session"),
      title:
        typeof item.title === "string" && item.title.trim()
          ? item.title.trim()
          : AGENT_LABELS[agent],
      agent,
      workspaceId:
        typeof item.workspaceId === "string" && item.workspaceId.trim()
          ? item.workspaceId.trim()
          : "",
      launch: {
        command,
        args: normalizeStringList(item.launch.args),
        cwd,
        env: normalizeEnvPairs(item.launch.env),
        ...(shellMode ? { shellMode } : {}),
      },
    });
  }
  return sessions;
}

function normalizeArrangements(value: unknown): Arrangement[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const arrangements: Arrangement[] = [];
  const seen = new Set<string>();
  for (const item of value) {
    if (!isRecord(item)) {
      continue;
    }
    const id =
      typeof item.id === "string" && item.id.trim()
        ? item.id.trim()
        : makeId("arrangement");
    if (seen.has(id)) {
      continue;
    }
    seen.add(id);
    arrangements.push({
      id,
      name:
        typeof item.name === "string" && item.name.trim()
          ? item.name.trim().slice(0, 80)
          : "Arrangement",
      createdAt: typeof item.createdAt === "number" ? item.createdAt : Date.now(),
      updatedAt: typeof item.updatedAt === "number" ? item.updatedAt : Date.now(),
      workspaceId:
        typeof item.workspaceId === "string" && item.workspaceId.trim()
          ? item.workspaceId.trim()
          : undefined,
      terminalLayout: isRecord(item.terminalLayout)
        ? (item.terminalLayout as unknown as TerminalPaneNode)
        : null,
      activeTerminalPaneId:
        typeof item.activeTerminalPaneId === "string"
          ? item.activeTerminalPaneId
          : null,
      maximizedTerminalPaneId:
        typeof item.maximizedTerminalPaneId === "string"
          ? item.maximizedTerminalPaneId
          : null,
      selectedFile: isRecord(item.selectedFile)
        ? (item.selectedFile as unknown as MainSelection)
        : null,
      activeSidebarView: normalizeWorkspaceSidebarView(item.activeSidebarView),
      sessions: normalizeArrangementSessions(item.sessions),
    });
  }
  return arrangements;
}

export function sessionAttentionState(
  session: Session,
  now = Date.now(),
): SessionAttentionState {
  const dismissedAt = session.attentionDismissedAt ?? 0;
  if (session.status === "stale") {
    return "stale";
  }
  if (session.status === "awaiting-approval" || session.pendingApprovals.length > 0) {
    return "needs-approval";
  }
  if (
    session.lastTrigger &&
    (session.lastTrigger.actions.includes("badge") ||
      session.lastTrigger.actions.includes("attention")) &&
    session.lastTrigger.timestamp > dismissedAt
  ) {
    return "triggered";
  }
  if (
    session.status === "error" ||
    (session.lastExitCode !== null &&
      session.lastExitCode !== undefined &&
      session.lastExitCode !== 0 &&
      (session.lastCommand?.endedAt ?? now) > dismissedAt)
  ) {
    return "failed";
  }
  if (session.status === "completed" && session.createdAt > dismissedAt) {
    return "exited";
  }
  if (
    session.status === "running" &&
    now - session.createdAt > 2 * 60 * 60 * 1000 &&
    session.createdAt > dismissedAt
  ) {
    return "stale";
  }
  return session.status === "running" ? "running" : "idle";
}

export function sessionHasRestorableTerminal(session: Session): boolean {
  return session.status !== "stale";
}

export function sessionCanHandoff(session: Session): boolean {
  return session.status === "running" || session.status === "awaiting-approval";
}

export function sessionNeedsAttention(session: Session, now = Date.now()): boolean {
  return ["needs-approval", "triggered", "failed", "exited"].includes(
    sessionAttentionState(session, now),
  );
}

export function attentionSessions(
  sessions: Session[],
  now = Date.now(),
): Session[] {
  return sessions.filter((session) => sessionNeedsAttention(session, now));
}

export function detectSessionPreview(text: string): SessionPreview | null {
  const plain = text.replace(/\x1b\].*?(?:\x07|\x1b\\)/g, "");
  const urlMatch = plain.match(
    /\bhttps?:\/\/(?:localhost|127\.0\.0\.1|\[::1\]|0\.0\.0\.0):(\d{2,5})(?:\/[^\s'"<>)]*)?/i,
  );
  const portMatch =
    urlMatch ??
    plain.match(
      /\b(?:localhost|127\.0\.0\.1|0\.0\.0\.0):(\d{2,5})(?:\/[^\s'"<>)]*)?/i,
    );
  if (!portMatch) {
    return null;
  }
  const raw = portMatch[0];
  const port = Number(portMatch[1]);
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    return null;
  }
  const url = raw.startsWith("http")
    ? raw.replace("0.0.0.0", "127.0.0.1")
    : `http://${raw.replace("0.0.0.0", "127.0.0.1")}`;
  return {
    url,
    port,
    label: `:${port}`,
    lastSeenAt: Date.now(),
  };
}

function appendEvent(
  events: SessionEvent[],
  event: Omit<SessionEvent, "id" | "timestamp">,
): SessionEvent[] {
  return [
    ...events.slice(-199),
    {
      ...event,
      id: makeId("event"),
      timestamp: Date.now(),
    },
  ];
}

function mergeCommandBlocks(
  current: CommandBlock[],
  incoming: CommandBlock[],
  limit = 300,
): CommandBlock[] {
  const byId = new Map<string, CommandBlock>();
  for (const block of [...current, ...incoming]) {
    byId.set(block.id, block);
  }
  return [...byId.values()]
    .sort((a, b) => b.startedAt - a.startedAt)
    .slice(0, limit);
}

function appendTranscript(
  transcript: SessionTranscript | null | undefined,
  text: string,
): SessionTranscript {
  const combined = `${transcript?.text ?? ""}${text}`;
  return {
    text: combined.slice(-SESSION_TRANSCRIPT_LIMIT),
    updatedAt: Date.now(),
  };
}

function snapshot(state: SessionStore): PersistedState {
  return {
    sessions: state.sessions,
    workspaces: state.workspaces,
    workspaceTabs: state.workspaceTabs,
    activeWorkspaceId: state.activeWorkspaceId,
    activeWorkspaceTabId: state.activeWorkspaceTabId,
    terminalLayout: state.terminalLayout,
    activeTerminalPaneId: state.activeTerminalPaneId,
    maximizedTerminalPaneId: state.maximizedTerminalPaneId,
    arrangements: state.arrangements,
    activeSidebarView: state.activeSidebarView,
    sessionEvents: state.sessionEvents,
    openBuffers: state.openBuffers,
    activeBufferKey: state.activeBufferKey,
    bufferAccessOrder: state.bufferAccessOrder,
    closedBufferStack: state.closedBufferStack,
    settings: state.settings,
  };
}

function persistLater() {
  window.clearTimeout(persistTimer);
  persistTimer = window.setTimeout(() => {
    void persistNow();
  }, 80);
}

export async function persistNow(): Promise<void> {
  try {
    const store = await getStore();
    const state = snapshot(useSessionStore.getState());
    await store.set("sessions", state.sessions);
    await store.set("workspaces", state.workspaces);
    await store.set("workspaceTabs", state.workspaceTabs);
    await store.set("activeWorkspaceId", state.activeWorkspaceId);
    await store.set("activeWorkspaceTabId", state.activeWorkspaceTabId);
    await store.set("terminalLayout", state.terminalLayout);
    await store.set("activeTerminalPaneId", state.activeTerminalPaneId);
    await store.set("maximizedTerminalPaneId", state.maximizedTerminalPaneId);
    await store.set("arrangements", state.arrangements);
    await store.set("activeSidebarView", state.activeSidebarView);
    await store.set("sessionEvents", state.sessionEvents);
    await store.set("openBuffers", state.openBuffers);
    await store.set("activeBufferKey", state.activeBufferKey);
    await store.set("bufferAccessOrder", state.bufferAccessOrder);
    await store.set("closedBufferStack", state.closedBufferStack);
    await store.set("settings", state.settings);
    await store.save();
  } catch (error) {
    console.warn("failed to persist session store", error);
  }
}

export const useSessionStore = create<SessionStore>((set) => ({
  hydrated: false,
  sessions: [],
  activeSessionId: null,
  terminalLayout: null,
  activeTerminalPaneId: null,
  maximizedTerminalPaneId: null,
  workspaceTabs: [],
  activeWorkspaceId: null,
  activeWorkspaceTabId: null,
  arrangements: [],
  activeSidebarView: "files",
  workspaces: [],
  selectedFile: null,
  openBuffers: [],
  activeBufferKey: null,
  bufferAccessOrder: [],
  closedBufferStack: [],
  dirtyBufferKeys: [],
  sessionEvents: [],
  commandBlocks: [],
  activeCommandBlocks: {},
  settings: DEFAULT_SETTINGS,
  setHydrated: (hydrated) => set({ hydrated }),
  setActiveSession: (id) => {
    set((state) => {
      if (!id) {
        return {
          activeSessionId: null,
          activeTerminalPaneId: null,
          selectedFile: null,
        };
      }
      const targetSession = state.sessions.find((session) => session.id === id);
      const targetTab = tabForSession(state, id);
      if (!targetSession || !targetTab) {
        return {
          activeSessionId: id,
          activeTerminalPaneId:
            paneIdForSession(state.terminalLayout, id) ?? state.activeTerminalPaneId,
          selectedFile: null,
        };
      }
      const activeTerminalPaneId =
        paneIdForSession(targetTab.terminalLayout, id) ??
        targetTab.activeTerminalPaneId;
      const workspaceTabs = updateWorkspaceTab(state.workspaceTabs, targetTab.id, {
        activeTerminalPaneId,
      });
      return {
        ...mirrorWorkspaceTab({
          ...targetTab,
          activeTerminalPaneId,
        }),
        workspaceTabs,
        activeSessionId: id,
        selectedFile: null,
      };
    });
    persistLater();
  },
  setActiveTerminalPane: (paneId) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      const sessionId = paneId && tab ? sessionIdForPane(tab.terminalLayout, paneId) : null;
      const tabs =
        tab && paneId
          ? updateWorkspaceTab(state.workspaceTabs, tab.id, {
              activeTerminalPaneId: paneId,
            })
          : state.workspaceTabs;
      return {
        workspaceTabs: tabs,
        activeTerminalPaneId: paneId,
        activeSessionId: sessionId ?? state.activeSessionId,
        selectedFile: null,
      };
    });
    persistLater();
  },
  setMaximizedTerminalPane: (paneId) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      const nextPaneId =
        paneId && tab && paneContainsPane(tab.terminalLayout, paneId) ? paneId : null;
      return {
        workspaceTabs: tab
          ? updateWorkspaceTab(state.workspaceTabs, tab.id, {
              maximizedTerminalPaneId: nextPaneId,
            })
          : state.workspaceTabs,
        maximizedTerminalPaneId: nextPaneId,
      };
    });
    persistLater();
  },
  toggleMaximizedTerminalPane: (paneId) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      const targetPaneId = paneId ?? state.activeTerminalPaneId;
      const next =
        tab &&
        targetPaneId &&
        tab.maximizedTerminalPaneId !== targetPaneId &&
        paneContainsPane(tab.terminalLayout, targetPaneId)
          ? targetPaneId
          : null;
      return {
        workspaceTabs: tab
          ? updateWorkspaceTab(state.workspaceTabs, tab.id, {
              maximizedTerminalPaneId: next,
            })
          : state.workspaceTabs,
        maximizedTerminalPaneId: next,
      };
    });
    persistLater();
  },
  focusRelativeTerminalPane: (delta) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      const leaves = terminalLeafOrder(tab?.terminalLayout ?? null);
      if (leaves.length === 0) {
        return {};
      }
      const currentIndex = Math.max(
        leaves.findIndex((leaf) => leaf.paneId === state.activeTerminalPaneId),
        0,
      );
      const next = leaves[(currentIndex + delta + leaves.length) % leaves.length];
      const tabs = tab
        ? updateWorkspaceTab(state.workspaceTabs, tab.id, {
            activeTerminalPaneId: next.paneId,
          })
        : state.workspaceTabs;
      return {
        workspaceTabs: tabs,
        activeTerminalPaneId: next.paneId,
        activeSessionId: next.sessionId,
        selectedFile: null,
      };
    });
    persistLater();
  },
  focusTerminalPaneIndex: (index) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      const leaves = terminalLeafOrder(tab?.terminalLayout ?? null);
      const target = leaves[Math.trunc(index) - 1];
      if (!target) {
        return {};
      }
      const tabs = tab
        ? updateWorkspaceTab(state.workspaceTabs, tab.id, {
            activeTerminalPaneId: target.paneId,
          })
        : state.workspaceTabs;
      return {
        workspaceTabs: tabs,
        activeTerminalPaneId: target.paneId,
        activeSessionId: target.sessionId,
        selectedFile: null,
      };
    });
    persistLater();
  },
  focusRelativeAttentionSession: (delta) => {
    set((state) => {
      const sessions = attentionSessions(state.sessions);
      if (sessions.length === 0) {
        return {};
      }
      const currentIndex = Math.max(
        sessions.findIndex((session) => session.id === state.activeSessionId),
        0,
      );
      const next = sessions[(currentIndex + delta + sessions.length) % sessions.length];
      const targetTab = tabForSession(state, next.id);
      const activeTerminalPaneId =
        targetTab && paneIdForSession(targetTab.terminalLayout, next.id)
          ? paneIdForSession(targetTab.terminalLayout, next.id)
          : state.activeTerminalPaneId;
      return {
        ...(targetTab
          ? mirrorWorkspaceTab({
              ...targetTab,
              activeTerminalPaneId,
            })
          : {}),
        activeSessionId: next.id,
        activeTerminalPaneId,
        selectedFile: null,
      };
    });
    persistLater();
  },
  setActiveWorkspace: (workspaceId) => {
    set((state) => {
      if (!workspaceId) {
        return {
          activeWorkspaceId: null,
          activeWorkspaceTabId: null,
          terminalLayout: null,
          activeTerminalPaneId: null,
          maximizedTerminalPaneId: null,
          activeSessionId: null,
          selectedFile: null,
        };
      }
      const { tabs, tab } = ensureWorkspaceHasTab(state, workspaceId);
      return {
        workspaceTabs: tabs,
        ...mirrorWorkspaceTab(tab),
        selectedFile: null,
      };
    });
    persistLater();
  },
  setActiveWorkspaceTab: (workspaceId, tabId) => {
    set((state) => {
      const tab = state.workspaceTabs.find(
        (item) => item.workspaceId === workspaceId && item.id === tabId,
      );
      if (!tab) {
        return {};
      }
      return {
        ...mirrorWorkspaceTab(tab),
        selectedFile: null,
      };
    });
    persistLater();
  },
  createWorkspaceTab: (workspaceId, title) => {
    const tab = makeWorkspaceTab(workspaceId, title?.trim() || "Terminal");
    set((state) => ({
      workspaceTabs: [...state.workspaceTabs, tab],
      ...mirrorWorkspaceTab(tab),
      selectedFile: null,
    }));
    persistLater();
    return tab.id;
  },
  reorderWorkspaces: (fromId, toId, before) => {
    set((state) => ({
      workspaces: reorderItemsById(state.workspaces, fromId, toId, before),
    }));
    persistLater();
  },
  toggleWorkspaceCollapsed: (workspaceId) => {
    set((state) => ({
      workspaces: state.workspaces.map((workspace) =>
        workspace.id === workspaceId
          ? { ...workspace, collapsed: !workspace.collapsed }
          : workspace,
      ),
    }));
    persistLater();
  },
  reorderWorkspaceTab: (workspaceId, fromTabId, toTabId, before) => {
    set((state) => {
      const workspaceTabs = state.workspaceTabs.filter(
        (tab) => tab.workspaceId === workspaceId,
      );
      const reordered = reorderItemsById(workspaceTabs, fromTabId, toTabId, before);
      const nextTabs = state.workspaceTabs.map((tab) => {
        if (tab.workspaceId !== workspaceId) {
          return tab;
        }
        return reordered.shift() ?? tab;
      });
      return { workspaceTabs: nextTabs };
    });
    persistLater();
  },
  focusRelativeWorkspace: (delta) => {
    set((state) => {
      if (state.workspaces.length === 0) {
        return {};
      }
      const currentIndex = Math.max(
        state.workspaces.findIndex(
          (workspace) => workspace.id === state.activeWorkspaceId,
        ),
        0,
      );
      const workspace =
        state.workspaces[
          (currentIndex + delta + state.workspaces.length) % state.workspaces.length
        ];
      const { tabs, tab } = ensureWorkspaceHasTab(state, workspace.id);
      return {
        workspaceTabs: tabs,
        ...mirrorWorkspaceTab(tab),
        selectedFile: null,
      };
    });
    persistLater();
  },
  focusWorkspaceIndex: (index) => {
    set((state) => {
      const workspace = state.workspaces[Math.trunc(index) - 1];
      if (!workspace) {
        return {};
      }
      const { tabs, tab } = ensureWorkspaceHasTab(state, workspace.id);
      return {
        workspaceTabs: tabs,
        ...mirrorWorkspaceTab(tab),
        selectedFile: null,
      };
    });
    persistLater();
  },
  focusRelativeWorkspaceTab: (delta) => {
    set((state) => {
      const workspaceId = state.activeWorkspaceId;
      if (!workspaceId) {
        return {};
      }
      const tabs = state.workspaceTabs.filter((tab) => tab.workspaceId === workspaceId);
      if (tabs.length === 0) {
        return {};
      }
      const currentIndex = Math.max(
        tabs.findIndex((tab) => tab.id === state.activeWorkspaceTabId),
        0,
      );
      const tab = tabs[(currentIndex + delta + tabs.length) % tabs.length];
      return {
        ...mirrorWorkspaceTab(tab),
        selectedFile: null,
      };
    });
    persistLater();
  },
  focusWorkspaceTabIndex: (index) => {
    set((state) => {
      const workspaceId = state.activeWorkspaceId;
      if (!workspaceId) {
        return {};
      }
      const tabs = state.workspaceTabs.filter((tab) => tab.workspaceId === workspaceId);
      const tab = tabs[Math.trunc(index) - 1];
      if (!tab) {
        return {};
      }
      return {
        ...mirrorWorkspaceTab(tab),
        selectedFile: null,
      };
    });
    persistLater();
  },
  updateTerminalSplitSizes: (splitPaneId, sizes) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      if (!tab?.terminalLayout) {
        const terminalLayout = updateTerminalSplitSizesInLayout(
          state.terminalLayout,
          splitPaneId,
          sizes,
        );
        return { terminalLayout };
      }
      const terminalLayout = updateTerminalSplitSizesInLayout(
        tab.terminalLayout,
        splitPaneId,
        sizes,
      );
      const workspaceTabs = updateWorkspaceTab(state.workspaceTabs, tab.id, {
        terminalLayout,
      });
      return {
        workspaceTabs,
        terminalLayout,
      };
    });
    persistLater();
  },
  moveTerminalPane: (sourcePaneId, targetPaneId, position) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      const baseLayout = tab?.terminalLayout ?? state.terminalLayout;
      const terminalLayout = moveTerminalPaneInLayout(
        baseLayout,
        sourcePaneId,
        targetPaneId,
        position,
      );
      if (terminalLayout === baseLayout) {
        return {};
      }
      const activeTerminalPaneId =
        state.activeTerminalPaneId &&
        paneContainsPane(terminalLayout, state.activeTerminalPaneId)
          ? state.activeTerminalPaneId
          : firstLeaf(terminalLayout)?.paneId ?? null;
      const maximizedTerminalPaneId =
        state.maximizedTerminalPaneId &&
        paneContainsPane(terminalLayout, state.maximizedTerminalPaneId)
          ? state.maximizedTerminalPaneId
          : null;
      const workspaceTabs = tab
        ? updateWorkspaceTab(state.workspaceTabs, tab.id, {
            terminalLayout,
            activeTerminalPaneId,
            maximizedTerminalPaneId,
          })
        : state.workspaceTabs;
      return {
        workspaceTabs,
        terminalLayout,
        activeTerminalPaneId,
        maximizedTerminalPaneId,
        activeSessionId: activeTerminalPaneId
          ? sessionIdForPane(terminalLayout, activeTerminalPaneId)
          : state.activeSessionId,
        selectedFile: null,
      };
    });
    persistLater();
  },
  applyTerminalLayoutPreset: (preset) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      const baseLayout = tab?.terminalLayout ?? state.terminalLayout;
      const terminalLayout = applyTerminalLayoutPresetToNode(baseLayout, preset);
      if (terminalLayout === baseLayout) {
        return {};
      }
      const activeTerminalPaneId =
        state.activeTerminalPaneId &&
        paneContainsPane(terminalLayout, state.activeTerminalPaneId)
          ? state.activeTerminalPaneId
          : firstLeaf(terminalLayout)?.paneId ?? null;
      const maximizedTerminalPaneId =
        state.maximizedTerminalPaneId &&
        paneContainsPane(terminalLayout, state.maximizedTerminalPaneId)
          ? state.maximizedTerminalPaneId
          : null;
      const workspaceTabs = tab
        ? updateWorkspaceTab(state.workspaceTabs, tab.id, {
            terminalLayout,
            activeTerminalPaneId,
            maximizedTerminalPaneId,
          })
        : state.workspaceTabs;
      return {
        workspaceTabs,
        terminalLayout,
        activeTerminalPaneId,
        maximizedTerminalPaneId,
        activeSessionId: activeTerminalPaneId
          ? sessionIdForPane(terminalLayout, activeTerminalPaneId)
          : state.activeSessionId,
        selectedFile: null,
      };
    });
    persistLater();
  },
  setActiveSidebarView: (view) => {
    set({ activeSidebarView: view });
    persistLater();
  },
  addSession: (session, placement) => {
    set((state) => {
      const ensured = activeTabOrWorkspaceTabForSession(state, session);
      const currentTab = ensured.tab;
      const currentLayout = currentTab.terminalLayout;
      let terminalLayout: TerminalPaneNode | null;
      let resolvedPaneId: string;
      if (placement?.type === "tab" && currentLayout) {
        terminalLayout = attachTabToPane(
          currentLayout,
          placement.targetPaneId,
          session.id,
        );
        resolvedPaneId = placement.targetPaneId;
      } else if (placement?.type === "split" && currentLayout && placement.direction) {
        const leaf = makeTerminalLeaf(session.id);
        terminalLayout = insertTerminalSplit(
          currentLayout,
          placement.targetPaneId,
          placement.direction,
          leaf,
        );
        resolvedPaneId = leaf.paneId;
      } else {
        const leaf = makeTerminalLeaf(session.id);
        terminalLayout = leaf;
        resolvedPaneId = leaf.paneId;
      }
      const workspaceTabs = updateWorkspaceTab(ensured.tabs, currentTab.id, {
        terminalLayout,
        activeTerminalPaneId: resolvedPaneId,
        maximizedTerminalPaneId: currentTab.maximizedTerminalPaneId
          ? resolvedPaneId
          : null,
      });
      const maximizedTerminalPaneId = currentTab.maximizedTerminalPaneId
        ? resolvedPaneId
        : null;
      return {
        sessions: [
          ...state.sessions.filter((existing) => existing.id !== session.id),
          session,
        ],
        workspaceTabs,
        activeWorkspaceId: session.workspaceId,
        activeWorkspaceTabId: currentTab.id,
        terminalLayout,
        activeTerminalPaneId: resolvedPaneId,
        maximizedTerminalPaneId,
        activeSessionId: session.id,
        selectedFile: null,
        sessionEvents: appendEvent(state.sessionEvents, {
          type: placement ? "session-split" : "session-started",
          workspaceId: session.workspaceId,
          sessionId: session.id,
          agent: session.agent,
          summary: placement
            ? placement.type === "tab"
              ? `Tab ${session.title}`
              : `Split ${session.title}`
            : `Started ${session.title}`,
          metadata: placement
            ? {
                targetPaneId: placement.targetPaneId,
                direction: placement.direction ?? null,
                placementType: placement.type,
              }
            : undefined,
        }),
      };
    });
    persistLater();
  },
  setActivePaneSession: (paneId, sessionId) => {
    set((state) => {
      const tab = activeWorkspaceTab(state);
      if (!tab?.terminalLayout) {
        if (!state.terminalLayout) return {};
        const nextLayout = setActiveTabInPane(state.terminalLayout, paneId, sessionId);
        return {
          terminalLayout: nextLayout,
          activeTerminalPaneId: paneId,
          activeSessionId: sessionId,
          selectedFile: null,
        };
      }
      const nextLayout = setActiveTabInPane(tab.terminalLayout, paneId, sessionId);
      const workspaceTabs = updateWorkspaceTab(state.workspaceTabs, tab.id, {
        terminalLayout: nextLayout,
        activeTerminalPaneId: paneId,
      });
      return {
        workspaceTabs,
        terminalLayout: nextLayout,
        activeTerminalPaneId: paneId,
        activeSessionId: sessionId,
        activeWorkspaceId: tab.workspaceId,
        activeWorkspaceTabId: tab.id,
        selectedFile: null,
      };
    });
    persistLater();
  },
  updateSession: (id, patch) => {
    set((state) => ({
      sessions: state.sessions.map((session) =>
        session.id === id ? { ...session, ...patch } : session,
      ),
    }));
    persistLater();
  },
  replaceSession: (id, replacement) => {
    set((state) => {
      if (state.workspaceTabs.length === 0) {
        const terminalLayout = replacePaneSession(
          state.terminalLayout,
          id,
          replacement.id,
        );
        const activeTerminalPaneId =
          state.activeSessionId === id
            ? paneIdForSession(terminalLayout, replacement.id) ??
              state.activeTerminalPaneId
            : state.activeTerminalPaneId;
        return {
          sessions: state.sessions.map((session) =>
            session.id === id ? replacement : session,
          ),
          terminalLayout,
          activeTerminalPaneId,
          activeSessionId:
            state.activeSessionId === id ? replacement.id : state.activeSessionId,
        };
      }
      const workspaceTabs = state.workspaceTabs.map((tab) => ({
        ...tab,
        terminalLayout: replacePaneSession(tab.terminalLayout, id, replacement.id),
      }));
      const nextState = { ...state, workspaceTabs };
      const targetTab =
        tabForSession(nextState, replacement.id) ?? activeWorkspaceTab(nextState);
      const mirrored = targetTab
        ? mirrorWorkspaceTab({
            ...targetTab,
            activeTerminalPaneId:
              state.activeSessionId === id
                ? paneIdForSession(targetTab.terminalLayout, replacement.id) ??
                  targetTab.activeTerminalPaneId
                : targetTab.activeTerminalPaneId,
          })
        : mirrorWorkspaceTab(null);
      return {
        sessions: state.sessions.map((session) =>
          session.id === id ? replacement : session,
        ),
        workspaceTabs,
        ...mirrored,
        activeSessionId:
          state.activeSessionId === id
            ? replacement.id
            : mirrored.activeSessionId ?? state.activeSessionId,
      };
    });
    persistLater();
  },
	  removeSession: (id) => {
	    set((state) => {
	      const sessions = state.sessions.filter((session) => session.id !== id);
	      const workspaces = pruneWorkspacesForSessions(state.workspaces, sessions);
	      const workspaceIds = new Set(workspaces.map((workspace) => workspace.id));
	      const scopedData = pruneWorkspaceScopedData(state, workspaceIds);
	      const liveSessionIds = new Set(sessions.map((session) => session.id));
	      const activeCommandBlocks = Object.fromEntries(
	        Object.entries(state.activeCommandBlocks).filter(([sessionId]) =>
	          liveSessionIds.has(sessionId),
	        ),
	      );
	      if (state.workspaceTabs.length === 0) {
	        const terminalLayout = removePaneSession(state.terminalLayout, id);
	        const activeTerminalPaneId =
	          state.activeTerminalPaneId &&
	          paneContainsPane(terminalLayout, state.activeTerminalPaneId)
	            ? state.activeTerminalPaneId
	            : firstLeaf(terminalLayout)?.paneId ?? null;
	        const activeSessionId =
	          state.activeSessionId === id
	            ? activeTerminalPaneId
	              ? sessionIdForPane(terminalLayout, activeTerminalPaneId)
	              : null
	            : state.activeSessionId;
	        const activeWorkspaceId = activeSessionId
	          ? sessions.find((session) => session.id === activeSessionId)?.workspaceId ?? null
	          : workspaces[0]?.id ?? null;
	        return {
	          sessions,
	          workspaces,
	          activeCommandBlocks,
	          ...scopedData,
	          terminalLayout,
	          activeTerminalPaneId,
	          maximizedTerminalPaneId:
	            state.maximizedTerminalPaneId &&
	            paneContainsPane(terminalLayout, state.maximizedTerminalPaneId)
	              ? state.maximizedTerminalPaneId
	              : null,
	          activeSessionId,
	          activeWorkspaceId,
	          activeWorkspaceTabId: null,
	        };
	      }
	      const workspaceTabs = state.workspaceTabs
	        .map((tab) => {
	          const terminalLayout = removePaneSession(tab.terminalLayout, id);
          return {
            ...tab,
            terminalLayout,
            activeTerminalPaneId:
              tab.activeTerminalPaneId && paneContainsPane(terminalLayout, tab.activeTerminalPaneId)
                ? tab.activeTerminalPaneId
                : firstLeaf(terminalLayout)?.paneId ?? null,
            maximizedTerminalPaneId:
              tab.maximizedTerminalPaneId &&
              paneContainsPane(terminalLayout, tab.maximizedTerminalPaneId)
                ? tab.maximizedTerminalPaneId
                : null,
          };
	        })
	        .filter(
	          (tab) =>
	            workspaceIds.has(tab.workspaceId) &&
	            (tab.terminalLayout !== null ||
	              tab.id === state.activeWorkspaceTabId ||
	              sessions.some((session) => session.workspaceId === tab.workspaceId)),
	        );
	      const nextState = { ...state, sessions, workspaceTabs };
	      const activeRemoved = state.activeSessionId === id;
	      const activeTab = activeWorkspaceTab(nextState);
	      const mirrored = mirrorWorkspaceTab(activeTab);
	      return {
	        sessions,
	        workspaces,
	        workspaceTabs,
	        activeCommandBlocks,
	        ...scopedData,
	        arrangements: state.arrangements.filter((arrangement) =>
	          arrangementInWorkspaceIds(arrangement, workspaceIds),
	        ),
	        ...mirrored,
	        activeSessionId: activeRemoved
	          ? mirrored.activeSessionId
	          : state.activeSessionId,
	      };
    });
    persistLater();
  },
  clearSessionAttention: (id) => {
    set((state) => ({
      sessions: state.sessions.map((session) =>
        session.id === id
          ? {
              ...session,
              attentionDismissedAt: Date.now(),
              lastTrigger: null,
              lastExitCode:
                session.status === "completed" || session.status === "error"
                  ? session.lastExitCode
                  : session.lastExitCode ?? null,
            }
          : session,
      ),
    }));
    persistLater();
  },
  appendSessionTranscript: (id, text) => {
    if (!text) {
      return;
    }
    set((state) => ({
      sessions: state.sessions.map((session) =>
        session.id === id
          ? {
              ...session,
              transcript: appendTranscript(session.transcript, text),
            }
          : session,
      ),
    }));
    persistLater();
  },
  startCommandBlock: (block) => {
    set((state) => ({
      activeCommandBlocks: {
        ...state.activeCommandBlocks,
        [block.sessionId]: block,
      },
      sessions: state.sessions.map((session) =>
        session.id === block.sessionId
          ? {
              ...session,
              lastCommandBlockId: block.id,
            }
          : session,
      ),
    }));
  },
  finishCommandBlock: (block) => {
    set((state) => {
      const { [block.sessionId]: _finished, ...activeCommandBlocks } =
        state.activeCommandBlocks;
      return {
        activeCommandBlocks,
        commandBlocks: mergeCommandBlocks(state.commandBlocks, [block]),
        sessions: state.sessions.map((session) =>
          session.id === block.sessionId
            ? {
                ...session,
                lastCommandBlockId: block.id,
              }
            : session,
        ),
        sessionEvents: appendEvent(state.sessionEvents, {
          type: "command-block",
          workspaceId: block.workspaceId,
          sessionId: block.sessionId,
          agent: block.agent,
          summary: `${block.command} exited ${block.exitCode ?? "unknown"}`,
          metadata: {
            commandBlockId: block.id,
            status: block.status,
            exitCode: block.exitCode ?? null,
          },
        }),
      };
    });
    persistLater();
  },
  setCommandBlocks: (blocks) => {
    set((state) => ({
      commandBlocks: mergeCommandBlocks([], [
        ...state.commandBlocks,
        ...blocks,
      ]),
    }));
  },
  saveCurrentArrangement: (name) => {
    const state = useSessionStore.getState();
    const sessions = state.sessions
      .filter((session) => session.restart)
      .map<ArrangementSession>((session) => ({
        sessionId: session.id,
        title: session.title,
        agent: session.agent,
        workspaceId: session.workspaceId,
        launch: session.restart!,
      }));
    if (sessions.length === 0) {
      return null;
    }
    const now = Date.now();
    const activeSession = state.sessions.find(
      (session) => session.id === state.activeSessionId,
    );
    const id = makeId("arrangement");
    const arrangement: Arrangement = {
      id,
      name:
        name?.trim() ||
        `${activeSession?.title ?? "Onibi"} arrangement ${new Date(now).toLocaleString()}`,
      createdAt: now,
      updatedAt: now,
      workspaceId: activeSession?.workspaceId ?? sessions[0]?.workspaceId,
      terminalLayout: state.terminalLayout,
      activeTerminalPaneId: state.activeTerminalPaneId,
      maximizedTerminalPaneId: state.maximizedTerminalPaneId,
      selectedFile: state.selectedFile,
      openBuffers: state.openBuffers,
      activeBufferKey: state.activeBufferKey,
      activeSidebarView: state.activeSidebarView,
      sessions,
    };
    set((current) => ({
      arrangements: [arrangement, ...current.arrangements].slice(0, 50),
      sessionEvents: appendEvent(current.sessionEvents, {
        type: "arrangement-saved",
        workspaceId: arrangement.workspaceId,
        summary: `Saved ${arrangement.name}`,
      }),
    }));
    persistLater();
    return id;
  },
  deleteArrangement: (id) => {
    set((state) => ({
      arrangements: state.arrangements.filter((arrangement) => arrangement.id !== id),
    }));
    persistLater();
  },
  appendSessionEvent: (event) => {
    set((state) => ({ sessionEvents: appendEvent(state.sessionEvents, event) }));
    persistLater();
  },
  openWebUrl: (url, sessionId) => {
    const currentState = useSessionStore.getState();
    const openerSession = sessionId
      ? currentState.sessions.find((session) => session.id === sessionId)
      : currentState.sessions.find((session) => session.id === currentState.activeSessionId) ??
        null;
    const workspace = openerSession
      ? currentState.workspaces.find(
          (item) => item.id === openerSession.workspaceId,
        ) ?? null
      : null;
    const mode = currentState.settings.webOpenMode;
    const openInApp =
      mode === "in-app" ||
      (mode === "ask" && window.confirm(`Open ${url} inside Onibi?`));
    set((state) => {
      const targetWorkspaceId = workspace?.id ?? openerSession?.workspaceId ?? null;
      const targetTab = targetWorkspaceId
        ? state.workspaceTabs.find((tab) => tab.workspaceId === targetWorkspaceId) ?? null
        : null;
      return {
        sessionEvents: appendEvent(state.sessionEvents, {
          type: "web-opened",
          workspaceId: openerSession?.workspaceId,
          sessionId: openerSession?.id,
          agent: openerSession?.agent,
          summary: `Opened ${url}`,
          metadata: { url },
        }),
        selectedFile:
          openInApp && workspace
            ? {
                type: "web",
                workspaceId: workspace.id,
                workspaceRoot: workspace.path,
                path: url,
                name: url,
                url,
              }
            : state.selectedFile,
        ...(targetTab ? mirrorWorkspaceTab(targetTab) : {}),
        activeWorkspaceId: targetWorkspaceId ?? state.activeWorkspaceId,
      };
    });
    if (!openInApp) {
      window.open(url, "_blank", "noopener,noreferrer");
    }
    persistLater();
  },
	  setWorkspaces: (workspaces) => {
	    set((state) => {
	      const workspaceIds = new Set(workspaces.map((workspace) => workspace.id));
	      const sessions = state.sessions.filter((session) =>
	        workspaceIds.has(session.workspaceId),
	      );
	      const sessionIds = new Set(sessions.map((session) => session.id));
	      const scopedData = pruneWorkspaceScopedData(state, workspaceIds);
	      const workspaceTabs = pruneWorkspaceTabs(
	        state.workspaceTabs.filter((tab) => workspaceIds.has(tab.workspaceId)),
	        sessionIds,
	      );
	      for (const workspace of workspaces) {
	        if (!workspaceTabs.some((tab) => tab.workspaceId === workspace.id)) {
	          workspaceTabs.push(makeWorkspaceTab(workspace.id));
	        }
	      }
      const activeTab =
        workspaceTabs.find((tab) => tab.id === state.activeWorkspaceTabId) ??
        workspaceTabs.find((tab) => tab.workspaceId === state.activeWorkspaceId) ??
        workspaceTabs[0] ??
	        null;
	      return {
	        sessions,
	        workspaces,
	        workspaceTabs,
	        ...scopedData,
	        arrangements: state.arrangements.filter((arrangement) =>
	          arrangementInWorkspaceIds(arrangement, workspaceIds),
	        ),
	        activeCommandBlocks: Object.fromEntries(
	          Object.entries(state.activeCommandBlocks).filter(([sessionId]) =>
	            sessionIds.has(sessionId),
	          ),
	        ),
	        ...mirrorWorkspaceTab(activeTab),
	      };
	    });
	    persistLater();
	  },
  addWorkspace: (workspace) => {
    set((state) => {
      const workspaces = [
        ...state.workspaces.filter((existing) => existing.id !== workspace.id),
        workspace,
      ];
      const existingTab =
        state.workspaceTabs.find((tab) => tab.workspaceId === workspace.id) ?? null;
      const tab = existingTab ?? makeWorkspaceTab(workspace.id);
      const workspaceTabs = existingTab
        ? state.workspaceTabs
        : [...state.workspaceTabs, tab];
      const shouldActivate = state.activeWorkspaceId === null;
      return {
        workspaces,
        workspaceTabs,
        ...(shouldActivate
          ? mirrorWorkspaceTab(tab)
          : {
              activeWorkspaceId: state.activeWorkspaceId,
              activeWorkspaceTabId:
                state.activeWorkspaceTabId ??
                (state.activeWorkspaceId === workspace.id ? tab.id : null),
            }),
      };
    });
    persistLater();
  },
  removeWorkspace: (id) => {
    set((state) => {
      const remainingBuffers = state.openBuffers.filter(
        (buffer) => buffer.workspaceId !== id,
      );
      const activeStillOpen =
        state.activeBufferKey &&
        remainingBuffers.some((buffer) => bufferKey(buffer) === state.activeBufferKey);
      const nextActiveKey = activeStillOpen ? state.activeBufferKey : null;
      const nextSelected = nextActiveKey
        ? remainingBuffers.find((buffer) => bufferKey(buffer) === nextActiveKey) ?? null
        : null;
      const workspaces = state.workspaces.filter((workspace) => workspace.id !== id);
      const sessions = state.sessions.filter((session) => session.workspaceId !== id);
      const workspaceTabs = state.workspaceTabs.filter((tab) => tab.workspaceId !== id);
      const removedActiveWorkspace = state.activeWorkspaceId === id;
      const nextTab = removedActiveWorkspace
        ? workspaceTabs[0] ?? null
        : activeWorkspaceTab({ ...state, workspaceTabs });
      const mirrored = mirrorWorkspaceTab(nextTab);
      return {
        workspaces,
        selectedFile: nextSelected,
        openBuffers: remainingBuffers,
        activeBufferKey: nextActiveKey,
        closedBufferStack: state.closedBufferStack.filter(
          (buffer) => buffer.workspaceId !== id,
        ),
        dirtyBufferKeys: state.dirtyBufferKeys.filter((key) =>
          remainingBuffers.some((buffer) => bufferKey(buffer) === key),
        ),
        bufferAccessOrder: state.bufferAccessOrder.filter((key) =>
          remainingBuffers.some((buffer) => bufferKey(buffer) === key),
        ),
        sessions,
        workspaceTabs,
        arrangements: state.arrangements.filter(
          (arrangement) => arrangement.workspaceId !== id,
        ),
        ...(removedActiveWorkspace ? mirrored : {}),
        activeSessionId: removedActiveWorkspace
          ? mirrored.activeSessionId
          : state.sessions.some(
              (session) =>
                session.workspaceId === id && session.id === state.activeSessionId,
            )
            ? mirrored.activeSessionId
            : state.activeSessionId,
      };
    });
    persistLater();
  },
  selectFile: (file) => {
    set((state) => {
      if (file === null) {
        // hide editor surface, keep tabs in store
        const tab = activeWorkspaceTab(state);
        return {
          ...(tab ? mirrorWorkspaceTab(tab) : {}),
          selectedFile: null,
          activeBufferKey: null,
        };
      }
      const key = bufferKey(file);
      const existing = state.openBuffers.findIndex(
        (buffer) => bufferKey(buffer) === key,
      );
      const nextOpenBuffers =
        existing >= 0
          ? state.openBuffers.map((buffer, index) =>
              index === existing ? file : buffer,
            )
          : [...state.openBuffers, file];
      const nextAccessOrder = touchAccessOrder(state.bufferAccessOrder, key);
      const limited = applyEditorOpenLimit(
        nextOpenBuffers,
        key,
        state.dirtyBufferKeys,
        nextAccessOrder,
        state.closedBufferStack,
        state.settings.editorOpenLimit,
        state.settings.closedBufferHistoryLimit,
      );
      const evictedKeys = new Set(
        nextOpenBuffers
          .filter((b) => !limited.openBuffers.some((kept) => bufferKey(kept) === bufferKey(b)))
          .map((b) => bufferKey(b)),
      );
      const prunedAccessOrder = nextAccessOrder.filter((k) => !evictedKeys.has(k));
      const ensured = ensureWorkspaceHasTab(state, file.workspaceId);
      const targetTab = ensured.tab;
      return {
        workspaceTabs: ensured.tabs,
        ...mirrorWorkspaceTab(targetTab),
        activeWorkspaceId: file.workspaceId,
        selectedFile: file,
        openBuffers: limited.openBuffers,
        closedBufferStack: limited.closedBufferStack,
        activeBufferKey: key,
        bufferAccessOrder: prunedAccessOrder,
        sessionEvents:
          file.type !== "web"
            ? appendEvent(state.sessionEvents, {
                type: "file-opened",
                workspaceId: file.workspaceId,
                summary: `Opened ${file.path}`,
                metadata: { path: file.path, kind: file.type ?? "file" },
              })
            : state.sessionEvents,
      };
    });
    persistLater();
  },
  setActiveBuffer: (key) => {
    set((state) => {
      const target = state.openBuffers.find(
        (buffer) => bufferKey(buffer) === key,
      );
      if (!target) return {};
      return {
        selectedFile: target,
        activeBufferKey: key,
        bufferAccessOrder: touchAccessOrder(state.bufferAccessOrder, key),
      };
    });
    persistLater();
  },
  closeBuffer: (key) => {
    set((state) => {
      const limit = state.settings.closedBufferHistoryLimit;
      const idx = state.openBuffers.findIndex(
        (buffer) => bufferKey(buffer) === key,
      );
      if (idx < 0) return {};
      const closing = state.openBuffers[idx];
      const openBuffers = state.openBuffers.filter((_, i) => i !== idx);
      const closedBufferStack = capClosedStack(
        [
          closing,
          ...state.closedBufferStack.filter((buffer) => bufferKey(buffer) !== key),
        ],
        limit,
      );
      const dirtyBufferKeys = state.dirtyBufferKeys.filter((k) => k !== key);
      const bufferAccessOrder = state.bufferAccessOrder.filter((k) => k !== key);
      if (state.activeBufferKey !== key) {
        return { openBuffers, closedBufferStack, dirtyBufferKeys, bufferAccessOrder };
      }
      // active buffer being closed: pick neighbor (prefer right, fall back left)
      const next =
        openBuffers[idx] ??
        openBuffers[idx - 1] ??
        null;
      return {
        openBuffers,
        closedBufferStack,
        dirtyBufferKeys,
        bufferAccessOrder: next
          ? touchAccessOrder(bufferAccessOrder, bufferKey(next))
          : bufferAccessOrder,
        activeBufferKey: next ? bufferKey(next) : null,
        selectedFile: next,
      };
    });
    persistLater();
  },
  closeOtherBuffers: (key) => {
    set((state) => {
      const limit = state.settings.closedBufferHistoryLimit;
      const target = state.openBuffers.find(
        (buffer) => bufferKey(buffer) === key,
      );
      if (!target) return {};
      const closing = state.openBuffers.filter(
        (buffer) => bufferKey(buffer) !== key,
      );
      const closedBufferStack = capClosedStack(
        [
          ...closing.reverse(),
          ...state.closedBufferStack.filter(
            (buffer) => !closing.some((c) => bufferKey(c) === bufferKey(buffer)),
          ),
        ],
        limit,
      );
      return {
        openBuffers: [target],
        closedBufferStack,
        dirtyBufferKeys: state.dirtyBufferKeys.filter((k) => k === key),
        bufferAccessOrder: [key],
        activeBufferKey: key,
        selectedFile: target,
      };
    });
    persistLater();
  },
  closeAllBuffers: () => {
    set((state) => {
      const limit = state.settings.closedBufferHistoryLimit;
      const closedBufferStack = capClosedStack(
        [
          ...[...state.openBuffers].reverse(),
          ...state.closedBufferStack.filter(
            (buffer) =>
              !state.openBuffers.some((c) => bufferKey(c) === bufferKey(buffer)),
          ),
        ],
        limit,
      );
      return {
        openBuffers: [],
        closedBufferStack,
        dirtyBufferKeys: [],
        bufferAccessOrder: [],
        activeBufferKey: null,
        selectedFile: null,
      };
    });
    persistLater();
  },
  reopenClosedBuffer: () => {
    let opened = false;
    set((state) => {
      if (state.closedBufferStack.length === 0) return {};
      const [next, ...rest] = state.closedBufferStack;
      const key = bufferKey(next);
      const exists = state.openBuffers.some(
        (buffer) => bufferKey(buffer) === key,
      );
      const nextOpenBuffers = exists
        ? state.openBuffers
        : [...state.openBuffers, next];
      const nextAccessOrder = touchAccessOrder(state.bufferAccessOrder, key);
      const limited = applyEditorOpenLimit(
        nextOpenBuffers,
        key,
        state.dirtyBufferKeys,
        nextAccessOrder,
        rest,
        state.settings.editorOpenLimit,
        state.settings.closedBufferHistoryLimit,
      );
      const evictedKeys = new Set(
        nextOpenBuffers
          .filter((b) => !limited.openBuffers.some((kept) => bufferKey(kept) === bufferKey(b)))
          .map((b) => bufferKey(b)),
      );
      const prunedAccessOrder = nextAccessOrder.filter((k) => !evictedKeys.has(k));
      opened = true;
      return {
        openBuffers: limited.openBuffers,
        closedBufferStack: limited.closedBufferStack,
        activeBufferKey: key,
        bufferAccessOrder: prunedAccessOrder,
        selectedFile: next,
      };
    });
    if (opened) {
      persistLater();
    }
    return opened;
  },
  setBufferDirty: (key, dirty) => {
    set((state) => {
      const has = state.dirtyBufferKeys.includes(key);
      if (dirty && !has) {
        return { dirtyBufferKeys: [...state.dirtyBufferKeys, key] };
      }
      if (!dirty && has) {
        return {
          dirtyBufferKeys: state.dirtyBufferKeys.filter((k) => k !== key),
        };
      }
      return {};
    });
  },
  reorderBuffer: (fromKey, toKey, before) => {
    set((state) => {
      const fromIdx = state.openBuffers.findIndex(
        (buffer) => bufferKey(buffer) === fromKey,
      );
      const toIdx = state.openBuffers.findIndex(
        (buffer) => bufferKey(buffer) === toKey,
      );
      if (fromIdx < 0 || toIdx < 0 || fromIdx === toIdx) return {};
      const moved = state.openBuffers[fromIdx];
      const remaining = state.openBuffers.filter((_, i) => i !== fromIdx);
      const adjustedTo = toIdx > fromIdx ? toIdx - 1 : toIdx;
      const insertAt = before ? adjustedTo : adjustedTo + 1;
      const openBuffers = [
        ...remaining.slice(0, insertAt),
        moved,
        ...remaining.slice(insertAt),
      ];
      return { openBuffers };
    });
    persistLater();
  },
  updateSettings: (patch) => {
    set((state) => {
      const nextSettings = mergeSettings({ ...state.settings, ...patch });
      // If a tab limit just got tighter, evict immediately.
      const limited = applyEditorOpenLimit(
        state.openBuffers,
        state.activeBufferKey,
        state.dirtyBufferKeys,
        state.bufferAccessOrder,
        state.closedBufferStack,
        nextSettings.editorOpenLimit,
        nextSettings.closedBufferHistoryLimit,
      );
      const evictedKeys = new Set(
        state.openBuffers
          .filter((b) => !limited.openBuffers.some((kept) => bufferKey(kept) === bufferKey(b)))
          .map((b) => bufferKey(b)),
      );
      return {
        settings: nextSettings,
        openBuffers: limited.openBuffers,
        closedBufferStack: capClosedStack(
          limited.closedBufferStack,
          nextSettings.closedBufferHistoryLimit,
        ),
        bufferAccessOrder: state.bufferAccessOrder.filter(
          (k) => !evictedKeys.has(k),
        ),
      };
    });
    persistLater();
  },
  updateAgentCommand: (agent, command) => {
    set((state) => ({
      settings: {
        ...state.settings,
        agentCommands: {
          ...state.settings.agentCommands,
          [agent]: command,
        },
      },
    }));
    persistLater();
  },
}));

async function loadDaemonSessions(): Promise<PtySessionMetadata[]> {
  try {
    return await ptySessions();
  } catch {
    const liveIds = await ptyList().catch(() => []);
    const now = Date.now();
    return liveIds.map((id) => ({
      id,
      paneId: id,
      status: "working",
      lifecycle: "running",
      rows: 30,
      cols: 100,
      createdAt: now,
      updatedAt: now,
    }));
  }
}

function mergeDaemonWorkspaces(
  stored: Workspace[],
  daemonSessions: PtySessionMetadata[],
  discoverDaemonWorkspaces: boolean,
): Workspace[] {
  const byId = new Map(stored.map((workspace) => [workspace.id, workspace]));
  if (!discoverDaemonWorkspaces) {
    return Array.from(byId.values());
  }
  for (const session of daemonSessions.filter(
    (session) => session.lifecycle === "running",
  )) {
    const workspace = workspaceFromDaemonSession(session);
    if (workspace && !byId.has(workspace.id)) {
      byId.set(workspace.id, workspace);
    }
  }
  return Array.from(byId.values());
}

function sessionRestoreTimestamp(session: Session): number {
  return session.createdAt || 0;
}

function sessionIsLive(session: Session): boolean {
  return (
    session.status !== "stale" &&
    session.status !== "completed" &&
    session.status !== "error"
  );
}

function deterministicSessionOrder(a: Session, b: Session): number {
  const liveDelta = Number(sessionIsLive(a)) - Number(sessionIsLive(b));
  if (liveDelta !== 0) {
    return liveDelta;
  }
  const timestampDelta = sessionRestoreTimestamp(a) - sessionRestoreTimestamp(b);
  if (timestampDelta !== 0) {
    return timestampDelta;
  }
  return a.id.localeCompare(b.id);
}

function newestLiveTerminalSession(sessions: Session[]): Session | null {
  return (
    [...sessions]
      .filter(
        (session) => sessionHasRestorableTerminal(session) && sessionIsLive(session),
      )
      .sort(deterministicSessionOrder)
      .at(-1) ?? null
  );
}

function mergeDaemonSessionRecords(
  stored: Session[],
  daemonSessions: PtySessionMetadata[],
  daemonById: Map<string, PtySessionMetadata>,
  livePtys: Set<string>,
): Session[] {
  const merged = stored.map((session) => {
    const normalized = normalizeSessionLaunchMetadata(session);
    const daemon = daemonById.get(normalized.id);
    if (daemon) {
      return mergeDaemonSession(normalized, daemon);
    }
    const live = livePtys.has(normalized.id);
    return {
      ...normalized,
      status: live
        ? normalized.status === "stale"
          ? "running"
          : normalized.status
        : "stale",
      pendingApprovals: normalized.pendingApprovals ?? [],
    } satisfies Session;
  });
  const knownIds = new Set(merged.map((session) => session.id));
  for (const daemon of daemonSessions) {
    if (knownIds.has(daemon.id) || daemon.lifecycle === "stopped") {
      continue;
    }
    merged.push(sessionFromDaemonMetadata(daemon));
    knownIds.add(daemon.id);
  }
  return merged;
}

function normalizeSessionLaunchMetadata(session: Session): Session {
  if (
    session.agent !== "claude-code" ||
    session.restart?.command !== DEFAULT_AGENT_COMMANDS["claude-code"] ||
    session.restart.args[0] !== "code"
  ) {
    return session;
  }
  return {
    ...session,
    restart: {
      ...session.restart,
      args: session.restart.args.slice(1),
    },
  };
}

function mergeDaemonSession(session: Session, daemon: PtySessionMetadata): Session {
  return {
    ...session,
    name: daemon.name ?? session.name,
    agent: agentKindFromDaemon(daemon.agent) ?? session.agent,
    workspaceId: daemon.workspaceId ?? session.workspaceId,
    title: daemon.title ?? daemon.name ?? session.title,
    status: sessionStatusFromDaemon(daemon),
    createdAt: daemon.createdAt || session.createdAt,
    pendingApprovals: session.pendingApprovals ?? [],
    cwd: daemon.cwd ?? session.cwd,
    lastExitCode: daemon.exitCode ?? session.lastExitCode ?? null,
    restart: daemon.restart ?? session.restart ?? null,
    provider: daemon.provider ?? session.provider ?? null,
  };
}

function sessionFromDaemonMetadata(daemon: PtySessionMetadata): Session {
  const cwd = daemon.cwd ?? daemonWorkspacePath(daemon) ?? "/";
  const agent = agentKindFromDaemon(daemon.agent) ?? "shell";
  return {
    id: daemon.id,
    name: daemon.name ?? undefined,
    agent,
    workspaceId: daemon.workspaceId ?? workspaceIdForPath(cwd),
    title: daemon.title ?? daemon.name ?? sessionTitle(agent, workspaceFromPathSync(cwd)),
    status: sessionStatusFromDaemon(daemon),
    createdAt: daemon.createdAt || Date.now(),
    pendingApprovals: [],
    cwd,
    lastExitCode: daemon.exitCode ?? null,
    lastTrigger: null,
    lastCommandBlockId: null,
    transcript: null,
    restart: daemon.restart ?? null,
    provider: daemon.provider ?? null,
  };
}

function agentKindFromDaemon(agent: string | null | undefined): AgentKind | null {
  return AGENT_KINDS.includes(agent as AgentKind) ? (agent as AgentKind) : null;
}

function sessionStatusFromDaemon(daemon: PtySessionMetadata): SessionStatus {
  if (daemon.lifecycle !== "running") {
    return "stale";
  }
  switch (daemon.status) {
    case "blocked":
      return "awaiting-approval";
    case "done":
      return "completed";
    case "idle":
      return "idle";
    default:
      return "running";
  }
}

function workspaceFromDaemonSession(
  session: PtySessionMetadata,
): Workspace | null {
  const path = daemonWorkspacePath(session);
  if (!path) {
    return null;
  }
  return {
    id: session.workspaceId ?? workspaceIdForPath(path),
    path,
    name: pathBasename(path),
  };
}

function daemonWorkspacePath(session: PtySessionMetadata): string | null {
  if (session.cwd) {
    return session.cwd;
  }
  if (session.workspaceId?.startsWith("workspace:")) {
    return session.workspaceId.slice("workspace:".length);
  }
  return null;
}

function workspaceFromPathSync(path: string): Workspace {
  return {
    id: workspaceIdForPath(path),
    path,
    name: pathBasename(path),
  };
}

export async function hydrateSessionStore(): Promise<void> {
  const state = useSessionStore.getState();
  if (state.hydrated) {
    return;
  }
  try {
    const store = await getStore();
    const [
      sessions,
      workspaces,
      storedWorkspaceTabs,
      storedActiveWorkspaceId,
      storedActiveWorkspaceTabId,
      settings,
    ] = await Promise.all([
      store.get<Session[]>("sessions"),
      store.get<Workspace[]>("workspaces"),
      store.get<WorkspaceTab[]>("workspaceTabs"),
      store.get<string | null>("activeWorkspaceId"),
      store.get<string | null>("activeWorkspaceTabId"),
      store.get<Partial<AppSettings>>("settings"),
    ]);
    const [
      terminalLayout,
      activeTerminalPaneId,
      maximizedTerminalPaneId,
      arrangements,
      activeSidebarView,
      sessionEvents,
      openBuffers,
      activeBufferKey,
      closedBufferStack,
      bufferAccessOrder,
    ] = await Promise.all([
      store.get<TerminalPaneNode | null>("terminalLayout"),
      store.get<string | null>("activeTerminalPaneId"),
      store.get<string | null>("maximizedTerminalPaneId"),
      store.get<Arrangement[]>("arrangements"),
      store.get<WorkspaceSidebarView>("activeSidebarView"),
      store.get<SessionEvent[]>("sessionEvents"),
      store.get<MainSelection[]>("openBuffers"),
      store.get<string | null>("activeBufferKey"),
      store.get<MainSelection[]>("closedBufferStack"),
      store.get<string[]>("bufferAccessOrder"),
    ]);
    const tomlConfig = await readOnibiConfigTomlFile().catch(() => null);
    const daemonSessions = await loadDaemonSessions();
    const daemonById = new Map(daemonSessions.map((session) => [session.id, session]));
    const livePtys = new Set(
      daemonSessions
        .filter((session) => session.lifecycle === "running")
        .map((session) => session.id),
    );
    const configuredWorkspaceIds = new Set(
      (tomlConfig?.workspaces ?? []).map((workspace) => workspace.id),
    );
    const mergedSessions = mergeDaemonSessionRecords(
      sessions ?? [],
      daemonSessions,
      daemonById,
      livePtys,
    );
    const restoredSessionWorkspaceIds = workspaceIdsForSessions(mergedSessions);
    const workspaceSources = [
      ...(tomlConfig?.workspaces ?? []),
      ...(workspaces ?? []).filter(
        (workspace) =>
          !configuredWorkspaceIds.has(workspace.id) &&
          restoredSessionWorkspaceIds.has(workspace.id),
      ),
    ];
    const discoverDaemonWorkspaces =
      workspaces === undefined && (tomlConfig?.workspaces.length ?? 0) === 0;
    const restoredWorkspaces = mergeDaemonWorkspaces(
      workspaceSources,
      daemonSessions,
      discoverDaemonWorkspaces,
    );
    const workspaceIds = new Set(restoredWorkspaces.map((w) => w.id));
    const restoredSessions = mergedSessions
      .filter((session) => workspaceIds.has(session.workspaceId))
      .sort(deterministicSessionOrder);
    const restoredTerminalSessions = restoredSessions.filter((session) =>
      sessionHasRestorableTerminal(session),
    );
    const restoredTerminalIds = new Set(
      restoredTerminalSessions.map((session) => session.id),
    );
    const legacyPrunedLayout =
      pruneTerminalLayout(terminalLayout, restoredTerminalIds) ??
      (restoredTerminalSessions.length > 0
        ? makeTerminalLeaf(
            restoredTerminalSessions[restoredTerminalSessions.length - 1].id,
          )
        : null);
    const legacyActiveLeaf =
      (activeTerminalPaneId && paneContainsPane(legacyPrunedLayout, activeTerminalPaneId)
        ? activeTerminalPaneId
        : null) ?? firstLeaf(legacyPrunedLayout)?.paneId ?? null;
    const fallbackTerminalSessionId =
      newestLiveTerminalSession(restoredTerminalSessions)?.id ??
      restoredTerminalSessions[restoredTerminalSessions.length - 1]?.id ??
      null;
    const legacyActiveSessionId =
      legacyActiveLeaf && legacyPrunedLayout
        ? sessionIdForPane(legacyPrunedLayout, legacyActiveLeaf) ??
          fallbackTerminalSessionId
        : fallbackTerminalSessionId;
    const ghosttyTheme = await readGhosttyTheme().catch(() => null);
    const mergedSettings = applyGhosttyDefaults(
      mergeSettings({
        ...(settings ?? {}),
        ...(tomlConfig?.settings ?? {}),
      }),
      ghosttyTheme,
    );
    let restoredWorkspaceTabs = normalizeWorkspaceTabs(
      storedWorkspaceTabs,
      workspaceIds,
    );
    if (restoredWorkspaceTabs.length > 0) {
      restoredWorkspaceTabs = pruneWorkspaceTabs(
        restoredWorkspaceTabs,
        restoredTerminalIds,
      );
    } else {
      restoredWorkspaceTabs = synthesizeWorkspaceTabs(
        restoredWorkspaces,
        restoredSessions,
        legacyPrunedLayout,
        activeTerminalPaneId ?? legacyActiveLeaf,
        maximizedTerminalPaneId ?? null,
      );
    }
    const tabWorkspaceIds = new Set(
      restoredWorkspaceTabs.map((tab) => tab.workspaceId),
    );
    for (const workspace of restoredWorkspaces) {
      if (!tabWorkspaceIds.has(workspace.id)) {
        restoredWorkspaceTabs.push(makeWorkspaceTab(workspace.id));
        tabWorkspaceIds.add(workspace.id);
      }
    }
    const restoredBuffers = (openBuffers ?? []).filter((buffer) =>
      workspaceIds.has(buffer.workspaceId),
    );
    const restoredActiveKey =
      activeBufferKey &&
      restoredBuffers.some((buffer) => bufferKey(buffer) === activeBufferKey)
        ? activeBufferKey
        : null;
    const restoredSelected = restoredActiveKey
      ? restoredBuffers.find((buffer) => bufferKey(buffer) === restoredActiveKey) ?? null
      : null;
    const activeSessionWorkspaceId =
      restoredSessions.find((session) => session.id === legacyActiveSessionId)
        ?.workspaceId ?? null;
    const restoredActiveWorkspaceId =
      storedActiveWorkspaceId && workspaceIds.has(storedActiveWorkspaceId)
        ? storedActiveWorkspaceId
        : restoredSelected?.workspaceId ??
          activeSessionWorkspaceId ??
          restoredWorkspaceTabs[0]?.workspaceId ??
          restoredWorkspaces[0]?.id ??
          null;
    const restoredActiveWorkspaceTabId =
      storedActiveWorkspaceTabId &&
      restoredWorkspaceTabs.some(
        (tab) =>
          tab.id === storedActiveWorkspaceTabId &&
          (!restoredActiveWorkspaceId ||
            tab.workspaceId === restoredActiveWorkspaceId),
      )
        ? storedActiveWorkspaceTabId
        : restoredWorkspaceTabs.find(
            (tab) =>
              tab.workspaceId === restoredActiveWorkspaceId &&
              legacyActiveSessionId &&
              tabContainsSession(tab, legacyActiveSessionId),
          )?.id ??
          restoredWorkspaceTabs.find(
            (tab) => tab.workspaceId === restoredActiveWorkspaceId,
          )?.id ??
          restoredWorkspaceTabs[0]?.id ??
          null;
    const restoredActiveTab =
      restoredWorkspaceTabs.find((tab) => tab.id === restoredActiveWorkspaceTabId) ??
      null;
    const mirroredActiveTab = mirrorWorkspaceTab(restoredActiveTab);
    const restoredClosedStack = capClosedStack(
      (closedBufferStack ?? []).filter((buffer) =>
        workspaceIds.has(buffer.workspaceId),
      ),
      mergedSettings.closedBufferHistoryLimit,
    );
    useSessionStore.setState({
      sessions: restoredSessions,
      activeSessionId: mirroredActiveTab.activeSessionId,
      terminalLayout: mirroredActiveTab.terminalLayout,
      activeTerminalPaneId: mirroredActiveTab.activeTerminalPaneId,
      maximizedTerminalPaneId: mirroredActiveTab.maximizedTerminalPaneId,
      workspaceTabs: restoredWorkspaceTabs,
      activeWorkspaceId: restoredActiveWorkspaceId,
      activeWorkspaceTabId: restoredActiveWorkspaceTabId,
      arrangements: normalizeArrangements(arrangements),
      activeSidebarView: normalizeWorkspaceSidebarView(activeSidebarView),
      workspaces: restoredWorkspaces,
      sessionEvents: sessionEvents ?? [],
      openBuffers: restoredBuffers,
      activeBufferKey: restoredActiveKey,
      bufferAccessOrder: Array.isArray(bufferAccessOrder)
        ? bufferAccessOrder.filter((key) =>
            restoredBuffers.some((buffer) => bufferKey(buffer) === key),
          )
        : restoredActiveKey
          ? [restoredActiveKey]
          : [],
      closedBufferStack: restoredClosedStack,
      dirtyBufferKeys: [],
      selectedFile: restoredSelected,
      settings: mergedSettings,
      hydrated: true,
    });
  } catch (error) {
    console.warn("failed to hydrate session store", error);
    useSessionStore.setState({ hydrated: true });
  }
}

export function workspaceIdForPath(path: string): string {
  return `workspace:${path}`;
}

export function pathBasename(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const parts = trimmed.split("/");
  return parts[parts.length - 1] || trimmed || "/";
}

export async function workspaceFromPath(path: string): Promise<Workspace> {
  const info = await invoke<{ path: string; name: string }>("fs_workspace_info", {
    path,
  });
  return {
    id: workspaceIdForPath(info.path),
    path: info.path,
    name: info.name || pathBasename(info.path),
  };
}

export async function openWorkspacePath(path: string): Promise<Workspace> {
  const workspace = await workspaceFromPath(path);
  const store = useSessionStore.getState();
  store.addWorkspace(workspace);
  store.setActiveWorkspace(workspace.id);
  store.selectFile(null);
  return workspace;
}

export async function launchAgentInWorkspacePath(
  agent: AgentKind,
  path: string,
  prompt = "",
): Promise<PtyId> {
  const workspace = await openWorkspacePath(path);
  return spawnAgentSession(agent, workspace, prompt, null, { cwd: workspace.path });
}

export async function removeWorkspaceAndCloseSessions(
  workspaceId: string,
): Promise<void> {
  const state = useSessionStore.getState();
  const sessions = state.sessions.filter((session) => session.workspaceId === workspaceId);
  await Promise.all(
    sessions.map(async (session) => {
      await ptyKill(session.id).catch(() => undefined);
      if (session.agent !== "shell") {
        await stopAgentReview(session.id).catch(() => undefined);
      }
    }),
  );
  useSessionStore.getState().removeWorkspace(workspaceId);
}

export async function listWorkspaceDir(
  workspaceRoot: string,
  path: string,
): Promise<FsEntry[]> {
  return invoke<FsEntry[]>("fs_list_dir", { root: workspaceRoot, path });
}

export async function readWorkspaceFile(
  workspaceRoot: string,
  path: string,
): Promise<number[]> {
  return invoke<number[]>("fs_read_file", { root: workspaceRoot, path });
}

export async function readWorkspacePreviewFile(
  workspaceRoot: string,
  path: string,
): Promise<number[]> {
  return invoke<number[]>("fs_read_preview_file", { root: workspaceRoot, path });
}

export async function writeWorkspaceFile(
  workspaceRoot: string,
  path: string,
  data: Uint8Array,
): Promise<void> {
  await Promise.resolve(
    invoke("agent_review_note_human_write", {
      root: workspaceRoot,
      path,
    }),
  ).catch(() => undefined);
  await invoke("fs_write_file", {
    root: workspaceRoot,
    path,
    data: Array.from(data),
  });
}

export async function createWorkspaceFile(
  workspaceRoot: string,
  parent: string,
  name: string,
): Promise<FsEntry> {
  return invoke<FsEntry>("fs_create_file", { root: workspaceRoot, parent, name });
}

export async function createWorkspaceFileWithContents(
  workspaceRoot: string,
  parent: string,
  name: string,
  data: Uint8Array,
): Promise<FsEntry> {
  return invoke<FsEntry>("fs_create_file_with_contents", {
    root: workspaceRoot,
    parent,
    name,
    data: Array.from(data),
  });
}

export async function createWorkspaceDir(
  workspaceRoot: string,
  parent: string,
  name: string,
): Promise<FsEntry> {
  return invoke<FsEntry>("fs_create_dir", { root: workspaceRoot, parent, name });
}

export async function renameWorkspacePath(
  workspaceRoot: string,
  path: string,
  name: string,
): Promise<FsEntry> {
  return invoke<FsEntry>("fs_rename_path", { root: workspaceRoot, path, name });
}

export async function moveWorkspacePath(
  workspaceRoot: string,
  path: string,
  destination: string,
): Promise<FsEntry> {
  return invoke<FsEntry>("fs_move_path", {
    root: workspaceRoot,
    path,
    destination,
  });
}

export async function deleteWorkspacePath(
  workspaceRoot: string,
  path: string,
): Promise<void> {
  await invoke("fs_delete_path", { root: workspaceRoot, path });
}

export async function listLocalFontFamilies(): Promise<string[]> {
  const families = await invoke<string[]>("list_font_families");
  return Array.isArray(families) ? families : [];
}

export function selectedFileFromEntry(
  workspace: Workspace,
  entry: FsEntry,
): SelectedFile {
  return {
    workspaceId: workspace.id,
    workspaceRoot: workspace.path,
    path: entry.path,
    name: entry.name,
    size: entry.size,
  };
}

function normalizeWorkspaces(value: unknown): Workspace[] {
  if (!Array.isArray(value)) {
    return [];
  }
  const workspaces: Workspace[] = [];
  for (const item of value) {
    if (!isRecord(item) || typeof item.path !== "string" || !item.path.trim()) {
      continue;
    }
    const path = item.path.trim();
    const name =
      typeof item.name === "string" && item.name.trim()
        ? item.name.trim()
        : pathBasename(path);
    const id =
      typeof item.id === "string" && item.id.trim()
        ? item.id.trim()
        : workspaceIdForPath(path);
    workspaces.push({
      id,
      path,
      name,
      collapsed: item.collapsed === true,
    });
  }
  return workspaces;
}

export function getOnibiConfigSnapshot(): OnibiConfigExport {
  const state = useSessionStore.getState();
  return {
    version: 1,
    settings: state.settings,
    workspaces: state.workspaces,
  };
}

function tomlString(value: string): string {
  return JSON.stringify(value);
}

function tomlArray(values: string[]): string {
  return `[${values.map(tomlString).join(", ")}]`;
}

function tomlScalar(value: string | number | boolean): string {
  return typeof value === "string" ? tomlString(value) : String(value);
}

function settingLine(key: string, value: string | number | boolean): string {
  return `${key} = ${tomlScalar(value)}`;
}

function stripTomlComment(line: string): string {
  let quoted = false;
  let escaped = false;
  let result = "";
  for (const char of line) {
    if (escaped) {
      result += char;
      escaped = false;
      continue;
    }
    if (char === "\\" && quoted) {
      result += char;
      escaped = true;
      continue;
    }
    if (char === '"') {
      quoted = !quoted;
      result += char;
      continue;
    }
    if (char === "#" && !quoted) {
      break;
    }
    result += char;
  }
  return result.trim();
}

function parseTomlValue(raw: string): unknown {
  const value = raw.trim();
  if (value.startsWith("[") && value.endsWith("]")) {
    const inner = value.slice(1, -1).trim();
    if (!inner) {
      return [];
    }
    const parts: string[] = [];
    let quoted = false;
    let escaped = false;
    let current = "";
    for (const char of inner) {
      if (escaped) {
        current += char;
        escaped = false;
        continue;
      }
      if (char === "\\" && quoted) {
        current += char;
        escaped = true;
        continue;
      }
      if (char === '"') {
        quoted = !quoted;
        current += char;
        continue;
      }
      if (char === "," && !quoted) {
        parts.push(current.trim());
        current = "";
        continue;
      }
      current += char;
    }
    parts.push(current.trim());
    return parts.map(parseTomlValue);
  }
  if (value.startsWith('"') && value.endsWith('"')) {
    try {
      return JSON.parse(value) as string;
    } catch {
      return value.slice(1, -1);
    }
  }
  if (value === "true" || value === "false") {
    return value === "true";
  }
  const number = Number(value);
  return Number.isFinite(number) ? number : value;
}

function camelFromTomlKey(key: string): string {
  return key.replace(/_([a-z])/g, (_match, letter: string) => letter.toUpperCase());
}

function assignTomlSetting(
  settings: Record<string, unknown>,
  path: string,
  key: string,
  value: unknown,
  currentObject: Record<string, unknown> | null,
) {
  if (path === "settings") {
    settings[camelFromTomlKey(key)] = value;
  } else if (path === "ui") {
    const mappedKey =
      key === "tab_bar_position"
        ? "tabBarPosition"
        : key === "default_agent"
          ? "defaultAgent"
          : key === "new_pane_cwd"
            ? "newPaneCwd"
            : camelFromTomlKey(key);
    settings[mappedKey] = value;
  } else if (path === "terminal") {
    const mappedKey =
      key === "shell_integration"
        ? "terminalShellIntegration"
        : key === "font_family"
          ? "terminalFontFamily"
          : key === "font_size"
            ? "terminalFontSize"
            : key === "scrollback_lines"
              ? "terminalScrollbackLines"
              : key === "screen_reader_mode"
                ? "terminalScreenReaderMode"
                : key === "copy_format"
                  ? "terminalCopyFormat"
                  : key === "osc52_clipboard"
                    ? "terminalOsc52Clipboard"
                    : key === "transparent_background"
                      ? "terminalTransparentBackground"
                      : key === "inline_images"
                        ? "terminalInlineImages"
                        : camelFromTomlKey(key);
    settings[mappedKey] = value;
  } else if (path === "keybindings") {
    if (key === "prefix") {
      settings.keybindingPrefix = value;
    }
  } else if (path === "settings.agent_commands") {
    const target = (settings.agentCommands ??= {}) as Record<string, unknown>;
    target[key] = value;
  } else if (path === "settings.agent_install_commands") {
    const target = (settings.agentInstallCommands ??= {}) as Record<string, unknown>;
    target[key] = value;
  } else if (path === "settings.agent_label_overrides") {
    const target = (settings.agentLabelOverrides ??= {}) as Record<string, unknown>;
    target[key] = value;
  } else if (path === "settings.sound_agents") {
    const target = (settings.soundAgents ??= {}) as Record<string, unknown>;
    target[key] = value;
  } else if (path === "settings.custom_color_scheme") {
    const target = (settings.customColorScheme ??= {}) as Record<string, unknown>;
    target[camelFromTomlKey(key)] = value;
  } else if (path === "settings.custom_color_scheme.colors") {
    const scheme = (settings.customColorScheme ??= {}) as Record<string, unknown>;
    const target = (scheme.colors ??= {}) as Record<string, unknown>;
    target[camelFromTomlKey(key)] = value;
  } else if (currentObject) {
    currentObject[camelFromTomlKey(key)] = value;
  }
}

function pushTomlTable(
  settings: Record<string, unknown>,
  workspaces: Record<string, unknown>[],
  path: string,
): Record<string, unknown> | null {
  if (path === "workspaces") {
    const workspace: Record<string, unknown> = {};
    workspaces.push(workspace);
    return workspace;
  }
  const key =
    path === "settings.app_keybindings"
      ? "appKeybindings"
      : path === "keybindings.app"
        ? "appKeybindings"
        : path === "keybindings.command" ||
            path === "settings.command_keybindings" ||
            path === "settings.custom_command_keybindings"
          ? "customCommandKeybindings"
          : path === "settings.terminal_keybindings"
            ? "terminalKeybindings"
            : path === "settings.terminal_triggers"
              ? "terminalTriggers"
              : null;
  if (!key) {
    return null;
  }
  const list = (settings[key] ??= []) as Record<string, unknown>[];
  const item: Record<string, unknown> = {};
  list.push(item);
  return item;
}

export function serializeOnibiConfigToml(config = getOnibiConfigSnapshot()): string {
  const settings = mergeSettings(config.settings);
  const lines: string[] = [
    "version = 1",
    "",
    "[server]",
    "port = 17893",
    "approval_timeout_secs = 600",
    "pty_ring_limit = 5000",
    "",
    "[settings]",
    settingLine("theme", settings.theme),
    settingLine("font_family", settings.fontFamily),
    settingLine("ui_font_family", settings.uiFontFamily),
    settingLine("terminal_font_family", settings.terminalFontFamily),
    settingLine("editor_font_family", settings.editorFontFamily),
    settingLine("ui_font_size", settings.uiFontSize),
    settingLine("terminal_font_size", settings.terminalFontSize),
    settingLine("terminal_scrollback_lines", settings.terminalScrollbackLines),
    settingLine("terminal_shell_integration", settings.terminalShellIntegration),
    settingLine("terminal_shell_mode", settings.terminalShellMode),
    settingLine("terminal_screen_reader_mode", settings.terminalScreenReaderMode),
    settingLine("terminal_copy_format", settings.terminalCopyFormat),
    settingLine("terminal_osc52_clipboard", settings.terminalOsc52Clipboard),
    settingLine("terminal_transparent_background", settings.terminalTransparentBackground),
    settingLine("terminal_inline_images", settings.terminalInlineImages),
    settingLine("terminal_confirm_close", settings.terminalConfirmClose),
    `terminal_shader_paths = ${tomlArray(settings.terminalShaderPaths)}`,
    settingLine("notification_delivery", settings.notificationDelivery),
    settingLine("sound_alerts_enabled", settings.soundAlertsEnabled),
    settingLine("sound_completion_path", settings.soundCompletionPath),
    settingLine("sound_request_path", settings.soundRequestPath),
    settingLine(
      "suppress_foreground_tab_notifications",
      settings.suppressForegroundTabNotifications,
    ),
    settingLine("keybinding_prefix", settings.keybindingPrefix),
    settingLine("new_pane_cwd", settings.newPaneCwd),
    settingLine("editor_font_size", settings.editorFontSize),
    settingLine("editor_keybinding_mode", settings.editorKeybindingMode),
    settingLine("editor_vim_relative_line_numbers", settings.editorVimRelativeLineNumbers),
    settingLine("editor_vim_system_clipboard", settings.editorVimSystemClipboard),
    settingLine("editor_emacs_system_clipboard", settings.editorEmacsSystemClipboard),
    settingLine("editor_open_limit", settings.editorOpenLimit),
    settingLine("closed_buffer_history_limit", settings.closedBufferHistoryLimit),
    settingLine("diff_view_mode", settings.diffViewMode),
    settingLine("tab_bar_orientation", settings.tabBarOrientation),
    settingLine("tab_bar_position", settings.tabBarPosition),
    settingLine("show_hidden_files", settings.showHiddenFiles),
    settingLine("show_file_icons", settings.showFileIcons),
    settingLine("show_terminal_pane_agent_labels", settings.showTerminalPaneAgentLabels),
    settingLine("web_open_mode", settings.webOpenMode),
    settingLine("preferred_browser", settings.preferredBrowser),
    settingLine("default_agent", settings.defaultAgent),
    "",
    "[settings.agent_commands]",
    ...AGENT_KINDS.map((agent) => settingLine(agent, settings.agentCommands[agent] ?? "")),
    "",
    "[settings.agent_install_commands]",
    ...Object.entries(settings.agentInstallCommands).map(([agent, command]) =>
      settingLine(agent, command ?? ""),
    ),
    "",
    "[settings.agent_label_overrides]",
    ...AGENT_KINDS.map((agent) =>
      settingLine(agent, settings.agentLabelOverrides[agent] ?? ""),
    ),
    "",
    "[settings.sound_agents]",
    ...AGENT_KINDS.map((agent) => settingLine(agent, settings.soundAgents[agent] !== false)),
    "",
    "[settings.custom_color_scheme]",
    settingLine("label", settings.customColorScheme.label),
    "",
    "[settings.custom_color_scheme.colors]",
    ...COLOR_SCHEME_COLOR_KEYS.map((key) =>
      settingLine(
        key.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`),
        settings.customColorScheme.colors[key],
      ),
    ),
  ];

  for (const binding of settings.appKeybindings) {
    lines.push(
      "",
      "[[settings.app_keybindings]]",
      settingLine("keys", binding.keys),
      settingLine("action", binding.action),
    );
  }

  for (const binding of settings.customCommandKeybindings) {
    lines.push(
      "",
      "[[keybindings.command]]",
      settingLine("keys", binding.keys),
      settingLine("command", binding.command),
      ...(binding.description ? [settingLine("description", binding.description)] : []),
    );
  }

  for (const binding of settings.terminalKeybindings) {
    lines.push(
      "",
      "[[settings.terminal_keybindings]]",
      settingLine("keys", binding.keys),
      settingLine("action", binding.action),
      ...(binding.source ? [settingLine("source", binding.source)] : []),
    );
  }

  for (const trigger of settings.terminalTriggers) {
    lines.push(
      "",
      "[[settings.terminal_triggers]]",
      settingLine("id", trigger.id),
      settingLine("label", trigger.label),
      settingLine("pattern", trigger.pattern),
      settingLine("enabled", trigger.enabled),
      `actions = ${tomlArray(trigger.actions)}`,
      settingLine("source", trigger.source),
    );
  }

  for (const workspace of config.workspaces) {
    lines.push(
      "",
      "[[workspaces]]",
      settingLine("id", workspace.id),
      settingLine("path", workspace.path),
      settingLine("name", workspace.name),
      settingLine("collapsed", workspace.collapsed === true),
    );
  }

  return `${lines.join("\n")}\n`;
}

export function serializeOnibiConfig(): string {
  return `${JSON.stringify(getOnibiConfigSnapshot(), null, 2)}\n`;
}

export function parseOnibiConfigToml(toml: string): OnibiConfigExport {
  const settings: Record<string, unknown> = {};
  const workspaces: Record<string, unknown>[] = [];
  let section = "";
  let currentObject: Record<string, unknown> | null = null;

  for (const rawLine of toml.split(/\r?\n/)) {
    const line = stripTomlComment(rawLine);
    if (!line) {
      continue;
    }
    if (line.startsWith("[[") && line.endsWith("]]")) {
      section = line.slice(2, -2).trim();
      currentObject = pushTomlTable(settings, workspaces, section);
      continue;
    }
    if (line.startsWith("[") && line.endsWith("]")) {
      section = line.slice(1, -1).trim();
      currentObject = null;
      continue;
    }
    const separator = line.indexOf("=");
    if (separator < 0) {
      continue;
    }
    const key = line.slice(0, separator).trim();
    const value = parseTomlValue(line.slice(separator + 1));
    if (section === "" && key === "version") {
      continue;
    }
    if (section === "workspaces") {
      currentObject ??= pushTomlTable(settings, workspaces, section);
    }
    assignTomlSetting(settings, section, key, value, currentObject);
  }

  return {
    version: 1,
    settings: mergeSettings(settings as Partial<AppSettings>),
    workspaces: normalizeWorkspaces(workspaces),
  };
}

export function parseOnibiConfigText(text: string): OnibiConfigExport {
  const trimmed = text.trim();
  return trimmed.startsWith("{")
    ? parseOnibiConfigJson(trimmed)
    : parseOnibiConfigToml(trimmed);
}

export function parseOnibiConfigJson(json: string): OnibiConfigExport {
  const parsed = JSON.parse(json) as unknown;
  if (!isRecord(parsed)) {
    throw new Error("Config must be a JSON object.");
  }
  return {
    version: 1,
    settings: mergeSettings(
      isRecord(parsed.settings) ? (parsed.settings as Partial<AppSettings>) : undefined,
    ),
    workspaces: normalizeWorkspaces(parsed.workspaces),
  };
}

export async function readOnibiConfigTomlFile(): Promise<OnibiConfigExport | null> {
  const raw = await readOnibiConfigTomlText();
  return typeof raw === "string" && raw.trim() ? parseOnibiConfigToml(raw) : null;
}

export async function readOnibiConfigTomlText(): Promise<string | null> {
  return invoke<string | null>("onibi_read_config_toml");
}

export async function writeOnibiConfigTomlFile(toml: string): Promise<void> {
  await invoke("onibi_write_config_toml", { toml });
}

export function applyOnibiConfig(config: OnibiConfigExport): void {
  const workspaces = normalizeWorkspaces(config.workspaces);
  useSessionStore.setState((state) => {
    const workspaceIds = new Set(workspaces.map((workspace) => workspace.id));
    const sessions = state.sessions.filter((session) =>
      workspaceIds.has(session.workspaceId),
    );
    const sessionIds = new Set(sessions.map((session) => session.id));
    const scopedData = pruneWorkspaceScopedData(state, workspaceIds);
    const workspaceTabs = pruneWorkspaceTabs(
      state.workspaceTabs.filter((tab) => workspaceIds.has(tab.workspaceId)),
      sessionIds,
    );
    for (const workspace of workspaces) {
      if (!workspaceTabs.some((tab) => tab.workspaceId === workspace.id)) {
        workspaceTabs.push(makeWorkspaceTab(workspace.id));
      }
    }
    const activeTab =
      workspaceTabs.find((tab) => tab.id === state.activeWorkspaceTabId) ??
      workspaceTabs.find((tab) => tab.workspaceId === state.activeWorkspaceId) ??
      workspaceTabs[0] ??
      null;
    return {
      settings: mergeSettings(config.settings),
      sessions,
      workspaces,
      workspaceTabs,
      ...scopedData,
      arrangements: state.arrangements.filter((arrangement) =>
        arrangementInWorkspaceIds(arrangement, workspaceIds),
      ),
      activeCommandBlocks: Object.fromEntries(
        Object.entries(state.activeCommandBlocks).filter(([sessionId]) =>
          sessionIds.has(sessionId),
        ),
      ),
      ...mirrorWorkspaceTab(activeTab),
    };
  });
  persistLater();
}

export function sessionTitle(
  agent: AgentKind,
  workspace: Workspace,
  settings?: Pick<AppSettings, "agentLabelOverrides"> | null,
): string {
  return `${agentDisplayLabel(agent, settings)} · ${workspace.name}`;
}

export function buildAgentHandoffPrompt(
  sourceSession: Session,
  workspace: Workspace,
  selectedPath: string | null,
  events: SessionEvent[],
): string {
  const settings = useSessionStore.getState().settings;
  const recentEvents = events
    .filter((event) => event.workspaceId === workspace.id)
    .slice(-8)
    .map((event) => `- ${event.type}: ${event.summary}`)
    .join("\n");
  return [
    `You are taking over an Onibi workspace from ${agentDisplayLabel(sourceSession.agent, settings)}.`,
    "",
    `Workspace: ${workspace.name}`,
    `Path: ${workspace.path}`,
    `Prior session: ${sourceSession.title}`,
    selectedPath ? `Open file or view: ${selectedPath}` : null,
    "",
    "Recent Onibi events:",
    recentEvents || "- No recorded events yet.",
    "",
    "Continue from this context. Inspect the repository before editing, and preserve any user changes.",
  ]
    .filter(Boolean)
    .join("\n");
}

function unquoteToken(token: string): string {
  if (
    (token.startsWith('"') && token.endsWith('"')) ||
    (token.startsWith("'") && token.endsWith("'"))
  ) {
    return token.slice(1, -1).replace(/\\"/g, '"').replace(/\\'/g, "'");
  }
  return token;
}

export function splitCommandLine(commandLine: string): string[] {
  const matches =
    commandLine.match(/"([^"\\]|\\.)*"|'([^'\\]|\\.)*'|[^\s]+/g) ?? [];
  return matches.map(unquoteToken);
}

export function launchCommandForAgent(
  agent: AgentKind,
  settings: AppSettings,
  initialPrompt: string,
): Pick<PtySpawnRequest, "command" | "args" | "shellMode"> {
  if (agent === "shell") {
    return { command: shellPath(), args: [], shellMode: settings.terminalShellMode };
  }
  const commandLine =
    settings.agentCommands[agent] || DEFAULT_AGENT_COMMANDS[agent];
  const [command, ...args] = splitCommandLine(commandLine);
  const prompt = initialPrompt.trim();
  return {
    command: command ?? "",
    args: prompt ? [...args, prompt] : args,
  };
}

export async function resolveAgentBinary(
  agent: AgentKind,
  settings: AppSettings,
): Promise<string | null> {
  if (agent === "shell") {
    return null;
  }
  return resolveCommandBinary(
    settings.agentCommands[agent] || DEFAULT_AGENT_COMMANDS[agent],
  );
}

export async function resolveCommandBinary(
  commandLine: string,
): Promise<string | null> {
  const [command] = splitCommandLine(commandLine);
  if (!command) {
    return null;
  }
  return invoke<string | null>("fs_resolve_binary", { command });
}

function workspaceForSession(session: Session, workspaces: Workspace[]): Workspace | null {
  return workspaces.find((workspace) => workspace.id === session.workspaceId) ?? null;
}

function shellIntegrationEnv(
  agent: AgentKind,
  settings: AppSettings,
): Array<[string, string]> {
  return agent === "shell" && settings.terminalShellIntegration
    ? [["ONIBI_SHELL_INTEGRATION", "1"]]
    : [];
}

function shellModePatch(
  shellMode: TerminalShellMode | null | undefined,
): Pick<PtySpawnRequest, "shellMode"> | Record<string, never> {
  return shellMode ? { shellMode } : {};
}

export async function spawnSessionFromLaunchSpec(
  spec: TerminalLaunchSpec,
  placement?: TerminalPanePlacement | null,
): Promise<PtyId> {
  const id = await ptySpawn({
    command: spec.command,
    args: spec.args,
    cwd: spec.cwd,
    env: spec.env,
    rows: 30,
    cols: 100,
    agent: spec.agent,
    workspaceId: spec.workspaceId,
    title: spec.title,
    ...shellModePatch(spec.shellMode),
  });
  const restart: SessionRestartMetadata = {
    command: spec.command,
    args: spec.args,
    cwd: spec.cwd,
    env: spec.env,
    ...(spec.shellMode ? { shellMode: spec.shellMode } : {}),
  };
  useSessionStore.getState().addSession(
    {
      id,
      agent: spec.agent,
      workspaceId: spec.workspaceId,
      title: spec.title,
      status: "running",
      createdAt: Date.now(),
      pendingApprovals: [],
      cwd: spec.cwd ?? undefined,
      lastExitCode: null,
      lastTrigger: null,
      lastCommandBlockId: null,
      transcript: null,
      restart,
    },
    placement,
  );
  const workspace = useSessionStore
    .getState()
    .workspaces.find((item) => item.id === spec.workspaceId);
  if (spec.agent !== "shell" && workspace) {
    void startAgentReview(id, workspace.path).catch((error) => {
      console.warn("failed to start agent review tracking", error);
    });
  }
  return id;
}

export async function spawnAgentSession(
  agent: AgentKind,
  workspace: Workspace,
  initialPrompt: string,
  placement?: TerminalPanePlacement | null,
  options?: { cwd?: string | null },
): Promise<PtyId> {
  const settings = useSessionStore.getState().settings;
  const launch = launchCommandForAgent(agent, settings, initialPrompt);
  const cwd = isAbsoluteCwdPath(options?.cwd) ? options.cwd : workspace.path;
  const env: Array<[string, string]> =
    shellIntegrationEnv(agent, settings);
  return spawnSessionFromLaunchSpec(
    {
      agent,
      workspaceId: workspace.id,
      title: sessionTitle(agent, workspace, settings),
      command: launch.command,
      args: launch.args,
      cwd,
      env,
      ...(launch.shellMode ? { shellMode: launch.shellMode } : {}),
      initialPrompt,
    },
    placement,
  );
}

export function sessionNeedsCloseConfirmation(
  session: Session | null | undefined,
  settings: AppSettings,
): boolean {
  return Boolean(
    session &&
      settings.terminalConfirmClose &&
      (session.status === "running" || session.status === "awaiting-approval"),
  );
}

export async function closeSession(sessionId: string, force = false): Promise<boolean> {
  const state = useSessionStore.getState();
  const session = state.sessions.find((item) => item.id === sessionId);
  if (!session) {
    return false;
  }
  if (
    !force &&
    sessionNeedsCloseConfirmation(session, state.settings) &&
    !window.confirm(`Close ${session.title}?`)
  ) {
    return false;
  }
  await ptyKill(sessionId).catch(() => undefined);
  if (session.agent !== "shell") {
    await stopAgentReview(sessionId).catch(() => undefined);
  }
  useSessionStore.getState().removeSession(sessionId);
  return true;
}

export async function restartSession(sessionId: string): Promise<PtyId | null> {
  const state = useSessionStore.getState();
  const session = state.sessions.find((item) => item.id === sessionId);
  if (!session?.restart) {
    return null;
  }
  if (session.status === "stale") {
    try {
      const attached = await sessionAttach(session.id);
      if (attached?.session?.id) {
        const replacement: Session = {
          ...mergeDaemonSession(session, attached.session),
          id: attached.session.id,
          status: "running",
          pendingApprovals: [],
          lastExitCode: null,
          lastTrigger: null,
          lastCommandBlockId: null,
          transcript: null,
        };
        if (session.agent !== "shell") {
          await stopAgentReview(session.id).catch(() => undefined);
          const workspace = workspaceForSession(session, state.workspaces);
          if (workspace) {
            void startAgentReview(replacement.id, workspace.path).catch((error) => {
              console.warn("failed to start agent review tracking", error);
            });
          }
        }
        useSessionStore.getState().replaceSession(session.id, replacement);
        return replacement.id;
      }
    } catch (error) {
      console.warn("daemon session attach failed, falling back to spawn", error);
    }
  }
  const id = await ptySpawn({
    command: session.restart.command,
    args: session.restart.args,
    cwd: session.restart.cwd,
    env: session.restart.env,
    rows: 30,
    cols: 100,
    agent: session.agent,
    workspaceId: session.workspaceId,
    title: session.title,
    ...shellModePatch(session.restart.shellMode),
  });
  const replacement: Session = {
    ...session,
    id,
    status: "running",
    createdAt: Date.now(),
    pendingApprovals: [],
    cwd: session.restart.cwd ?? session.cwd,
    lastExitCode: null,
    lastTrigger: null,
    lastCommandBlockId: null,
    transcript: null,
  };
  await ptyKill(sessionId).catch(() => undefined);
  if (session.agent !== "shell") {
    await stopAgentReview(sessionId).catch(() => undefined);
    const workspace = workspaceForSession(session, state.workspaces);
    if (workspace) {
      void startAgentReview(id, workspace.path).catch((error) => {
        console.warn("failed to start agent review tracking", error);
      });
    }
  }
  useSessionStore.getState().replaceSession(sessionId, replacement);
  return id;
}

export async function duplicateSession(
  sessionId: string,
  placement?: TerminalPanePlacement | null,
): Promise<PtyId | null> {
  const state = useSessionStore.getState();
  const session = state.sessions.find((item) => item.id === sessionId);
  if (!session?.restart) {
    return null;
  }
  return spawnSessionFromLaunchSpec(
    {
      agent: session.agent,
      workspaceId: session.workspaceId,
      title: `${session.title} copy`,
      command: session.restart.command,
      args: session.restart.args,
      cwd: session.cwd ?? session.restart.cwd,
      env: session.restart.env,
      ...(session.restart.shellMode ? { shellMode: session.restart.shellMode } : {}),
    },
    placement,
  );
}

export async function restoreArrangement(arrangementId: string): Promise<boolean> {
  const state = useSessionStore.getState();
  const arrangement = state.arrangements.find((item) => item.id === arrangementId);
  if (!arrangement) {
    return false;
  }
  if (
    state.sessions.some((session) => sessionNeedsCloseConfirmation(session, state.settings)) &&
    !window.confirm(`Restore ${arrangement.name} and close current sessions?`)
  ) {
    return false;
  }

  const newSessions: Session[] = [];
  const sessionIds = new Map<string, string>();
  for (const savedSession of arrangement.sessions) {
    const workspace =
      state.workspaces.find((item) => item.id === savedSession.workspaceId) ??
      state.workspaces.find((item) => item.id === arrangement.workspaceId);
    if (!workspace) {
      continue;
    }
    const launch = savedSession.launch;
    const id = await ptySpawn({
      command: launch.command,
      args: launch.args,
      cwd: launch.cwd,
      env: launch.env,
      rows: 30,
      cols: 100,
      agent: savedSession.agent,
      workspaceId: savedSession.workspaceId,
      title: savedSession.title,
      ...shellModePatch(launch.shellMode),
    });
    sessionIds.set(savedSession.sessionId, id);
    newSessions.push({
      id,
      agent: savedSession.agent,
      workspaceId: savedSession.workspaceId,
      title: savedSession.title,
      status: "running",
      createdAt: Date.now(),
      pendingApprovals: [],
      cwd: launch.cwd ?? undefined,
      lastExitCode: null,
      lastTrigger: null,
      lastCommandBlockId: null,
      transcript: null,
      restart: launch,
    });
    if (savedSession.agent !== "shell") {
      void startAgentReview(id, workspace.path).catch((error) => {
        console.warn("failed to start agent review tracking", error);
      });
    }
  }

  for (const session of state.sessions) {
    await ptyKill(session.id).catch(() => undefined);
    if (session.agent !== "shell") {
      await stopAgentReview(session.id).catch(() => undefined);
    }
  }

  const terminalLayout = mapTerminalLayoutSessions(
    arrangement.terminalLayout,
    sessionIds,
  );
  const activeTerminalPaneId =
    arrangement.activeTerminalPaneId &&
    terminalLayout &&
    paneContainsPane(terminalLayout, arrangement.activeTerminalPaneId)
      ? arrangement.activeTerminalPaneId
      : firstLeaf(terminalLayout)?.paneId ?? null;
  const activeSessionId =
    activeTerminalPaneId && terminalLayout
      ? sessionIdForPane(terminalLayout, activeTerminalPaneId)
      : newSessions[0]?.id ?? null;
  const workspaceId = arrangement.workspaceId ?? newSessions[0]?.workspaceId ?? null;
  const restoredTab = workspaceId
    ? makeWorkspaceTab(
        workspaceId,
        arrangement.name,
        terminalLayout,
        activeTerminalPaneId,
        arrangement.maximizedTerminalPaneId,
      )
    : null;
  // restore buffers if the saved arrangement had any; otherwise fall back
  // to the single legacy `selectedFile` (old arrangements).
  const savedBuffers = arrangement.openBuffers ?? [];
  const fallbackBuffers = savedBuffers.length === 0 && arrangement.selectedFile
    ? [arrangement.selectedFile]
    : savedBuffers;
  const restoredActiveKey = arrangement.activeBufferKey
    ?? (arrangement.selectedFile ? bufferKey(arrangement.selectedFile) : null);
  const resolvedActiveKey =
    restoredActiveKey &&
    fallbackBuffers.some((buffer) => bufferKey(buffer) === restoredActiveKey)
      ? restoredActiveKey
      : (fallbackBuffers[0] ? bufferKey(fallbackBuffers[0]) : null);
  const resolvedSelected = resolvedActiveKey
    ? fallbackBuffers.find((buffer) => bufferKey(buffer) === resolvedActiveKey) ?? null
    : null;
  useSessionStore.setState((current) => ({
    sessions: newSessions,
    workspaceTabs: restoredTab
      ? [
          ...current.workspaceTabs.filter((tab) => tab.workspaceId !== restoredTab.workspaceId),
          restoredTab,
        ]
      : current.workspaceTabs,
    activeWorkspaceId: restoredTab?.workspaceId ?? current.activeWorkspaceId,
    activeWorkspaceTabId: restoredTab?.id ?? current.activeWorkspaceTabId,
    terminalLayout,
    activeTerminalPaneId,
    maximizedTerminalPaneId:
      arrangement.maximizedTerminalPaneId &&
      terminalLayout &&
      paneContainsPane(terminalLayout, arrangement.maximizedTerminalPaneId)
        ? arrangement.maximizedTerminalPaneId
        : null,
    activeSessionId,
    selectedFile: resolvedSelected,
    openBuffers: fallbackBuffers,
    activeBufferKey: resolvedActiveKey,
    activeSidebarView: arrangement.activeSidebarView,
    sessionEvents: appendEvent(current.sessionEvents, {
      type: "arrangement-restored",
      workspaceId: arrangement.workspaceId,
      summary: `Restored ${arrangement.name}`,
    }),
  }));
  persistLater();
  return true;
}

function parseGhosttyColor(value: string): string | undefined {
  const trimmed = value.trim();
  return /^#[0-9a-fA-F]{6}$/.test(trimmed) ? trimmed : undefined;
}

export function parseGhosttyConfig(config: string): GhosttyTheme {
  const theme: GhosttyTheme = { palette: {} };
  for (const rawLine of config.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    const separator = line.indexOf("=");
    if (separator < 0) {
      continue;
    }
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (key === "font-family") {
      theme.fontFamily = value;
    } else if (key === "font-size") {
      const parsed = Number(value);
      if (Number.isFinite(parsed) && parsed > 0) {
        theme.fontSize = parsed;
      }
    } else if (key === "background") {
      theme.background = parseGhosttyColor(value);
    } else if (key === "foreground") {
      theme.foreground = parseGhosttyColor(value);
    } else if (key === "palette") {
      const [index, color] = value.split("=").map((part) => part.trim());
      const parsedIndex = Number(index);
      const parsedColor = color ? parseGhosttyColor(color) : undefined;
      if (Number.isInteger(parsedIndex) && parsedColor) {
        theme.palette[parsedIndex] = parsedColor;
      }
    }
  }
  return theme;
}

function parseConfigHexColor(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  const trimmed = value.trim().replace(/^['"]|['"]$/g, "");
  const color = trimmed.match(/#?[0-9a-fA-F]{6}/)?.[0];
  if (!color) {
    return undefined;
  }
  const normalized = color.startsWith("#") ? color : `#${color}`;
  return /^#[0-9a-fA-F]{6}$/.test(normalized) ? normalized : undefined;
}

function parseSimpleAssignments(config: string): Record<string, string> {
  const values: Record<string, string> = {};
  for (const rawLine of config.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    const separator = line.includes("=") ? line.indexOf("=") : line.search(/\s/);
    if (separator < 0) {
      continue;
    }
    const key = line.slice(0, separator).trim();
    const value = line.slice(separator + 1).trim();
    if (key) {
      values[key] = value;
    }
  }
  return values;
}

function parseLooseAssignments(config: string): Record<string, string> {
  const values: Record<string, string> = {};
  for (const rawLine of config.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#") || line.startsWith("//")) {
      continue;
    }
    const separator = line.includes("=")
      ? line.indexOf("=")
      : line.includes(":")
        ? line.indexOf(":")
        : line.search(/\s/);
    if (separator < 0) {
      continue;
    }
    const key = line.slice(0, separator).trim().replace(/^["']|["']$/g, "");
    const value = line
      .slice(separator + 1)
      .trim()
      .replace(/,$/, "")
      .replace(/^["'`]|["'`]$/g, "");
    if (key) {
      values[key] = value;
      values[key.toLowerCase()] = value;
    }
  }
  return values;
}

function looseValue(
  values: Record<string, string>,
  keys: string[],
): string | undefined {
  for (const key of keys) {
    const value = values[key] ?? values[key.toLowerCase()];
    if (value) {
      return value;
    }
  }
  return undefined;
}

function cleanTerminalFontFamily(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  const cleaned = value
    .replace(/^["'`]+|["'`]+$/g, "")
    .split(",")[0]
    .replace(/:size=.*$/i, "")
    .replace(/\s+[0-9.]+$/g, "")
    .replace(/^["'`]+|["'`]+$/g, "")
    .trim();
  return cleaned || undefined;
}

function looseFontSize(
  config: string,
  values: Record<string, string>,
): number | undefined {
  const explicit = looseValue(values, [
    "font-size",
    "font_size",
    "fontSize",
    "terminal.fontSize",
    "font.size",
    "size",
  ]);
  const explicitNumber = Number(explicit);
  if (Number.isFinite(explicitNumber)) {
    return explicitNumber;
  }
  const footSize = config.match(/:size=([0-9.]+)/i)?.[1];
  const footNumber = Number(footSize);
  if (Number.isFinite(footNumber)) {
    return footNumber;
  }
  const konsoleSize = config.match(/^Font=[^,\n]+,([0-9.]+)/m)?.[1];
  const konsoleNumber = Number(konsoleSize);
  return Number.isFinite(konsoleNumber) ? konsoleNumber : undefined;
}

function matchFirst(config: string, patterns: RegExp[]): string | undefined {
  for (const pattern of patterns) {
    const match = config.match(pattern);
    if (match?.[1]) {
      return match[1].trim();
    }
  }
  return undefined;
}

function normalizeKeyCombo(value: string): string {
  const parts = value
    .trim()
    .replace(/-/g, "+")
    .split("+")
    .map((part) => part.trim().toLowerCase())
    .filter(Boolean);
  const modifiers = new Set<string>();
  let key = "";
  for (const part of parts) {
    if (["cmd", "command", "meta", "super"].includes(part)) {
      modifiers.add("cmd");
    } else if (["ctrl", "control"].includes(part)) {
      modifiers.add("ctrl");
    } else if (["alt", "option", "opt"].includes(part)) {
      modifiers.add("alt");
    } else if (["shift"].includes(part)) {
      modifiers.add("shift");
    } else if (part === "return") {
      key = "enter";
    } else if (part === "escape") {
      key = "esc";
    } else if (part === "spacebar") {
      key = "space";
    } else {
      key = part;
    }
  }
  if (!key) {
    return "";
  }
  return [
    ...(["cmd", "ctrl", "alt", "shift"] as const).filter((modifier) =>
      modifiers.has(modifier),
    ),
    key,
  ].join("+");
}

function keyComboFromModsAndKey(mods: string | undefined, key: string): string {
  const modifierParts = (mods ?? "")
    .split(/[|+]/)
    .map((part) => part.trim())
    .filter(Boolean);
  return normalizeKeyCombo([...modifierParts, key].join("+"));
}

function actionFromTerminalAction(value: string | undefined): TerminalKeybindingAction | null {
  const normalized = value?.toLowerCase() ?? "";
  if (
    normalized.includes("copytoclipboard") ||
    normalized.includes("copy_to_clipboard") ||
    normalized === "copy"
  ) {
    return "copy";
  }
  if (
    normalized.includes("pastefromclipboard") ||
    normalized.includes("paste_from_clipboard") ||
    normalized === "paste"
  ) {
    return "paste";
  }
  if (
    normalized.includes("clear_screen") ||
    normalized.includes("clear_terminal") ||
    normalized.includes("clear-history") ||
    normalized === "clear"
  ) {
    return "clear";
  }
  if (normalized.includes("selectall") || normalized.includes("select_all")) {
    return "select-all";
  }
  if (
    normalized.includes("find") ||
    normalized.includes("search") ||
    normalized === "find"
  ) {
    return "find";
  }
  return null;
}

function makeTerminalKeybinding(
  source: TerminalConfigSource,
  keys: string,
  action: TerminalKeybindingAction | null,
): TerminalKeybinding | null {
  const normalizedKeys = normalizeKeyCombo(keys);
  return normalizedKeys && action ? { keys: normalizedKeys, action, source } : null;
}

function uniqueKeybindings(bindings: Array<TerminalKeybinding | null>): TerminalKeybinding[] {
  const seen = new Set<string>();
  const result: TerminalKeybinding[] = [];
  for (const binding of bindings) {
    if (!binding) {
      continue;
    }
    const id = `${binding.keys}:${binding.action}`;
    if (seen.has(id)) {
      continue;
    }
    seen.add(id);
    result.push(binding);
  }
  return result;
}

function parseGhosttyKeybindings(config: string): TerminalKeybinding[] {
  const bindings: Array<TerminalKeybinding | null> = [];
  for (const rawLine of config.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line.startsWith("keybind")) {
      continue;
    }
    const value = line.slice(line.indexOf("=") + 1).trim();
    const parts = value.split("=").map((part) => part.trim());
    if (parts.length < 2) {
      continue;
    }
    const action = parts[parts.length - 1];
    const keys = parts[parts.length - 2].replace(/^(global|all):/, "");
    bindings.push(makeTerminalKeybinding("ghostty", keys, actionFromTerminalAction(action)));
  }
  return uniqueKeybindings(bindings);
}

function parseSimpleMappedKeybindings(
  source: TerminalConfigSource,
  config: string,
): TerminalKeybinding[] {
  const bindings: Array<TerminalKeybinding | null> = [];
  for (const rawLine of config.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }
    const match = line.match(/^(?:map|bind-key|bind)\s+(\S+)\s+(.+)$/);
    if (!match) {
      continue;
    }
    bindings.push(
      makeTerminalKeybinding(source, match[1], actionFromTerminalAction(match[2])),
    );
  }
  return uniqueKeybindings(bindings);
}

function parseAlacrittyKeybindings(config: string): TerminalKeybinding[] {
  const bindings: Array<TerminalKeybinding | null> = [];
  for (const match of config.matchAll(/\[\[keyboard\.bindings\]\]([\s\S]*?)(?=\n\[\[|$)/g)) {
    const block = match[1];
    const key = matchFirst(block, [/key\s*=\s*["']([^"']+)["']/]);
    const mods = matchFirst(block, [/mods\s*=\s*["']([^"']+)["']/]);
    const action = matchFirst(block, [/action\s*=\s*["']([^"']+)["']/]);
    if (!key) {
      continue;
    }
    bindings.push(
      makeTerminalKeybinding(
        "alacritty",
        keyComboFromModsAndKey(mods, key),
        actionFromTerminalAction(action),
      ),
    );
  }
  return uniqueKeybindings(bindings);
}

function parseWezTermKeybindings(config: string): TerminalKeybinding[] {
  const bindings: Array<TerminalKeybinding | null> = [];
  for (const match of config.matchAll(/\{[^{}]*key\s*=\s*["']([^"']+)["'][^{}]*\}/g)) {
    const block = match[0];
    const key = match[1];
    const mods = matchFirst(block, [/mods\s*=\s*["']([^"']+)["']/]);
    const action = matchFirst(block, [/action\s*=\s*([^,}]+)/]);
    bindings.push(
      makeTerminalKeybinding(
        "wezterm",
        keyComboFromModsAndKey(mods, key),
        actionFromTerminalAction(action),
      ),
    );
  }
  return uniqueKeybindings(bindings);
}

function parseShaderPaths(config: string, patterns: RegExp[]): string[] {
  const paths: string[] = [];
  for (const pattern of patterns) {
    for (const match of config.matchAll(pattern)) {
      const path = match[1]?.trim().replace(/^['"]|['"]$/g, "");
      if (path) {
        paths.push(path);
      }
    }
  }
  return normalizeStringArray(paths);
}

function importFromGhostty(candidate: TerminalConfigCandidate): TerminalConfigImport {
  const theme = parseGhosttyConfig(candidate.content);
  const colors: Partial<ColorSchemeColors> = {};
  if (theme.background) {
    colors.bg0 = theme.background;
    colors.terminalBackground = theme.background;
  }
  if (theme.foreground) {
    colors.fg0 = theme.foreground;
    colors.terminalForeground = theme.foreground;
  }
  if (theme.palette[1]) {
    colors.danger = theme.palette[1];
  }
  if (theme.palette[2]) {
    colors.accent2 = theme.palette[2];
  }
  if (theme.palette[4]) {
    colors.accent = theme.palette[4];
  }
  return terminalImport(
    candidate,
    colors,
    theme.fontFamily,
    theme.fontSize,
    undefined,
    parseGhosttyKeybindings(candidate.content),
    parseShaderPaths(candidate.content, [
      /custom-shader\s*=\s*([^\n#]+)/g,
      /shader\s*=\s*([^\n#]+)/g,
    ]),
  );
}

function importFromKitty(candidate: TerminalConfigCandidate): TerminalConfigImport {
  const values = parseSimpleAssignments(candidate.content);
  const colors: Partial<ColorSchemeColors> = {};
  const background = parseConfigHexColor(values.background);
  const foreground = parseConfigHexColor(values.foreground);
  const cursor = parseConfigHexColor(values.cursor);
  const selection = parseConfigHexColor(values.selection_background);
  if (background) {
    colors.bg0 = background;
    colors.terminalBackground = background;
  }
  if (foreground) {
    colors.fg0 = foreground;
    colors.terminalForeground = foreground;
  }
  if (cursor) {
    colors.terminalCursor = cursor;
    colors.accent = cursor;
  }
  if (selection) {
    colors.terminalSelection = selection;
    colors.bg3 = selection;
  }
  const danger = parseConfigHexColor(values.color1);
  const accent2 = parseConfigHexColor(values.color2);
  const accent = parseConfigHexColor(values.color4);
  if (danger) {
    colors.danger = danger;
  }
  if (accent2) {
    colors.accent2 = accent2;
  }
  if (accent) {
    colors.accent = accent;
  }
  const fontSize = values.font_size ? Number(values.font_size) : undefined;
  return terminalImport(
    candidate,
    colors,
    values.font_family?.replace(/^['"]|['"]$/g, ""),
    Number.isFinite(fontSize) ? fontSize : undefined,
    undefined,
    parseSimpleMappedKeybindings("kitty", candidate.content),
  );
}

function importFromAlacritty(candidate: TerminalConfigCandidate): TerminalConfigImport {
  const config = candidate.content;
  const colors: Partial<ColorSchemeColors> = {};
  const background = parseConfigHexColor(
    matchFirst(config, [/background\s*=\s*["']([^"']+)["']/]),
  );
  const foreground = parseConfigHexColor(
    matchFirst(config, [/foreground\s*=\s*["']([^"']+)["']/]),
  );
  const cursor = parseConfigHexColor(
    matchFirst(config, [/cursor\s*=\s*\{[^}]*cursor\s*=\s*["']([^"']+)["']/s]),
  );
  const selection = parseConfigHexColor(
    matchFirst(config, [
      /selection\s*=\s*\{[^}]*background\s*=\s*["']([^"']+)["']/s,
      /\[colors\.selection\][\s\S]*?background\s*=\s*["']([^"']+)["']/,
    ]),
  );
  if (background) {
    colors.bg0 = background;
    colors.terminalBackground = background;
  }
  if (foreground) {
    colors.fg0 = foreground;
    colors.terminalForeground = foreground;
  }
  if (cursor) {
    colors.terminalCursor = cursor;
    colors.accent = cursor;
  }
  if (selection) {
    colors.terminalSelection = selection;
    colors.bg3 = selection;
  }
  const fontFamily = matchFirst(config, [
    /family\s*=\s*["']([^"']+)["']/,
    /normal\s*=\s*\{[^}]*family\s*=\s*["']([^"']+)["']/s,
  ]);
  const fontSize = Number(matchFirst(config, [/size\s*=\s*([0-9.]+)/]));
  return terminalImport(
    candidate,
    colors,
    fontFamily,
    Number.isFinite(fontSize) ? fontSize : undefined,
    undefined,
    parseAlacrittyKeybindings(candidate.content),
  );
}

function importFromWezTerm(candidate: TerminalConfigCandidate): TerminalConfigImport {
  const config = candidate.content;
  const colors: Partial<ColorSchemeColors> = {};
  const background = parseConfigHexColor(
    matchFirst(config, [/background\s*=\s*["']([^"']+)["']/]),
  );
  const foreground = parseConfigHexColor(
    matchFirst(config, [/foreground\s*=\s*["']([^"']+)["']/]),
  );
  const cursor = parseConfigHexColor(
    matchFirst(config, [/cursor_bg\s*=\s*["']([^"']+)["']/]),
  );
  const selection = parseConfigHexColor(
    matchFirst(config, [/selection_bg\s*=\s*["']([^"']+)["']/]),
  );
  if (background) {
    colors.bg0 = background;
    colors.terminalBackground = background;
  }
  if (foreground) {
    colors.fg0 = foreground;
    colors.terminalForeground = foreground;
  }
  if (cursor) {
    colors.terminalCursor = cursor;
    colors.accent = cursor;
  }
  if (selection) {
    colors.terminalSelection = selection;
    colors.bg3 = selection;
  }
  const colorSchemeName = matchFirst(config, [/color_scheme\s*=\s*["']([^"']+)["']/]);
  const fontFamily = matchFirst(config, [
    /font\s*=\s*wezterm\.font(?:_with_fallback)?\(\s*["']([^"']+)["']/,
    /font\s*=\s*["']([^"']+)["']/,
  ]);
  const fontSize = Number(matchFirst(config, [/font_size\s*=\s*([0-9.]+)/]));
  return terminalImport(
    candidate,
    colors,
    fontFamily,
    Number.isFinite(fontSize) ? fontSize : undefined,
    colorSchemeName,
    parseWezTermKeybindings(candidate.content),
  );
}

function importFromWarp(candidate: TerminalConfigCandidate): TerminalConfigImport {
  const values = parseSimpleAssignments(candidate.content.replace(/:/g, " = "));
  const colors: Partial<ColorSchemeColors> = {};
  const background = parseConfigHexColor(values.background);
  const foreground = parseConfigHexColor(values.foreground);
  const cursor = parseConfigHexColor(values.cursor);
  const accent = parseConfigHexColor(values.accent ?? values.blue);
  const danger = parseConfigHexColor(values.red);
  const accent2 = parseConfigHexColor(values.green ?? values.cyan);
  if (background) {
    colors.bg0 = background;
    colors.terminalBackground = background;
  }
  if (foreground) {
    colors.fg0 = foreground;
    colors.terminalForeground = foreground;
  }
  if (cursor) {
    colors.terminalCursor = cursor;
  }
  if (accent) {
    colors.accent = accent;
  }
  if (danger) {
    colors.danger = danger;
  }
  if (accent2) {
    colors.accent2 = accent2;
  }
  return terminalImport(candidate, colors, undefined, undefined, candidate.label);
}

function importFromLooseTerminal(candidate: TerminalConfigCandidate): TerminalConfigImport {
  const config = candidate.content;
  const values = parseLooseAssignments(config);
  const colors: Partial<ColorSchemeColors> = {};
  const background = parseConfigHexColor(
    looseValue(values, [
      "background",
      "backgroundColor",
      "background_color",
      "ColorBackground",
    ]),
  );
  const foreground = parseConfigHexColor(
    looseValue(values, [
      "foreground",
      "foregroundColor",
      "foreground_color",
      "ColorForeground",
    ]),
  );
  const cursor = parseConfigHexColor(
    looseValue(values, ["cursor", "cursorColor", "cursor_color", "ColorCursor"]),
  );
  const selection = parseConfigHexColor(
    looseValue(values, [
      "selection",
      "selectionColor",
      "selectionBackground",
      "selection_background",
      "ColorSelection",
    ]),
  );
  if (background) {
    colors.bg0 = background;
    colors.terminalBackground = background;
  }
  if (foreground) {
    colors.fg0 = foreground;
    colors.terminalForeground = foreground;
  }
  if (cursor) {
    colors.terminalCursor = cursor;
    colors.accent = cursor;
  }
  if (selection) {
    colors.terminalSelection = selection;
    colors.bg3 = selection;
  }

  const fontFamily =
    cleanTerminalFontFamily(
      looseValue(values, [
        "font-family",
        "font_family",
        "fontFamily",
        "font",
        "font_face",
        "fontFace",
        "FontName",
        "Font",
        "family",
        "face",
      ]),
    ) ?? cleanTerminalFontFamily(config.match(/^Font=([^,\n]+)/m)?.[1]);
  const fontSize = looseFontSize(config, values);
  const colorSchemeName = looseValue(values, [
    "theme",
    "color_scheme",
    "colorScheme",
    "ColorScheme",
  ]);

  return terminalImport(
    candidate,
    colors,
    fontFamily,
    fontSize,
    colorSchemeName,
    parseSimpleMappedKeybindings(candidate.source, candidate.content),
  );
}

function importFromWindowsTerminal(candidate: TerminalConfigCandidate): TerminalConfigImport {
  const colors: Partial<ColorSchemeColors> = {};
  let fontFamily: string | undefined;
  let fontSize: number | undefined;
  let colorSchemeName: string | undefined;
  try {
    const parsed = JSON.parse(candidate.content) as unknown;
    if (isRecord(parsed)) {
      const profiles = isRecord(parsed.profiles) ? parsed.profiles : {};
      const defaults = isRecord(profiles.defaults) ? profiles.defaults : {};
      const font = isRecord(defaults.font) ? defaults.font : {};
      fontFamily = typeof font.face === "string" ? font.face : undefined;
      fontSize = typeof defaults.fontSize === "number" ? defaults.fontSize : undefined;
      const schemes = Array.isArray(parsed.schemes) ? parsed.schemes : [];
      const scheme = schemes.find(isRecord);
      if (scheme) {
        colorSchemeName =
          typeof scheme.name === "string" ? scheme.name : "Windows Terminal Scheme";
        const background = parseConfigHexColor(
          typeof scheme.background === "string" ? scheme.background : undefined,
        );
        const foreground = parseConfigHexColor(
          typeof scheme.foreground === "string" ? scheme.foreground : undefined,
        );
        const cursor = parseConfigHexColor(
          typeof scheme.cursorColor === "string" ? scheme.cursorColor : undefined,
        );
        const selection = parseConfigHexColor(
          typeof scheme.selectionBackground === "string"
            ? scheme.selectionBackground
            : undefined,
        );
        if (background) {
          colors.bg0 = background;
          colors.terminalBackground = background;
        }
        if (foreground) {
          colors.fg0 = foreground;
          colors.terminalForeground = foreground;
        }
        if (cursor) {
          colors.terminalCursor = cursor;
          colors.accent = cursor;
        }
        if (selection) {
          colors.terminalSelection = selection;
          colors.bg3 = selection;
        }
      }
    }
  } catch {
    // Invalid Windows Terminal JSON is surfaced as an import with no fields.
  }
  return terminalImport(candidate, colors, fontFamily, fontSize, colorSchemeName);
}

function itermComponent(block: string, name: string): number | null {
  const match = block.match(
    new RegExp(`<key>${name} Component</key>\\s*<real>([^<]+)</real>`),
  );
  const parsed = Number(match?.[1]);
  return Number.isFinite(parsed) ? Math.min(255, Math.max(0, Math.round(parsed * 255))) : null;
}

function itermColor(config: string, name: string): string | undefined {
  const match = config.match(
    new RegExp(`<key>${name}</key>\\s*<dict>([\\s\\S]*?)</dict>`),
  );
  const block = match?.[1];
  if (!block) {
    return undefined;
  }
  const red = itermComponent(block, "Red");
  const green = itermComponent(block, "Green");
  const blue = itermComponent(block, "Blue");
  if (red === null || green === null || blue === null) {
    return undefined;
  }
  return `#${[red, green, blue]
    .map((component) => component.toString(16).padStart(2, "0"))
    .join("")}`;
}

function firstItermProfile(parsed: unknown): Record<string, unknown> | null {
  if (!isRecord(parsed)) {
    return null;
  }
  const directProfiles = Array.isArray(parsed.Profiles) ? parsed.Profiles : [];
  const bookmarks = Array.isArray(parsed["New Bookmarks"])
    ? parsed["New Bookmarks"]
    : [];
  const profiles = [...directProfiles, ...bookmarks].filter(isRecord);
  return profiles[0] ?? (isRecord(parsed) ? parsed : null);
}

function parseItermFont(
  value: string | undefined,
): { family: string; size?: number } | null {
  if (!value) {
    return null;
  }
  const match = value.match(/^(.+?)\s+([0-9.]+)$/);
  const family = (match?.[1] ?? value)
    .replace(/[-_](Regular|Medium|Bold|Italic|SemiBold)$/i, "")
    .replace(/-/g, " ")
    .trim();
  const size = Number(match?.[2]);
  return {
    family,
    size: Number.isFinite(size) ? size : undefined,
  };
}

function itermJsonColor(value: unknown): string | undefined {
  if (!isRecord(value)) {
    return undefined;
  }
  const red = Number(value["Red Component"]);
  const green = Number(value["Green Component"]);
  const blue = Number(value["Blue Component"]);
  if (![red, green, blue].every(Number.isFinite)) {
    return undefined;
  }
  return `#${[red, green, blue]
    .map((component) =>
      Math.min(255, Math.max(0, Math.round(component * 255)))
        .toString(16)
        .padStart(2, "0"),
    )
    .join("")}`;
}

function copyItermJsonColor(
  profile: Record<string, unknown>,
  sourceKey: string,
  colors: Partial<ColorSchemeColors>,
  targets: ColorSchemeColorKey[],
): void {
  const color = itermJsonColor(profile[sourceKey]);
  if (!color) {
    return;
  }
  for (const target of targets) {
    colors[target] = color;
  }
}

function importFromIterm2(candidate: TerminalConfigCandidate): TerminalConfigImport {
  const colors: Partial<ColorSchemeColors> = {};
  let fontFamily: string | undefined;
  let fontSize: number | undefined;
  let colorSchemeName = candidate.label;
  let content = candidate.content;
  try {
    const parsed = JSON.parse(candidate.content) as unknown;
    const profile = firstItermProfile(parsed);
    if (profile) {
      content = JSON.stringify(profile);
      colorSchemeName =
        typeof profile.Name === "string" ? profile.Name : candidate.label;
      const font = parseItermFont(
        typeof profile["Normal Font"] === "string"
          ? profile["Normal Font"]
          : undefined,
      );
      fontFamily = font?.family;
      fontSize = font?.size;
      copyItermJsonColor(profile, "Background Color", colors, [
        "bg0",
        "terminalBackground",
      ]);
      copyItermJsonColor(profile, "Foreground Color", colors, [
        "fg0",
        "terminalForeground",
      ]);
      copyItermJsonColor(profile, "Cursor Color", colors, [
        "accent",
        "terminalCursor",
      ]);
      copyItermJsonColor(profile, "Selection Color", colors, [
        "bg3",
        "terminalSelection",
      ]);
    }
  } catch {
    // .itermcolors files are XML plists and are parsed below.
  }
  const background = itermColor(content, "Background Color");
  const foreground = itermColor(content, "Foreground Color");
  const cursor = itermColor(content, "Cursor Color");
  const selection = itermColor(content, "Selection Color");
  if (background) {
    colors.bg0 = background;
    colors.terminalBackground = background;
  }
  if (foreground) {
    colors.fg0 = foreground;
    colors.terminalForeground = foreground;
  }
  if (cursor) {
    colors.terminalCursor = cursor;
    colors.accent = cursor;
  }
  if (selection) {
    colors.terminalSelection = selection;
    colors.bg3 = selection;
  }
  return terminalImport(candidate, colors, fontFamily, fontSize, colorSchemeName);
}

function terminalImport(
  candidate: TerminalConfigCandidate,
  colors: Partial<ColorSchemeColors>,
  fontFamily?: string,
  fontSize?: number,
  colorSchemeName?: string,
  keybindings: TerminalKeybinding[] = [],
  shaderPaths: string[] = [],
): TerminalConfigImport {
  const importedFields: string[] = [];
  if (Object.keys(colors).length > 0) {
    importedFields.push("colors");
  }
  if (fontFamily) {
    importedFields.push("font family");
  }
  if (fontSize) {
    importedFields.push("font size");
  }
  if (colorSchemeName) {
    importedFields.push("theme name");
  }
  if (keybindings.length > 0) {
    importedFields.push("keybindings");
  }
  if (shaderPaths.length > 0) {
    importedFields.push("shaders");
  }
  return {
    id: `${candidate.source}:${candidate.path}`,
    source: candidate.source,
    label: candidate.label,
    path: candidate.path,
    colors,
    fontFamily,
    fontSize,
    colorSchemeName,
    keybindings,
    shaderPaths,
    importedFields,
    unsupportedFields: [],
  };
}

export function parseTerminalConfigImport(
  candidate: TerminalConfigCandidate,
): TerminalConfigImport {
  if (candidate.source === "ghostty") {
    return importFromGhostty(candidate);
  }
  if (candidate.source === "alacritty") {
    return importFromAlacritty(candidate);
  }
  if (candidate.source === "wezterm") {
    return importFromWezTerm(candidate);
  }
  if (candidate.source === "kitty") {
    return importFromKitty(candidate);
  }
  if (candidate.source === "windows-terminal") {
    return importFromWindowsTerminal(candidate);
  }
  if (candidate.source === "iterm2") {
    return importFromIterm2(candidate);
  }
  if (candidate.source === "warp") {
    return importFromWarp(candidate);
  }
  if (
    candidate.source === "rio" ||
    candidate.source === "tabby" ||
    candidate.source === "hyper" ||
    candidate.source === "contour" ||
    candidate.source === "foot" ||
    candidate.source === "konsole" ||
    candidate.source === "xfce-terminal" ||
    candidate.source === "terminal-app" ||
    candidate.source === "muxy" ||
    candidate.source === "cmux"
  ) {
    return importFromLooseTerminal(candidate);
  }
  if (
    candidate.source === "tmux" ||
    candidate.source === "zellij"
  ) {
    return terminalImport(
      candidate,
      {},
      undefined,
      undefined,
      undefined,
      parseSimpleMappedKeybindings(candidate.source, candidate.content),
    );
  }
  return terminalImport(candidate, {}, undefined, undefined);
}

export async function detectTerminalConfigImports(): Promise<TerminalConfigImport[]> {
  const candidates = await invoke<TerminalConfigCandidate[]>(
    "fs_detect_terminal_configs",
  );
  return candidates.map(parseTerminalConfigImport);
}

async function readGhosttyTheme(): Promise<GhosttyTheme | null> {
  const config = await invoke<string | null>("fs_read_ghostty_config");
  if (!config) {
    return null;
  }
  return parseGhosttyConfig(config);
}

function applyGhosttyDefaults(
  settings: AppSettings,
  ghosttyTheme: GhosttyTheme | null,
): AppSettings {
  if (!ghosttyTheme) {
    return settings;
  }
  return {
    ...settings,
    fontFamily:
      settings.terminalFontFamily === DEFAULT_SETTINGS.terminalFontFamily &&
      ghosttyTheme.fontFamily
        ? ghosttyTheme.fontFamily
        : settings.terminalFontFamily,
    terminalFontFamily:
      settings.terminalFontFamily === DEFAULT_SETTINGS.terminalFontFamily &&
      ghosttyTheme.fontFamily
        ? ghosttyTheme.fontFamily
        : settings.terminalFontFamily,
    terminalFontSize:
      settings.terminalFontSize === DEFAULT_SETTINGS.terminalFontSize && ghosttyTheme.fontSize
        ? ghosttyTheme.fontSize
        : settings.terminalFontSize,
    ghosttyTheme,
  };
}

export function applyDocumentSettings(settings: AppSettings): void {
  const root = document.documentElement;
  const scheme = resolveColorScheme(settings);
  const colors = colorsForSettings(settings);
  root.dataset.theme = scheme.id;
  root.style.setProperty("--font-ui", settings.uiFontFamily);
  root.style.setProperty("--font-terminal", settings.terminalFontFamily);
  root.style.setProperty("--font-editor", settings.editorFontFamily);
  root.style.setProperty("--font-mono", settings.terminalFontFamily);
  root.style.setProperty("--font-size-ui", `${settings.uiFontSize}px`);
  root.style.setProperty("--font-size-terminal", `${settings.terminalFontSize}px`);
  root.style.setProperty("--font-size-editor", `${settings.editorFontSize}px`);
  for (const key of COLOR_SCHEME_COLOR_KEYS) {
    root.style.setProperty(COLOR_SCHEME_CSS_VARIABLES[key], colors[key]);
  }
}

function prefersLightTheme(): boolean {
  return (
    typeof window.matchMedia === "function" &&
    window.matchMedia("(prefers-color-scheme: light)").matches
  );
}

export function resolveThemeMode(theme: ThemeMode): Exclude<ThemeMode, "system"> {
  return theme === "system"
    ? prefersLightTheme()
      ? "vscode-light-plus"
      : "vscode-dark-plus"
    : theme;
}

function builtInColorScheme(id: BuiltInThemeMode): ColorScheme {
  return (
    BUILT_IN_COLOR_SCHEMES.find((scheme) => scheme.id === id) ??
    BUILT_IN_COLOR_SCHEMES[0]
  );
}

export function resolveColorScheme(settings: AppSettings): ResolvedColorScheme {
  const resolvedTheme = resolveThemeMode(settings.theme);
  if (resolvedTheme === "custom") {
    return {
      id: "custom",
      label: settings.customColorScheme.label || DEFAULT_CUSTOM_COLOR_SCHEME.label,
      colors: settings.customColorScheme.colors,
    };
  }
  return builtInColorScheme(resolvedTheme);
}

function colorsForSettings(settings: AppSettings): ColorSchemeColors {
  const scheme = resolveColorScheme(settings);
  const colors = { ...scheme.colors };
  if (scheme.id === "terminal") {
    const importedColors = settings.customColorScheme?.colors;
    if (importedColors) {
      colors.bg0 = importedColors.terminalBackground;
      colors.bg3 = importedColors.terminalSelection;
      colors.fg0 = importedColors.terminalForeground;
      colors.accent = importedColors.terminalCursor;
      colors.terminalBackground = importedColors.terminalBackground;
      colors.terminalForeground = importedColors.terminalForeground;
      colors.terminalCursor = importedColors.terminalCursor;
      colors.terminalSelection = importedColors.terminalSelection;
    }
    if (settings.ghosttyTheme?.background) {
      colors.bg0 = settings.ghosttyTheme.background;
      colors.terminalBackground = settings.ghosttyTheme.background;
    }
    if (settings.ghosttyTheme?.foreground) {
      colors.fg0 = settings.ghosttyTheme.foreground;
      colors.terminalForeground = settings.ghosttyTheme.foreground;
      colors.terminalCursor = settings.ghosttyTheme.foreground;
    }
    if (settings.ghosttyTheme?.palette[4]) {
      colors.accent = settings.ghosttyTheme.palette[4];
      colors.terminalSelection = settings.ghosttyTheme.palette[4];
    }
  } else if (scheme.id === "github-dark") {
    if (settings.ghosttyTheme?.background) {
      colors.bg0 = settings.ghosttyTheme.background;
      colors.terminalBackground = settings.ghosttyTheme.background;
    }
    if (settings.ghosttyTheme?.foreground) {
      colors.fg0 = settings.ghosttyTheme.foreground;
      colors.terminalForeground = settings.ghosttyTheme.foreground;
    }
  }
  return colors;
}

export function terminalThemeForSettings(settings: AppSettings): TerminalTheme {
  const colors = colorsForSettings(settings);
  return {
    background: colors.terminalBackground,
    foreground: colors.terminalForeground,
    cursor: colors.terminalCursor,
    selectionBackground: colors.terminalSelection,
  };
}

export function terminalScrollbackLinesForSettings(settings: AppSettings): number {
  return settings.terminalScrollbackLines === 0
    ? TERMINAL_EFFECTIVE_UNLIMITED_SCROLLBACK_LINES
    : settings.terminalScrollbackLines;
}
