import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import type {
  PtyAttachResult,
  PtyReplaySnapshot,
  PtyProviderSession,
  PtySessionMetadata,
  PtySpawnRequest,
  PtyWireEvent,
  RemoteSshBootstrapRequest,
  RemoteSshBootstrapResult,
  RemoteSshDaemonRequest,
  RemoteSshDaemonResult,
  RemoteSshDaemonSessionRequest,
  RemoteSshDaemonSessionResult,
  RemoteSshStageFileRequest,
  RemoteSshStageFileResult,
  ShellMode,
} from "./contracts/generated";

export type PtyId = string;
export type PtyShellMode = ShellMode;
export type {
  PtyAttachResult,
  PtyProviderSession,
  PtyReplaySnapshot,
  PtySessionMetadata,
  PtySpawnRequest,
  PtyWireEvent,
  RemoteSshBootstrapRequest,
  RemoteSshBootstrapResult,
  RemoteSshDaemonRequest,
  RemoteSshDaemonResult,
  RemoteSshDaemonSessionRequest,
  RemoteSshDaemonSessionResult,
  RemoteSshStageFileRequest,
  RemoteSshStageFileResult,
};

export function shellPath(): string {
  return "";
}

export function ptySpawn(req: PtySpawnRequest): Promise<PtyId> {
  return invoke<PtyId>("pty_spawn", { req });
}

export function ptyWrite(id: PtyId, data: Uint8Array): Promise<void> {
  return invoke("pty_write", { id, data: Array.from(data) });
}

export function clipboardReadImagePng(): Promise<number[] | null> {
  return invoke<number[] | null>("clipboard_read_image_png");
}

export function remoteSshBootstrap(
  req: RemoteSshBootstrapRequest,
): Promise<RemoteSshBootstrapResult> {
  return invoke<RemoteSshBootstrapResult>("remote_ssh_bootstrap", { req });
}

export function remoteSshDaemon(req: RemoteSshDaemonRequest): Promise<RemoteSshDaemonResult> {
  return invoke<RemoteSshDaemonResult>("remote_ssh_daemon", { req });
}

export function remoteSshDaemonSession(
  req: RemoteSshDaemonSessionRequest,
): Promise<RemoteSshDaemonSessionResult> {
  return invoke<RemoteSshDaemonSessionResult>("remote_ssh_daemon_session", { req });
}

export function remoteSshStageFile(
  req: RemoteSshStageFileRequest,
): Promise<RemoteSshStageFileResult> {
  return invoke<RemoteSshStageFileResult>("remote_ssh_stage_file", { req });
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
