import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

export type PtyId = string;
export type PtyShellMode = "auto" | "login" | "non_login";

export interface PtySpawnRequest {
  command: string;
  args: string[];
  cwd: string | null;
  env: Array<[string, string]>;
  shellMode?: PtyShellMode;
  rows: number;
  cols: number;
  name?: string | null;
  agent?: string | null;
  workspaceId?: string | null;
  title?: string | null;
  remote?: import("./sessions").RemoteSessionMetadata | null;
}

export interface PtyNotificationPayload {
  source: "osc9" | "osc99" | "osc777";
  title: string;
  body?: string | null;
  urgency?: string | null;
}

export type PtyWireEvent =
  | { type: "data"; data: string; offset: number }
  | { type: "exit"; code: number; signal: string | null }
  | ({ type: "notification" } & PtyNotificationPayload);

export interface PtyReplaySnapshot {
  data: string;
  startOffset: number;
  endOffset: number;
}

export interface PtySessionRestart {
  command: string;
  args: string[];
  cwd: string | null;
  env: Array<[string, string]>;
  shellMode?: PtyShellMode;
  remote?: import("./sessions").RemoteSessionMetadata | null;
}

export interface PtyProviderResume {
  command: string;
  args: string[];
  source?: string | null;
}

export interface PtyProviderSession {
  agent: string;
  providerSessionId?: string | null;
  conversationId?: string | null;
  resume?: PtyProviderResume | null;
  updatedAt: number;
}

export interface PtySessionMetadata {
  id: string;
  paneId: string;
  name?: string | null;
  agent?: string | null;
  workspaceId?: string | null;
  cwd?: string | null;
  title?: string | null;
  status: "idle" | "working" | "blocked" | "done";
  lifecycle: "running" | "stale" | "stopped";
  rows: number;
  cols: number;
  createdAt: number;
  updatedAt: number;
  processId?: number | null;
  stoppedAt?: number | null;
  exitCode?: number | null;
  exitSignal?: string | null;
  restart?: PtySessionRestart | null;
  provider?: PtyProviderSession | null;
  remote?: import("./sessions").RemoteSessionMetadata | null;
}

export interface PtyAttachResult {
  ok: boolean;
  attached: boolean;
  relaunched: boolean;
  previousSessionId?: string | null;
  id: string;
  sessionId: string;
  paneId: string;
  session: PtySessionMetadata;
}

export function shellPath(): string {
  return "";
}

export function ptySpawn(req: PtySpawnRequest): Promise<PtyId> {
  return invoke<PtyId>("pty_spawn", { req });
}

export function ptyWrite(id: PtyId, data: Uint8Array): Promise<void> {
  return invoke("pty_write", { id, data: Array.from(data) });
}

export function ptyResize(id: PtyId, rows: number, cols: number): Promise<void> {
  return invoke("pty_resize", { id, rows, cols });
}

export function ptyKill(id: PtyId): Promise<void> {
  return invoke("pty_kill", { id });
}

export function ptyList(): Promise<PtyId[]> {
  return invoke<PtyId[]>("pty_list");
}

export function ptySessions(): Promise<PtySessionMetadata[]> {
  return invoke<PtySessionMetadata[]>("pty_sessions");
}

export function sessionAttach(id: string): Promise<PtyAttachResult> {
  return invoke<PtyAttachResult>("session_attach", { id });
}

export function ptyReplay(id: PtyId): Promise<PtyReplaySnapshot | null> {
  return invoke<PtyReplaySnapshot | null>("pty_replay", { id });
}

export function subscribePty(
  id: PtyId,
  handler: (event: PtyWireEvent) => void,
): Promise<UnlistenFn> {
  return listen<PtyWireEvent>(`pty:${id}`, (event) => handler(event.payload));
}
