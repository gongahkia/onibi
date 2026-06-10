import {
  ensureApprovalConnectionConfig,
  storedApprovalPort,
  storedApprovalToken,
} from "./approval-client";
import type { AcpPromptBody, AcpPromptResponse } from "./contracts/generated";
import type { ConfigStatusResponse, AdapterRuntimeConfig } from "./config-status";
import type { AgentKind } from "./sessions";

const PROTOCOL_VERSION = "1.0";

export type AcpAgentKind = Extract<AgentKind, "claude-code" | "hermes">;

export interface AcpPromptOptions {
  agent: AcpAgentKind;
  cwd: string;
  prompt: string;
  resumeSessionId?: string | null;
}

export function acpAdapterForAgent(
  agent: AgentKind,
  status?: ConfigStatusResponse | null,
): AdapterRuntimeConfig | null {
  if (agent === "claude-code") {
    return status?.adapters?.claude ?? null;
  }
  if (agent === "hermes") {
    return (
      status?.adapters?.hermes ?? {
        transport: "acp",
        acpCommand: "hermes",
        acpArgs: ["acp"],
      }
    );
  }
  return null;
}

export function isAcpTransportEnabled(
  agent: AgentKind,
  status?: ConfigStatusResponse | null,
): agent is AcpAgentKind {
  return acpAdapterForAgent(agent, status)?.transport === "acp";
}

export function acpCommandLine(adapter: AdapterRuntimeConfig): string {
  return [adapter.acpCommand, ...adapter.acpArgs].filter(Boolean).join(" ");
}

export async function promptAcpAgentSession({
  agent,
  cwd,
  prompt,
  resumeSessionId,
}: AcpPromptOptions): Promise<AcpPromptResponse> {
  const { token, port } = await ensureApprovalConnectionConfig();
  const body: AcpPromptBody = {
    protocol_version: PROTOCOL_VERSION,
    cwd,
    prompt,
    resumeSessionId: resumeSessionId ?? null,
  };
  const endpoint = `http://127.0.0.1:${port ?? storedApprovalPort() ?? 17893}/v1/adapters/${encodeURIComponent(agent)}/acp/prompt`;
  const response = await fetch(
    endpoint,
    {
      method: "POST",
      headers: { "content-type": "application/json", ...authHeaders(token) },
      body: JSON.stringify(body),
    },
  );
  if (!response.ok) {
    throw new Error(`${agent} ACP prompt failed: ${await responseError(response)}`);
  }
  return (await response.json()) as AcpPromptResponse;
}

function authHeaders(token = storedApprovalToken()): Record<string, string> {
  return token ? { authorization: `Bearer ${token}` } : {};
}

async function responseError(response: Response): Promise<string> {
  const text = await response.text().catch(() => "");
  if (text.trim()) {
    try {
      const payload = JSON.parse(text);
      if (payload && typeof payload.error === "string" && payload.error.trim()) {
        return payload.error;
      }
    } catch {
      return text.trim();
    }
    return text.trim();
  }
  return `HTTP ${response.status}`;
}
