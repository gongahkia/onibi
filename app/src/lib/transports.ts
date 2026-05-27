import { storedApprovalPort, storedApprovalToken } from "./approval-client";

export type TransportStatus =
  | { state: "stopped" }
  | { state: "starting" }
  | { state: "running"; url?: string | null; fingerprint?: string | null }
  | { state: "failed"; message: string };

export interface TransportSnapshot {
  name: string;
  label: string;
  requiresExternalDep?: string | null;
  enabled: boolean;
  status: TransportStatus;
  url?: string | null;
  fingerprint?: string | null;
}

function endpoint(path: string, port = storedApprovalPort() ?? 17893): string {
  return `http://127.0.0.1:${port}${path}`;
}

function authHeaders(): HeadersInit {
  const token = storedApprovalToken();
  return token ? { authorization: `Bearer ${token}` } : {};
}

async function parseJson<T>(response: Response, action: string): Promise<T> {
  if (!response.ok) {
    throw new Error(`${action} failed: HTTP ${response.status}`);
  }
  return (await response.json()) as T;
}

export async function fetchTransportStatus(): Promise<TransportSnapshot[]> {
  const response = await fetch(endpoint("/v1/transport/status"), {
    headers: authHeaders(),
  });
  return parseJson<TransportSnapshot[]>(response, "transport status");
}

export async function enableTransport(name: string): Promise<TransportSnapshot> {
  const response = await fetch(endpoint(`/v1/transport/${encodeURIComponent(name)}/enable`), {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...authHeaders(),
    },
    body: "{}",
  });
  return parseJson<TransportSnapshot>(response, `enable ${name}`);
}

export async function disableTransport(name: string): Promise<void> {
  const response = await fetch(endpoint(`/v1/transport/${encodeURIComponent(name)}/disable`), {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...authHeaders(),
    },
    body: "{}",
  });
  if (!response.ok) {
    throw new Error(`disable ${name} failed: HTTP ${response.status}`);
  }
}

export async function fetchLanCertQr(): Promise<string> {
  const response = await fetch(endpoint("/v1/transport/lan/cert-qr"), {
    headers: authHeaders(),
  });
  if (!response.ok) {
    throw new Error(`LAN certificate QR failed: HTTP ${response.status}`);
  }
  const blob = await response.blob();
  return URL.createObjectURL(blob);
}
