import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";

export type PtyId = string;

export interface PtySpawnRequest {
  command: string;
  args: string[];
  cwd: string | null;
  env: Array<[string, string]>;
  rows: number;
  cols: number;
}

export interface PtyNotificationPayload {
  source: "osc9" | "osc99" | "osc777";
  title: string;
  body?: string | null;
  urgency?: string | null;
}

export type PtyWireEvent =
  | { type: "data"; data: string }
  | { type: "exit"; code: number; signal: string | null }
  | ({ type: "notification" } & PtyNotificationPayload);

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

export function subscribePty(
  id: PtyId,
  handler: (event: PtyWireEvent) => void,
): Promise<UnlistenFn> {
  return listen<PtyWireEvent>(`pty:${id}`, (event) => handler(event.payload));
}
