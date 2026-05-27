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

export type ThemeMode = "dark" | "light" | "system";
export type TabBarOrientation = "vertical" | "horizontal";
export type TabBarPosition = "left" | "right" | "top" | "bottom";

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
  agentCommands: Record<AgentKind, string>;
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

export const DEFAULT_SETTINGS: AppSettings = {
  theme: "dark",
  fontFamily: "Menlo, Monaco, monospace",
  fontSize: 13,
  tabBarOrientation: "vertical",
  tabBarPosition: "left",
  showHiddenFiles: false,
  agentCommands: DEFAULT_AGENT_COMMANDS,
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

function mergeSettings(settings: Partial<AppSettings> | undefined): AppSettings {
  const agentCommands = isRecord(settings?.agentCommands)
    ? {
        ...DEFAULT_AGENT_COMMANDS,
        ...(settings?.agentCommands as Partial<Record<AgentKind, string>>),
      }
    : DEFAULT_AGENT_COMMANDS;
  return {
    ...DEFAULT_SETTINGS,
    ...settings,
    agentCommands,
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
  const prefersLight = window.matchMedia("(prefers-color-scheme: light)").matches;
  const resolvedTheme =
    settings.theme === "system" ? (prefersLight ? "light" : "dark") : settings.theme;
  root.dataset.theme = resolvedTheme;
  root.style.setProperty("--font-ui", settings.fontFamily);
  root.style.setProperty("--font-mono", settings.fontFamily);
  root.style.setProperty("--font-size-terminal", `${settings.fontSize}px`);
  if (settings.ghosttyTheme?.background) {
    root.style.setProperty("--bg-0", settings.ghosttyTheme.background);
  }
  if (settings.ghosttyTheme?.foreground) {
    root.style.setProperty("--fg-0", settings.ghosttyTheme.foreground);
  }
}
