import { invoke } from "@tauri-apps/api/core";
import { load, type Store } from "@tauri-apps/plugin-store";
import { create } from "zustand";
import {
  ptyList,
  ptySpawn,
  shellPath,
  type PtyId,
  type PtySpawnRequest,
} from "./tauri-bridge";

export const AGENT_KINDS = [
  "claude-code",
  "codex",
  "opencode",
  "gemini",
  "aider",
  "cursor",
  "goose",
  "shell",
] as const;

export type AgentKind = (typeof AGENT_KINDS)[number];

export type SessionStatus =
  | "idle"
  | "running"
  | "awaiting-approval"
  | "completed"
  | "error";

export interface Session {
  id: string;
  agent: AgentKind;
  workspaceId: string;
  title: string;
  status: SessionStatus;
  createdAt: number;
  pendingApprovals: string[];
}

export interface Workspace {
  id: string;
  path: string;
  name: string;
}

export interface FsEntry {
  name: string;
  path: string;
  kind: "file" | "dir";
  size: number;
}

export interface SelectedFile {
  workspaceId: string;
  workspaceRoot: string;
  path: string;
  name: string;
  size: number;
}

export type ThemeMode =
  | "system"
  | "dark"
  | "light"
  | "graphite"
  | "ember"
  | "forest"
  | "ocean"
  | "violet"
  | "solarized-dark"
  | "solarized-light"
  | "paper"
  | "high-contrast"
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

export interface AppSettings {
  theme: ThemeMode;
  fontFamily: string;
  fontSize: number;
  tabBarOrientation: TabBarOrientation;
  tabBarPosition: TabBarPosition;
  showHiddenFiles: boolean;
  showFileIcons: boolean;
  agentCommands: Record<AgentKind, string>;
  customColorScheme: CustomColorScheme;
  ghosttyTheme: GhosttyTheme | null;
}

type PersistedState = {
  sessions?: Session[];
  workspaces?: Workspace[];
  settings?: Partial<AppSettings>;
};

type SessionStore = {
  hydrated: boolean;
  sessions: Session[];
  activeSessionId: string | null;
  workspaces: Workspace[];
  selectedFile: SelectedFile | null;
  settings: AppSettings;
  setHydrated: (hydrated: boolean) => void;
  setActiveSession: (id: string | null) => void;
  addSession: (session: Session) => void;
  updateSession: (id: string, patch: Partial<Session>) => void;
  removeSession: (id: string) => void;
  setWorkspaces: (workspaces: Workspace[]) => void;
  addWorkspace: (workspace: Workspace) => void;
  removeWorkspace: (id: string) => void;
  selectFile: (file: SelectedFile | null) => void;
  updateSettings: (patch: Partial<AppSettings>) => void;
  updateAgentCommand: (agent: AgentKind, command: string) => void;
};

const STORE_PATH = "settings.json";

export const AGENT_LABELS: Record<AgentKind, string> = {
  "claude-code": "Claude Code",
  codex: "Codex",
  opencode: "OpenCode",
  gemini: "Gemini",
  aider: "Aider",
  cursor: "Cursor",
  goose: "Goose",
  shell: "Plain shell",
};

export const DEFAULT_AGENT_COMMANDS: Record<AgentKind, string> = {
  "claude-code": "claude code",
  codex: "codex",
  opencode: "opencode",
  gemini: "gemini",
  aider: "aider",
  cursor: "cursor-agent",
  goose: "goose session",
  shell: "",
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
    id: "dark",
    label: "Dark",
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
    id: "light",
    label: "Light",
    colors: {
      bg0: "#f7f4ee",
      bg1: "#ebe7de",
      bg2: "#ffffff",
      bg3: "#d8d2c7",
      fg0: "#18232a",
      fg1: "#43525b",
      fg2: "#6a7780",
      accent: "#b87913",
      accent2: "#087f72",
      danger: "#b3261e",
      flash: "#b87913",
      border: "#cac3b7",
      terminalBackground: "#fbfaf7",
      terminalForeground: "#18232a",
      terminalCursor: "#a46300",
      terminalSelection: "#cbe7e3",
    },
  },
  {
    id: "graphite",
    label: "Graphite",
    colors: {
      bg0: "#0d0f12",
      bg1: "#15181c",
      bg2: "#20242a",
      bg3: "#2d333b",
      fg0: "#f0f3f6",
      fg1: "#b9c0c9",
      fg2: "#828c99",
      accent: "#8ab4f8",
      accent2: "#8bd3c7",
      danger: "#ff6b6b",
      flash: "#8ab4f8",
      border: "#30363d",
      terminalBackground: "#0d0f12",
      terminalForeground: "#f0f3f6",
      terminalCursor: "#8ab4f8",
      terminalSelection: "#334155",
    },
  },
  {
    id: "ember",
    label: "Ember",
    colors: {
      bg0: "#140d0b",
      bg1: "#201512",
      bg2: "#2e1d18",
      bg3: "#3b281f",
      fg0: "#fff1e8",
      fg1: "#d8b8a7",
      fg2: "#9d7768",
      accent: "#ffb454",
      accent2: "#ff7a59",
      danger: "#ff5c5c",
      flash: "#ffb454",
      border: "#4a2c22",
      terminalBackground: "#170f0d",
      terminalForeground: "#ffe8d6",
      terminalCursor: "#ffb454",
      terminalSelection: "#5f3226",
    },
  },
  {
    id: "forest",
    label: "Forest",
    colors: {
      bg0: "#07110d",
      bg1: "#0e1a15",
      bg2: "#15261f",
      bg3: "#1f352b",
      fg0: "#ecf7ef",
      fg1: "#b7c9bd",
      fg2: "#78917f",
      accent: "#a3d977",
      accent2: "#4ecdc4",
      danger: "#ff6f61",
      flash: "#a3d977",
      border: "#244537",
      terminalBackground: "#09130f",
      terminalForeground: "#e8f5e9",
      terminalCursor: "#a3d977",
      terminalSelection: "#285243",
    },
  },
  {
    id: "ocean",
    label: "Ocean",
    colors: {
      bg0: "#07131a",
      bg1: "#0d1d27",
      bg2: "#142b38",
      bg3: "#1d3c4d",
      fg0: "#edf8ff",
      fg1: "#b6cbd7",
      fg2: "#7895a5",
      accent: "#6ec6ff",
      accent2: "#3dd6c6",
      danger: "#ff6b7a",
      flash: "#6ec6ff",
      border: "#254454",
      terminalBackground: "#07131a",
      terminalForeground: "#e6f7ff",
      terminalCursor: "#6ec6ff",
      terminalSelection: "#24546b",
    },
  },
  {
    id: "violet",
    label: "Violet",
    colors: {
      bg0: "#120d19",
      bg1: "#1b1424",
      bg2: "#261c33",
      bg3: "#342645",
      fg0: "#f7edff",
      fg1: "#cbbad9",
      fg2: "#907ca3",
      accent: "#c792ea",
      accent2: "#82aaff",
      danger: "#ff6e91",
      flash: "#c792ea",
      border: "#3e2d52",
      terminalBackground: "#110c18",
      terminalForeground: "#f2e8ff",
      terminalCursor: "#c792ea",
      terminalSelection: "#4a3563",
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
  {
    id: "solarized-light",
    label: "Solarized Light",
    colors: {
      bg0: "#fdf6e3",
      bg1: "#eee8d5",
      bg2: "#f6f0dc",
      bg3: "#ded6bf",
      fg0: "#073642",
      fg1: "#586e75",
      fg2: "#839496",
      accent: "#b58900",
      accent2: "#2aa198",
      danger: "#dc322f",
      flash: "#b58900",
      border: "#d6ccad",
      terminalBackground: "#fdf6e3",
      terminalForeground: "#073642",
      terminalCursor: "#b58900",
      terminalSelection: "#eee8d5",
    },
  },
  {
    id: "paper",
    label: "Paper",
    colors: {
      bg0: "#fbf7ef",
      bg1: "#f0eadf",
      bg2: "#fffdf8",
      bg3: "#ded6c8",
      fg0: "#1f2528",
      fg1: "#4f5a5f",
      fg2: "#7b868a",
      accent: "#8f5c2e",
      accent2: "#267a7a",
      danger: "#b13d35",
      flash: "#8f5c2e",
      border: "#d1c7b8",
      terminalBackground: "#fffcf5",
      terminalForeground: "#1f2528",
      terminalCursor: "#8f5c2e",
      terminalSelection: "#d7e6e4",
    },
  },
  {
    id: "high-contrast",
    label: "High Contrast",
    colors: {
      bg0: "#000000",
      bg1: "#080808",
      bg2: "#111111",
      bg3: "#1f1f1f",
      fg0: "#ffffff",
      fg1: "#d7d7d7",
      fg2: "#a8a8a8",
      accent: "#ffd400",
      accent2: "#00d9ff",
      danger: "#ff4d4d",
      flash: "#ffd400",
      border: "#595959",
      terminalBackground: "#000000",
      terminalForeground: "#ffffff",
      terminalCursor: "#ffd400",
      terminalSelection: "#144f63",
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

export const DEFAULT_SETTINGS: AppSettings = {
  theme: "dark",
  fontFamily: "Menlo, Monaco, monospace",
  fontSize: 13,
  tabBarOrientation: "vertical",
  tabBarPosition: "left",
  showHiddenFiles: false,
  showFileIcons: true,
  agentCommands: DEFAULT_AGENT_COMMANDS,
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

function mergeSettings(settings: Partial<AppSettings> | undefined): AppSettings {
  const agentCommands = isRecord(settings?.agentCommands)
    ? {
        ...DEFAULT_AGENT_COMMANDS,
        ...(settings?.agentCommands as Partial<Record<AgentKind, string>>),
      }
    : { ...DEFAULT_AGENT_COMMANDS };
  const merged = {
    ...DEFAULT_SETTINGS,
    ...settings,
  };
  return {
    ...merged,
    theme: isThemeMode(merged.theme) ? merged.theme : DEFAULT_SETTINGS.theme,
    fontFamily:
      typeof merged.fontFamily === "string" && merged.fontFamily.trim()
        ? merged.fontFamily
        : DEFAULT_SETTINGS.fontFamily,
    fontSize:
      Number.isFinite(merged.fontSize) && merged.fontSize > 0
        ? Math.min(Math.max(merged.fontSize, 10), 24)
        : DEFAULT_SETTINGS.fontSize,
    tabBarOrientation: isTabBarOrientation(merged.tabBarOrientation)
      ? merged.tabBarOrientation
      : DEFAULT_SETTINGS.tabBarOrientation,
    tabBarPosition: isTabBarPosition(merged.tabBarPosition)
      ? merged.tabBarPosition
      : DEFAULT_SETTINGS.tabBarPosition,
    showHiddenFiles: Boolean(merged.showHiddenFiles),
    showFileIcons: Boolean(merged.showFileIcons),
    agentCommands,
    customColorScheme: normalizeCustomColorScheme(merged.customColorScheme),
    ghosttyTheme: settings?.ghosttyTheme ?? null,
  };
}

function snapshot(state: SessionStore): PersistedState {
  return {
    sessions: state.sessions,
    workspaces: state.workspaces,
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
  workspaces: [],
  selectedFile: null,
  settings: DEFAULT_SETTINGS,
  setHydrated: (hydrated) => set({ hydrated }),
  setActiveSession: (id) => {
    set({ activeSessionId: id, selectedFile: null });
    persistLater();
  },
  addSession: (session) => {
    set((state) => ({
      sessions: [
        ...state.sessions.filter((existing) => existing.id !== session.id),
        session,
      ],
      activeSessionId: session.id,
      selectedFile: null,
    }));
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
  removeSession: (id) => {
    set((state) => {
      const sessions = state.sessions.filter((session) => session.id !== id);
      return {
        sessions,
        activeSessionId:
          state.activeSessionId === id
            ? sessions[sessions.length - 1]?.id ?? null
            : state.activeSessionId,
      };
    });
    persistLater();
  },
  setWorkspaces: (workspaces) => {
    set({ workspaces });
    persistLater();
  },
  addWorkspace: (workspace) => {
    set((state) => ({
      workspaces: [
        ...state.workspaces.filter((existing) => existing.id !== workspace.id),
        workspace,
      ],
    }));
    persistLater();
  },
  removeWorkspace: (id) => {
    set((state) => ({
      workspaces: state.workspaces.filter((workspace) => workspace.id !== id),
      selectedFile:
        state.selectedFile?.workspaceId === id ? null : state.selectedFile,
      sessions: state.sessions.filter((session) => session.workspaceId !== id),
      activeSessionId:
        state.sessions.some(
          (session) => session.workspaceId === id && session.id === state.activeSessionId,
        )
          ? null
          : state.activeSessionId,
    }));
    persistLater();
  },
  selectFile: (file) => set({ selectedFile: file }),
  updateSettings: (patch) => {
    set((state) => ({ settings: mergeSettings({ ...state.settings, ...patch }) }));
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

export async function hydrateSessionStore(): Promise<void> {
  const state = useSessionStore.getState();
  if (state.hydrated) {
    return;
  }
  try {
    const store = await getStore();
    const [sessions, workspaces, settings] = await Promise.all([
      store.get<Session[]>("sessions"),
      store.get<Workspace[]>("workspaces"),
      store.get<Partial<AppSettings>>("settings"),
    ]);
    const livePtys = new Set(await ptyList().catch(() => []));
    const liveSessions = (sessions ?? []).filter((session) =>
      livePtys.has(session.id),
    );
    const ghosttyTheme = await readGhosttyTheme().catch(() => null);
    const mergedSettings = applyGhosttyDefaults(mergeSettings(settings), ghosttyTheme);
    useSessionStore.setState({
      sessions: liveSessions,
      activeSessionId: liveSessions[liveSessions.length - 1]?.id ?? null,
      workspaces: workspaces ?? [],
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

export async function writeWorkspaceFile(
  workspaceRoot: string,
  path: string,
  data: Uint8Array,
): Promise<void> {
  await invoke("fs_write_file", {
    root: workspaceRoot,
    path,
    data: Array.from(data),
  });
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

export function sessionTitle(agent: AgentKind, workspace: Workspace): string {
  return `${AGENT_LABELS[agent]} · ${workspace.name}`;
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
): Pick<PtySpawnRequest, "command" | "args"> {
  if (agent === "shell") {
    return { command: shellPath(), args: [] };
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
  const [command] = splitCommandLine(
    settings.agentCommands[agent] || DEFAULT_AGENT_COMMANDS[agent],
  );
  if (!command) {
    return null;
  }
  return invoke<string | null>("fs_resolve_binary", { command });
}

export async function spawnAgentSession(
  agent: AgentKind,
  workspace: Workspace,
  initialPrompt: string,
): Promise<PtyId> {
  const settings = useSessionStore.getState().settings;
  const launch = launchCommandForAgent(agent, settings, initialPrompt);
  const id = await ptySpawn({
    command: launch.command,
    args: launch.args,
    cwd: workspace.path,
    env: [],
    rows: 30,
    cols: 100,
  });
  useSessionStore.getState().addSession({
    id,
    agent,
    workspaceId: workspace.id,
    title: sessionTitle(agent, workspace),
    status: "running",
    createdAt: Date.now(),
    pendingApprovals: [],
  });
  return id;
}

function parseGhosttyColor(value: string): string | undefined {
  const trimmed = value.trim();
  return /^#[0-9a-fA-F]{6}$/.test(trimmed) ? trimmed : undefined;
}

export function parseGhosttyConfig(config: string): GhosttyTheme {
  const theme: GhosttyTheme = { palette: {} };
  for (const rawLine of config.split(/\r?\n/)) {
    const line = rawLine.replace(/\s+#.*$/, "").trim();
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
      settings.fontFamily === DEFAULT_SETTINGS.fontFamily && ghosttyTheme.fontFamily
        ? ghosttyTheme.fontFamily
        : settings.fontFamily,
    fontSize:
      settings.fontSize === DEFAULT_SETTINGS.fontSize && ghosttyTheme.fontSize
        ? ghosttyTheme.fontSize
        : settings.fontSize,
    ghosttyTheme,
  };
}

export function applyDocumentSettings(settings: AppSettings): void {
  const root = document.documentElement;
  const scheme = resolveColorScheme(settings);
  const colors = colorsForSettings(settings);
  root.dataset.theme = scheme.id;
  root.style.setProperty("--font-ui", settings.fontFamily);
  root.style.setProperty("--font-mono", settings.fontFamily);
  root.style.setProperty("--font-size-terminal", `${settings.fontSize}px`);
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
  return theme === "system" ? (prefersLightTheme() ? "light" : "dark") : theme;
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
  if (scheme.id === "dark") {
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
