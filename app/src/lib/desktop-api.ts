import {
  ensureApprovalConnectionConfig,
  subscribeApprovalEvents,
  type ApprovalClientOptions,
  type DesktopCommandMessage,
} from "./approval-client";
import { listCommandBlocks } from "./command-blocks";
import {
  AGENT_KINDS,
  restoreArrangement,
  sessionAttentionState,
  spawnAgentSession,
  useSessionStore,
  workspaceFromPath,
  type AgentKind,
  type Arrangement,
  type Session,
  type Workspace,
} from "./sessions";
import { ptyWrite } from "./tauri-bridge";

const PROTOCOL_VERSION = "1.0";

interface DesktopSnapshot {
  protocol_version: string;
  sessions: Array<{
    id: string;
    title: string;
    agent: string;
    workspaceId: string;
    cwd?: string | null;
    status: string;
    attention: string;
    previewUrl?: string | null;
    commandBlockCount?: number;
    lastCommandBlockId?: string | null;
  }>;
  arrangements: Array<{ id: string; name: string }>;
  updatedAt: number;
}

function desktopEndpoint(path: string, port: number): string {
  return `http://127.0.0.1:${port}${path}`;
}

function authHeaders(config: ApprovalClientOptions): Record<string, string> {
  const headers: Record<string, string> = { "content-type": "application/json" };
  if (config.token) {
    headers.authorization = `Bearer ${config.token}`;
  }
  return headers;
}

function namedRefs<T extends { id: string; name: string }>(items: T[]) {
  return items.map((item) => ({ id: item.id, name: item.name }));
}

function snapshotSession(session: Session) {
  const state = useSessionStore.getState();
  return {
    id: session.id,
    title: session.title,
    agent: session.agent,
    workspaceId: session.workspaceId,
    cwd: session.cwd ?? null,
    status: session.status,
    attention: sessionAttentionState(session),
    previewUrl: session.preview?.url ?? null,
    commandBlockCount: state.commandBlocks.filter(
      (block) => block.sessionId === session.id,
    ).length,
    lastCommandBlockId: session.lastCommandBlockId ?? null,
  };
}

function desktopSnapshot(): DesktopSnapshot {
  const state = useSessionStore.getState();
  return {
    protocol_version: PROTOCOL_VERSION,
    sessions: state.sessions.map(snapshotSession),
    arrangements: namedRefs(state.arrangements),
    updatedAt: Date.now(),
  };
}

async function postDesktopSnapshot(config: ApprovalClientOptions): Promise<void> {
  const port = config.port ?? 17893;
  await fetch(desktopEndpoint("/v1/desktop/state", port), {
    method: "POST",
    headers: authHeaders(config),
    body: JSON.stringify(desktopSnapshot()),
  }).catch(() => undefined);
}

function stringPayloadField(payload: unknown, key: string): string | null {
  if (typeof payload !== "object" || payload === null) {
    return null;
  }
  const value = (payload as Record<string, unknown>)[key];
  return typeof value === "string" && value.trim() ? value : null;
}

function findArrangement(value: string, arrangements: Arrangement[]): Arrangement | null {
  const lower = value.toLowerCase();
  return (
    arrangements.find((arrangement) => arrangement.id === value) ??
    arrangements.find((arrangement) => arrangement.name.toLowerCase() === lower) ??
    null
  );
}

function findAgent(value: string | null, fallback: AgentKind): AgentKind | null {
  if (!value) {
    return fallback;
  }
  const lower = value.toLowerCase();
  return (
    AGENT_KINDS.find((agent) => agent === value || agent.toLowerCase() === lower) ??
    null
  );
}

function findWorkspace(value: string, workspaces: Workspace[]): Workspace | null {
  return (
    workspaces.find((workspace) => workspace.id === value) ??
    workspaces.find((workspace) => workspace.path === value) ??
    workspaces.find((workspace) => workspace.name.toLowerCase() === value.toLowerCase()) ??
    null
  );
}

async function workspaceForCommand(value: string): Promise<Workspace> {
  const state = useSessionStore.getState();
  const existing = findWorkspace(value, state.workspaces);
  if (existing) {
    return existing;
  }
  const workspace = await workspaceFromPath(value);
  useSessionStore.getState().addWorkspace(workspace);
  return workspace;
}

async function executeDesktopCommand(message: DesktopCommandMessage): Promise<void> {
  const state = useSessionStore.getState();
  if (message.kind === "session-focus") {
    const sessionId = stringPayloadField(message.payload, "sessionId");
    if (sessionId) {
      state.setActiveSession(sessionId);
    }
    return;
  }
  if (message.kind === "session-input") {
    const sessionId = stringPayloadField(message.payload, "sessionId");
    const text = stringPayloadField(message.payload, "text");
    if (sessionId && text) {
      const payload = text.endsWith("\n") ? text : `${text}\n`;
      await ptyWrite(sessionId, new TextEncoder().encode(payload));
    }
    return;
  }
  if (message.kind === "arrangement-restore") {
    const value = stringPayloadField(message.payload, "arrangementId");
    const arrangement = value ? findArrangement(value, state.arrangements) : null;
    if (arrangement) {
      await restoreArrangement(arrangement.id);
    }
    return;
  }
  if (message.kind === "session-launch") {
    const agentValue = stringPayloadField(message.payload, "agent");
    const workspaceValue = stringPayloadField(message.payload, "workspace");
    if (!workspaceValue) {
      return;
    }
    const agent = findAgent(agentValue, state.settings.defaultAgent);
    if (!agent) {
      return;
    }
    const workspace = await workspaceForCommand(workspaceValue);
    const prompt = stringPayloadField(message.payload, "prompt") ?? "";
    const cwd = stringPayloadField(message.payload, "cwd");
    await spawnAgentSession(agent, workspace, prompt, null, { cwd });
  }
}

export async function startDesktopBridge(): Promise<() => void> {
  const config = await ensureApprovalConnectionConfig();
  void listCommandBlocks({ limit: 150 })
    .then((blocks) => useSessionStore.getState().setCommandBlocks(blocks))
    .catch(() => undefined);
  let disposed = false;
  let timer: number | null = null;
  const queueSnapshot = () => {
    if (disposed || timer !== null) {
      return;
    }
    timer = window.setTimeout(() => {
      timer = null;
      void postDesktopSnapshot(config);
    }, 150);
  };
  const unsubscribeStore = useSessionStore.subscribe(queueSnapshot);
  queueSnapshot();
  const disposeRealtime = await subscribeApprovalEvents(config, (message) => {
    if (message.type === "desktop-command") {
      void executeDesktopCommand(message).then(queueSnapshot);
    }
  });
  const interval = window.setInterval(queueSnapshot, 3000);
  return () => {
    disposed = true;
    if (timer !== null) {
      window.clearTimeout(timer);
    }
    window.clearInterval(interval);
    unsubscribeStore();
    disposeRealtime();
  };
}
