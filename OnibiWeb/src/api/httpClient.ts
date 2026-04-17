import type {
  ConnectionConfig,
  GatewayBootstrapResponse,
  RemoteInputAcceptance,
  RemoteInputPayload,
  SessionOutputBufferSnapshot,
  ControllableSessionSnapshot
} from "../types";

export class APIError extends Error {
  readonly statusCode?: number;
  readonly code?: string;

  constructor(message: string, statusCode?: number, code?: string) {
    super(message);
    this.name = "APIError";
    this.statusCode = statusCode;
    this.code = code;
  }
}

function normalizeBaseURL(baseURL: string): string {
  return baseURL.trim().replace(/\/+$/, "");
}

function buildURL(baseURL: string, path: string): URL {
  const normalized = normalizeBaseURL(baseURL);
  return new URL(path, `${normalized}/`);
}

async function requestJSON<T>(
  connection: ConnectionConfig,
  path: string,
  init?: RequestInit
): Promise<T> {
  const url = buildURL(connection.baseURL, path);
  const response = await fetch(url, {
    ...init,
    headers: {
      Authorization: `Bearer ${connection.token}`,
      "Content-Type": "application/json",
      ...(init?.headers ?? {})
    }
  });

  const text = await response.text();
  let parsed: unknown;
  try {
    parsed = text.length > 0 ? JSON.parse(text) : null;
  } catch {
    parsed = null;
  }

  if (!response.ok) {
    const code =
      typeof parsed === "object" &&
      parsed !== null &&
      "error" in parsed &&
      typeof (parsed as Record<string, unknown>).error === "string"
        ? ((parsed as Record<string, unknown>).error as string)
        : undefined;
    throw new APIError(`Request failed (${response.status})`, response.status, code);
  }

  return parsed as T;
}

export async function fetchBootstrap(
  connection: ConnectionConfig
): Promise<GatewayBootstrapResponse> {
  return requestJSON(connection, "/api/v2/bootstrap");
}

export async function fetchSessions(
  connection: ConnectionConfig
): Promise<ControllableSessionSnapshot[]> {
  return requestJSON(connection, "/api/v2/sessions");
}

export async function fetchSessionBuffer(
  connection: ConnectionConfig,
  sessionId: string
): Promise<SessionOutputBufferSnapshot> {
  return requestJSON(connection, `/api/v2/sessions/${sessionId}/buffer`);
}

export async function sendSessionInput(
  connection: ConnectionConfig,
  sessionId: string,
  payload: RemoteInputPayload
): Promise<RemoteInputAcceptance> {
  return requestJSON(connection, `/api/v2/sessions/${sessionId}/input`, {
    method: "POST",
    body: JSON.stringify(payload)
  });
}

export function toUserFacingConnectionError(error: unknown): string {
  if (error instanceof APIError) {
    if (error.statusCode === 401 || error.code === "unauthorized") {
      return "Pairing token rejected by host.";
    }
    if (error.code === "realtime_disabled") {
      return "Remote control is disabled on the host.";
    }
    return `Host request failed (${error.statusCode ?? "unknown"}).`;
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Unable to connect to host.";
}

export function normalizeConnectionConfig(connection: ConnectionConfig): ConnectionConfig {
  return {
    baseURL: normalizeBaseURL(connection.baseURL),
    token: connection.token.replace(/\s+/g, "")
  };
}
