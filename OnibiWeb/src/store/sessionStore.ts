import type {
  ControllableSessionSnapshot,
  SessionOutputChunk,
  SessionOutputStream
} from "../types";

const MAX_CHUNK_COUNT_PER_SESSION = 5000;
const ANSI_CSI_PATTERN = /\x1b\[[0-?]*[ -/]*[@-~]/g;
const ANSI_OSC_PATTERN = /\x1b\][^\x07]*(\x07|\x1b\\)/g;
const CONTROL_CHAR_PATTERN = /[\x00-\x08\x0b-\x1f\x7f]/g;
const UTF8_DECODERS_BY_SESSION = new Map<string, TextDecoder>();

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
    [sessionId]: toOutputEntries(sessionId, chunks, true)
  };
}

export function mergeBufferSnapshot(
  outputBySession: OutputBySession,
  sessionId: string,
  chunks: SessionOutputChunk[],
  incomingCursor?: string | null,
  requestCursor?: string | null
): OutputBySession {
  const existing = outputBySession[sessionId] ?? [];
  const incoming = toOutputEntries(sessionId, chunks, true);

  if (requestCursor && existing.length > 0) {
    const requestCursorIndex = existing.findIndex((entry) => entry.id === requestCursor);
    if (requestCursorIndex >= 0) {
      const knownIds = new Set(existing.map((entry) => entry.id));
      const tail = incoming.filter((entry) => !knownIds.has(entry.id));
      return {
        ...outputBySession,
        [sessionId]: [...existing, ...tail].slice(-MAX_CHUNK_COUNT_PER_SESSION)
      };
    }
  }

  if (incomingCursor && existing.length > 0 && existing[existing.length - 1]?.id === incomingCursor) {
    return outputBySession
  }

  let merged = incoming;
  if (incomingCursor && existing.length > 0) {
    const incomingCursorIndex = existing.findIndex((entry) => entry.id === incomingCursor);
    if (incomingCursorIndex >= 0 && incomingCursorIndex < existing.length - 1) {
      const knownIds = new Set(incoming.map((entry) => entry.id));
      const tail = existing
        .slice(incomingCursorIndex + 1)
        .filter((entry) => !knownIds.has(entry.id));
      merged = [...incoming, ...tail];
    }
  }

  if (merged.length > MAX_CHUNK_COUNT_PER_SESSION) {
    merged = merged.slice(merged.length - MAX_CHUNK_COUNT_PER_SESSION);
  }

  return {
    ...outputBySession,
    [sessionId]: merged
  };
}

export function appendChunk(
  outputBySession: OutputBySession,
  chunk: SessionOutputChunk
): OutputBySession {
  const existing = outputBySession[chunk.sessionId] ?? [];
  if (existing.some((entry) => entry.id === chunk.id)) {
    return outputBySession;
  }
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

export function renderOutputPreview(entries: OutputEntry[], maxChars = 160): string | null {
  if (entries.length === 0) {
    return null;
  }

  const recentOutput = entries
    .slice(-64)
    .map((entry) => entry.text)
    .join("");
  const sanitized = sanitizeOutputPreview(recentOutput);
  if (!sanitized) {
    return null;
  }

  if (sanitized.length <= maxChars) {
    return sanitized;
  }

  return `…${sanitized.slice(-maxChars)}`;
}

function sanitizeOutputPreview(text: string): string {
  return text
    .replace(ANSI_OSC_PATTERN, "")
    .replace(ANSI_CSI_PATTERN, "")
    .replace(CONTROL_CHAR_PATTERN, " ")
    .replace(/\s+/g, " ")
    .trim();
}

function toOutputEntries(
  sessionId: string,
  chunks: SessionOutputChunk[],
  resetDecoder: boolean
): OutputEntry[] {
  if (resetDecoder) {
    UTF8_DECODERS_BY_SESSION.delete(sessionId);
  }

  return chunks.map((chunk) => toOutputEntry(chunk));
}

function toOutputEntry(chunk: SessionOutputChunk): OutputEntry {
  return {
    id: chunk.id,
    sessionId: chunk.sessionId,
    stream: chunk.stream,
    timestamp: chunk.timestamp,
    text: decodeChunkText(chunk.sessionId, chunk.data)
  };
}

function decodeChunkText(sessionId: string, base64Data: string): string {
  try {
    const binary = atob(base64Data);
    const bytes = new Uint8Array(binary.length);
    for (let index = 0; index < binary.length; index += 1) {
      bytes[index] = binary.charCodeAt(index);
    }

    let decoder = UTF8_DECODERS_BY_SESSION.get(sessionId);
    if (!decoder) {
      decoder = new TextDecoder("utf-8");
      UTF8_DECODERS_BY_SESSION.set(sessionId, decoder);
    }

    return decoder.decode(bytes, {
      stream: true
    });
  } catch {
    return "[invalid output chunk]";
  }
}
