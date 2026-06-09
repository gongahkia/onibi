import {
  ensureApprovalConnectionConfig,
  storedApprovalPort,
  storedApprovalToken,
} from "./approval-client";
import type {
  CheckpointDiff,
  CheckpointPruneBody,
  CheckpointPruneResponse,
  CheckpointRecord,
  CheckpointRestoreBody,
} from "./contracts/generated";

const PROTOCOL_VERSION = "1.0";

export interface CheckpointListOptions {
  limit?: number;
}

export async function fetchCheckpoints({
  limit = 500,
}: CheckpointListOptions = {}): Promise<CheckpointRecord[]> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const params = new URLSearchParams({ limit: String(limit) });
  const response = await fetch(
    `http://127.0.0.1:${port ?? storedApprovalPort() ?? 17893}/v1/checkpoints/list?${params}`,
    { headers: authHeaders(token) },
  );
  if (!response.ok) {
    throw new Error(`checkpoint list failed: HTTP ${response.status}`);
  }
  return (await response.json()) as CheckpointRecord[];
}

export async function fetchCheckpointDiff(approvalId: string): Promise<CheckpointDiff> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const response = await fetch(
    `http://127.0.0.1:${port ?? storedApprovalPort() ?? 17893}/v1/checkpoints/${approvalId}/diff`,
    { headers: authHeaders(token) },
  );
  if (!response.ok) {
    throw new Error(`checkpoint diff failed: HTTP ${response.status}`);
  }
  return (await response.json()) as CheckpointDiff;
}

export async function restoreCheckpoint(approvalId: string): Promise<void> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const body: CheckpointRestoreBody = { protocol_version: PROTOCOL_VERSION };
  const response = await fetch(
    `http://127.0.0.1:${port ?? storedApprovalPort() ?? 17893}/v1/checkpoints/${approvalId}/restore`,
    {
      method: "POST",
      headers: { "content-type": "application/json", ...authHeaders(token) },
      body: JSON.stringify(body),
    },
  );
  if (!response.ok) {
    throw new Error(`checkpoint restore failed: HTTP ${response.status}`);
  }
}

export async function pruneCheckpoints(): Promise<CheckpointPruneResponse> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const body: CheckpointPruneBody = { protocol_version: PROTOCOL_VERSION };
  const response = await fetch(
    `http://127.0.0.1:${port ?? storedApprovalPort() ?? 17893}/v1/checkpoints/prune`,
    {
      method: "POST",
      headers: { "content-type": "application/json", ...authHeaders(token) },
      body: JSON.stringify(body),
    },
  );
  if (!response.ok) {
    throw new Error(`checkpoint prune failed: HTTP ${response.status}`);
  }
  return (await response.json()) as CheckpointPruneResponse;
}

function authHeaders(token = storedApprovalToken()): Record<string, string> {
  return token ? { authorization: `Bearer ${token}` } : {};
}
