import { useState } from "react";
import {
  ACTIVE_CONNECTION_STORAGE_KEY,
  CONNECTIONS_STORAGE_KEY,
  PENDING_CACHE_PREFIX,
  STORAGE_KEY,
} from "./constants";
import type { Approval, Connection } from "./types";
import { storageAvailable } from "./utils";

export interface StoredConnectionsState {
  connections: Connection[];
  activeId: string | null;
  activeConnection: Connection | null;
  addConnection: (next: Connection) => void;
  replaceConnection: (oldId: string, next: Connection) => void;
  removeConnection: (id: string) => void;
  selectConnection: (id: string) => void;
  markNeedsRePair: (id: string, message: string) => void;
}

export function connectionId(machineId: string, deviceId: string): string {
  return `${machineId}:${deviceId}`;
}

export function normalizeConnection(connection: Connection): Connection {
  const machineId = connection.machineId || "unknown-machine";
  const deviceId = connection.deviceId || "unknown-device";
  return {
    ...connection,
    id: connection.id || connectionId(machineId, deviceId),
    machineId,
    deviceId,
    scope: connection.scope ?? "full",
    transports: connection.transports ?? [],
    needsRePair: connection.needsRePair ?? false,
  };
}

export function mergeConnection(
  connections: Connection[],
  next: Connection,
  replaceId?: string,
): Connection[] {
  const normalized = normalizeConnection(next);
  const withoutOld = connections.filter(
    (connection) => connection.id !== normalized.id && connection.id !== replaceId,
  );
  return [...withoutOld, normalized];
}

export function loadStoredConnections(): Connection[] {
  if (!storageAvailable()) {
    return [];
  }
  try {
    const raw = window.localStorage.getItem(CONNECTIONS_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as Connection[];
      return parsed.map(normalizeConnection);
    }
    const legacy = window.localStorage.getItem(STORAGE_KEY);
    return legacy ? [normalizeConnection(JSON.parse(legacy) as Connection)] : [];
  } catch {
    return [];
  }
}

export function loadStoredActiveConnectionId(): string | null {
  if (!storageAvailable()) {
    return null;
  }
  return window.localStorage.getItem(ACTIVE_CONNECTION_STORAGE_KEY);
}

export function pendingCacheKey(connection: Connection): string {
  return `${PENDING_CACHE_PREFIX}${connection.machineId}`;
}

export function loadCachedPending(connection: Connection): Approval[] {
  if (!storageAvailable()) {
    return [];
  }
  try {
    const raw = window.localStorage.getItem(pendingCacheKey(connection));
    return raw ? (JSON.parse(raw) as Approval[]) : [];
  } catch {
    return [];
  }
}

export function cachePending(connection: Connection, pending: Approval[]) {
  if (!storageAvailable()) {
    return;
  }
  window.localStorage.setItem(pendingCacheKey(connection), JSON.stringify(pending));
}

export function useStoredConnections(): StoredConnectionsState {
  const [connections, setConnectionsState] = useState<Connection[]>(() =>
    loadStoredConnections(),
  );
  const [activeId, setActiveIdState] = useState<string | null>(() =>
    loadStoredActiveConnectionId(),
  );
  const activeConnection =
    connections.find((connection) => connection.id === activeId) ?? connections[0] ?? null;

  const persist = (nextConnections: Connection[], nextActiveId: string | null) => {
    setConnectionsState(nextConnections);
    setActiveIdState(nextActiveId);
    if (!storageAvailable()) {
      return;
    }
    window.localStorage.setItem(CONNECTIONS_STORAGE_KEY, JSON.stringify(nextConnections));
    if (nextActiveId) {
      window.localStorage.setItem(ACTIVE_CONNECTION_STORAGE_KEY, nextActiveId);
    } else {
      window.localStorage.removeItem(ACTIVE_CONNECTION_STORAGE_KEY);
    }
    window.localStorage.removeItem(STORAGE_KEY);
  };

  const upsertConnection = (next: Connection, replaceId?: string) => {
    const normalized = normalizeConnection(next);
    const merged = mergeConnection(connections, normalized, replaceId);
    persist(merged, normalized.id);
  };

  return {
    connections,
    activeId: activeConnection?.id ?? null,
    activeConnection,
    addConnection: (next) => upsertConnection(next),
    replaceConnection: (oldId, next) => upsertConnection(next, oldId),
    removeConnection: (id) => {
      const next = connections.filter((connection) => connection.id !== id);
      const nextActive =
        activeConnection?.id === id ? next[0]?.id ?? null : activeConnection?.id ?? null;
      persist(next, nextActive);
    },
    selectConnection: (id) => {
      if (connections.some((connection) => connection.id === id)) {
        persist(connections, id);
      }
    },
    markNeedsRePair: (id, message) => {
      const next = connections.map((connection) =>
        connection.id === id
          ? { ...connection, token: "", needsRePair: true, authError: message }
          : connection,
      );
      persist(next, id);
    },
  };
}
