import {
  ensureApprovalConnectionConfig,
  storedApprovalPort,
  storedApprovalToken,
  type ApprovalDecision,
} from "./approval-client";
import type { Approval } from "./contracts/generated";

export type ApprovalAuditRecord = Approval;

export interface ApprovalHistoryOptions {
  agent?: string;
  decision?: ApprovalDecision | "all";
  from?: number;
  limit?: number;
  to?: number;
  tool?: string;
}

export async function fetchApprovalHistory({
  agent,
  decision = "all",
  from,
  limit = 200,
  to,
  tool,
}: ApprovalHistoryOptions = {}): Promise<ApprovalAuditRecord[]> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const params = new URLSearchParams({ limit: String(limit) });
  if (agent) {
    params.set("agent", agent);
  }
  if (decision !== "all") {
    params.set("decision", decision);
  }
  if (from !== undefined) {
    params.set("from", String(from));
  }
  if (to !== undefined) {
    params.set("to", String(to));
  }
  if (tool) {
    params.set("tool", tool);
  }
  const response = await fetch(
    `http://127.0.0.1:${port ?? storedApprovalPort() ?? 17893}/v1/approval/history?${params}`,
    { headers: authHeaders(token) },
  );
  if (!response.ok) {
    throw new Error(`approval history failed: HTTP ${response.status}`);
  }
  return (await response.json()) as ApprovalAuditRecord[];
}

function authHeaders(token = storedApprovalToken()): Record<string, string> {
  return token ? { authorization: `Bearer ${token}` } : {};
}
