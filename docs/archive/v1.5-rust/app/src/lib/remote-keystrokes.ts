import {
  ensureApprovalConnectionConfig,
  storedApprovalPort,
  storedApprovalToken,
} from "./approval-client";
import type {
  PaneSendKeysBody,
  PaneRunBody,
  PaneSendResponse,
  PaneSendTextBody,
  PaneTarget,
  PaneTargetsResponse,
} from "./contracts/generated";

const PROTOCOL_VERSION = "1.0";

export type RemotePaneTarget = PaneTarget;

export class RemotePaneApiError extends Error {
  readonly status: number;
  readonly details: unknown;

  constructor(message: string, status: number, details?: unknown) {
    super(message);
    this.name = "RemotePaneApiError";
    this.status = status;
    this.details = details;
  }
}

export async function listPaneTargets(): Promise<RemotePaneTarget[]> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const response = await fetch(remoteEndpoint("/v1/panes/targets", port), {
    headers: authHeaders(token),
  });
  if (!response.ok) {
    throw await apiError(response, "pane targets failed");
  }
  const payload = (await response.json()) as PaneTargetsResponse;
  return payload.targets;
}

export async function sendPaneText(
  paneId: string,
  text: string,
  sendEnter: boolean,
  confirmed: boolean,
): Promise<PaneSendResponse> {
  const body: PaneSendTextBody = {
    protocol_version: PROTOCOL_VERSION,
    text,
    sendEnter,
    confirmed,
  };
  return sendPaneRequest(`/v1/panes/${encodeURIComponent(paneId)}/send-text`, body);
}

export async function sendPanePreset(
  paneId: string,
  preset: string,
  confirmed: boolean,
): Promise<PaneSendResponse> {
  const body: PaneSendKeysBody = {
    protocol_version: PROTOCOL_VERSION,
    preset,
    confirmed,
  };
  return sendPaneRequest(`/v1/panes/${encodeURIComponent(paneId)}/send-keys`, body);
}

export async function runPaneCommand(
  paneId: string,
  command: string,
  confirmed: boolean,
): Promise<PaneSendResponse> {
  const body: PaneRunBody = {
    protocol_version: PROTOCOL_VERSION,
    command,
    confirmed,
  };
  return sendPaneRequest(`/v1/panes/${encodeURIComponent(paneId)}/run`, body);
}

async function sendPaneRequest(
  path: string,
  body: PaneSendTextBody | PaneSendKeysBody | PaneRunBody,
): Promise<PaneSendResponse> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const response = await fetch(remoteEndpoint(path, port), {
    method: "POST",
    headers: { "content-type": "application/json", ...authHeaders(token) },
    body: JSON.stringify(body),
  });
  if (!response.ok) {
    throw await apiError(response, "remote pane input failed");
  }
  return (await response.json()) as PaneSendResponse;
}

function remoteEndpoint(path: string, port?: number): string {
  return `http://127.0.0.1:${port ?? storedApprovalPort() ?? 17893}${path}`;
}

function authHeaders(token = storedApprovalToken()): Record<string, string> {
  return token ? { authorization: `Bearer ${token}` } : {};
}

async function apiError(response: Response, fallback: string): Promise<RemotePaneApiError> {
  const text = await response.text().catch(() => "");
  const payload = parseJsonObject(text);
  const message =
    typeof payload?.error === "string" && payload.error.trim()
      ? `${fallback}: ${payload.error}`
      : text.trim()
        ? `${fallback}: ${text.trim()}`
        : `${fallback}: HTTP ${response.status}`;
  return new RemotePaneApiError(message, response.status, payload?.details);
}

function parseJsonObject(text: string): Record<string, unknown> | null {
  if (!text.trim()) {
    return null;
  }
  try {
    const value = JSON.parse(text);
    return value && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}
