import {
  ensureApprovalConnectionConfig,
  storedApprovalPort,
  storedApprovalToken,
} from "./approval-client";

export interface PolicyValidationStatus {
  path: string;
  exists: boolean;
  ruleCount: number;
  ok: boolean;
  error?: string | null;
}

export interface AdapterRuntimeConfig {
  transport: string;
  acpCommand: string;
  acpArgs: string[];
}

export interface ConfigStatusResponse {
  adapters?: {
    claude?: AdapterRuntimeConfig;
    hermes?: AdapterRuntimeConfig;
  };
  policyValidation?: PolicyValidationStatus;
}

export async function fetchConfigStatus(): Promise<ConfigStatusResponse> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const response = await fetch(
    `http://127.0.0.1:${port ?? storedApprovalPort() ?? 17893}/v1/config/status`,
    { headers: authHeaders(token) },
  );
  if (!response.ok) {
    throw new Error(`config status failed: HTTP ${response.status}`);
  }
  return (await response.json()) as ConfigStatusResponse;
}

function authHeaders(token = storedApprovalToken()): Record<string, string> {
  return token ? { authorization: `Bearer ${token}` } : {};
}
