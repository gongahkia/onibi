import type {
  ControllableSessionSnapshot,
  SessionOutputChunk,
  SessionOutputStream
} from "../types";

const MAX_CHUNK_COUNT_PER_SESSION = 5000;

export interface OutputEntry {
  id: string;
  sessionId: string;
  stream: SessionOutputStream;
  timestamp: string;
  text: string;
}

export type OutputBySession = Record<string, OutputEntry[]>;

export function sortSessionsByRecentActivity(
  sessions: ControllableSessionSnapshot[]
): ControllableSessionSnapshot[] {
  return [...sessions].sort((left, right) => {
    return Date.parse(right.lastActivityAt) - Date.parse(left.lastActivityAt);
  });
}

export function upsertSession(
  sessions: ControllableSessionSnapshot[],
  nextSession: ControllableSessionSnapshot
): ControllableSessionSnapshot[] {
  const withoutExisting = sessions.filter((session) => session.id !== nextSession.id);
  return sortSessionsByRecentActivity([...withoutExisting, nextSession]);
}

export function removeSession(
  sessions: ControllableSessionSnapshot[],
  sessionId: string
): ControllableSessionSnapshot[] {
  return sessions.filter((session) => session.id !== sessionId);
}

export function replaceBuffer(
  outputBySession: OutputBySession,
  sessionId: string,
  chunks: SessionOutputChunk[]
): OutputBySession {
  return {
    ...outputBySession,
    [sessionId]: chunks.map(toOutputEntry)
  };
}

export function appendChunk(
  outputBySession: OutputBySession,
  chunk: SessionOutputChunk
): OutputBySession {
  const existing = outputBySession[chunk.sessionId] ?? [];
  const next = [...existing, toOutputEntry(chunk)];

  if (next.length > MAX_CHUNK_COUNT_PER_SESSION) {
    next.splice(0, next.length - MAX_CHUNK_COUNT_PER_SESSION);
  }

  return {
    ...outputBySession,
    [chunk.sessionId]: next
  };
}

export function renderOutput(entries: OutputEntry[]): string {
  return entries.map((entry) => entry.text).join("");
}

function toOutputEntry(chunk: SessionOutputChunk): OutputEntry {
  return {
    id: chunk.id,
    sessionId: chunk.sessionId,
    stream: chunk.stream,
    timestamp: chunk.timestamp,
    text: decodeChunkText(chunk.data)
  };
}

function decodeChunkText(base64Data: string): string {
  try {
    const binary = atob(base64Data);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) {
      bytes[index] = binary.charCodeAt(index);
    }
    return new TextDecoder().decode(bytes);
  } catch {
    return "[invalid output chunk]";
  }
}
