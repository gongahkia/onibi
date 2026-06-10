export type Decision = "allow" | "deny";
export type ClientScope = "full" | "read-only";
export type TrustMode = "approval-required" | "full-access";
export type WsState = "idle" | "connecting" | "fallback" | "open" | "closed";
export type Tab = "pending" | "recent" | "terminal" | "send";

export type BeforeInstallPromptEvent = Event & {
  prompt(): Promise<void>;
  userChoice: Promise<{ outcome: "accepted" | "dismissed"; platform: string }>;
};

export interface TransportEndpoint {
  name: string;
  url: string;
  fingerprint?: string | null;
}

export interface PairingPayload {
  protocol_version?: string;
  machine_id?: string;
  machineId?: string;
  host?: string;
  port?: number;
  token: string;
  scope?: ClientScope;
  vapid_public_key?: string;
  vapidPublicKey?: string;
  transports?: TransportEndpoint[];
}

export interface PairResponse {
  deviceId: string;
  machineId: string;
  scope?: ClientScope;
}

export interface Connection {
  id: string;
  baseUrl: string;
  token: string;
  deviceId: string;
  machineId: string;
  scope: ClientScope;
  vapidPublicKey?: string;
  transports: TransportEndpoint[];
  needsRePair?: boolean;
  authError?: string;
}

export interface Approval {
  approval_id: string;
  machine_id: string;
  session_id: string;
  agent: string;
  tool: string;
  input: unknown;
  cwd: string;
  metadata?: unknown;
  created_at: number;
}

export interface RunEvent {
  id?: number;
  session_id: string;
  kind: string;
  payload: unknown;
  ts: number;
}

export interface PaneTarget {
  paneId: string;
  sessionId: string;
  label: string;
  agent?: string | null;
  workspaceId?: string | null;
  cwd?: string | null;
  status: string;
  trustMode: TrustMode;
}

export interface PaneTargetsResponse {
  targets: PaneTarget[];
}

export interface PaneSendResponse {
  ok: boolean;
  paneId: string;
  sessionId: string;
  bytes: number;
  auditId: string;
  trustMode: TrustMode;
  destructive: boolean;
  preset?: string | null;
}

export interface RemotePreset {
  key: string;
  label: string;
  destructive: boolean;
}

export interface PendingRemoteDispatch {
  kind: "text" | "preset";
  preset?: RemotePreset;
  destructive: boolean;
  message: string;
}

export interface ServerMessage {
  type: string;
  approval_id?: string;
  machine_id?: string;
  session_id?: string;
  agent?: string;
  tool?: string;
  input?: unknown;
  cwd?: string;
  metadata?: unknown;
  decision?: Decision;
  kind?: string;
  payload?: unknown;
  data?: string;
  created_at?: number;
}

export type BarcodeDetectorCtor = new (options: { formats: string[] }) => {
  detect(image: ImageBitmapSource): Promise<Array<{ rawValue: string }>>;
};
