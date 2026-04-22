export type SessionTransportStatus = "starting" | "running" | "exited" | "failed";
export type SessionOutputStream = "stdout" | "stderr";
export type RemoteInputKind = "text" | "key";
export type RemoteInputKey =
  | "enter"
  | "ctrl_c"
  | "ctrl_d"
  | "ctrl_s"
  | "ctrl_q"
  | "tab"
  | "backspace"
  | "escape"
  | "delete"
  | "home"
  | "end"
  | "page_up"
  | "page_down"
  | "arrow_up"
  | "arrow_down"
  | "arrow_left"
  | "arrow_right"
  | "space";

export interface ControllableSessionSnapshot {
  id: string;
  displayName: string;
  startedAt: string;
  lastActivityAt: string;
  status: SessionTransportStatus;
  isControllable: boolean;
  workingDirectory: string | null;
  lastCommandPreview: string | null;
  bufferCursor: string | null;
  shell?: string | null;
  pid?: number | null;
  hostname?: string | null;
  proxyVersion?: string | null;
  terminalCols?: number | null;
  terminalRows?: number | null;
  terminalTitle?: string | null;
}

export interface SessionOutputChunk {
  id: string;
  sessionId: string;
  stream: SessionOutputStream;
  timestamp: string;
  data: string;
}

export interface SessionOutputBufferSnapshot {
  session: ControllableSessionSnapshot;
  bufferCursor: string | null;
  chunks: SessionOutputChunk[];
  truncated: boolean;
}

export interface RemoteInputPayload {
  kind: RemoteInputKind;
  text?: string;
  key?: RemoteInputKey;
}

export interface RemoteInputAcceptance {
  sessionId: string;
  acceptedAt: string;
}

export interface DiagnosticsEventPreview {
  timestamp: string;
  component: string;
  severity: "debug" | "info" | "warning" | "error" | "critical";
  message: string;
}

export interface DiagnosticsResponse {
  generatedAt: string;
  hostVersion: string;
  diagnosticsEventCount: number;
  warningCount: number;
  errorCount: number;
  criticalCount: number;
  schedulerEventsProcessed: number;
  realtimeProtocolVersion?: number;
  minimumSupportedRealtimeProtocolVersion?: number;
  storageLogCount: number;
  storageBytes: number;
  tailscaleStatus: string;
  latestErrorTitle?: string;
  latestErrorTimestamp?: string;
  recentEvents: DiagnosticsEventPreview[];
  controllableSessionCount?: number;
  connectedRealtimeClientCount?: number;
  websocketAuthFailureCount?: number;
  proxyRegistrationFailureCount?: number;
  proxyDisconnectCount?: number;
  staleSessionCount?: number;
  localProxySocketHealthy?: boolean;
  bufferTruncationCount?: number;
  lastInputRoutingError?: string;
  tokenIssuedAt?: string;
  latestProxyVersion?: string;
  proxyVersionMismatchCount?: number;
}

export interface HostHealth {
  ghosttyRunning: boolean;
  schedulerRunning: boolean;
  lastIngestAt?: string;
  activeSessionCount: number;
  gatewayRunning: boolean;
}

export interface FeatureFlagsResponse {
  legacyMonitoringEnabled: boolean;
  remoteControlEnabled: boolean;
  realtimeSessionsEnabled: boolean;
  websocketEnabled: boolean;
  fallbackInputEnabled: boolean;
}

export interface GatewayBootstrapResponse {
  health: HostHealth;
  featureFlags: FeatureFlagsResponse;
  sessions: ControllableSessionSnapshot[];
  diagnostics: DiagnosticsResponse;
}

export type RealtimeClientMessageType =
  | "auth"
  | "subscribe"
  | "unsubscribe"
  | "request_buffer"
  | "send_input"
  | "resize";

export interface RealtimeClientMessage {
  type: RealtimeClientMessageType;
  token?: string;
  sessionId?: string;
  kind?: RemoteInputKind;
  text?: string;
  key?: RemoteInputKey;
  cols?: number;
  rows?: number;
  clientRequestId?: string;
}

export type RealtimeServerMessageType =
  | "auth_ok"
  | "sessions_snapshot"
  | "session_added"
  | "session_updated"
  | "session_removed"
  | "buffer_snapshot"
  | "output"
  | "input_accepted"
  | "error";

export interface RealtimeServerMessage {
  type: RealtimeServerMessageType;
  hostVersion?: string;
  realtimeProtocolVersion?: number;
  minimumSupportedRealtimeProtocolVersion?: number;
  sessions?: ControllableSessionSnapshot[];
  session?: ControllableSessionSnapshot;
  sessionId?: string;
  chunks?: SessionOutputChunk[];
  bufferCursor?: string;
  truncated?: boolean;
  chunk?: SessionOutputChunk;
  clientRequestId?: string;
  code?: string;
  message?: string;
}

export interface ConnectionConfig {
  baseURL: string;
  token: string;
}

declare global {
  // Injected by vite.config.ts define: compile-time constant for the web build version.
  const __ONIBI_WEB_VERSION__: string;
}

export const ONIBI_WEB_VERSION: string =
  typeof __ONIBI_WEB_VERSION__ !== "undefined" ? __ONIBI_WEB_VERSION__ : "dev";
