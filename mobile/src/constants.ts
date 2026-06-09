import type { RemotePreset } from "./types";

export const STORAGE_KEY = "onibi.mobile.connection";
export const CONNECTIONS_STORAGE_KEY = "onibi.mobile.connections.v1";
export const ACTIVE_CONNECTION_STORAGE_KEY = "onibi.mobile.activeConnectionId";
export const PENDING_CACHE_PREFIX = "onibi.mobile.pending.";
export const DEVICE_LABEL = "Onibi Mobile PWA";

export const REMOTE_PRESETS: RemotePreset[] = [
  { key: "continue", label: "Continue", destructive: false },
  { key: "approve", label: "Approve", destructive: false },
  { key: "interrupt", label: "Interrupt", destructive: true },
];
