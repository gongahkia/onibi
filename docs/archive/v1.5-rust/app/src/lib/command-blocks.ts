import {
  ensureApprovalConnectionConfig,
  type ApprovalClientOptions,
} from "./approval-client";
import type { CommandBlock } from "./sessions";
import type { DesktopCommandBlock } from "./contracts/generated";

const PROTOCOL_VERSION = "1.0";

function endpoint(path: string, config: ApprovalClientOptions): string {
  return `http://127.0.0.1:${config.port ?? 17893}${path}`;
}

function headers(config: ApprovalClientOptions): Record<string, string> {
  const result: Record<string, string> = { "content-type": "application/json" };
  if (config.token) {
    result.authorization = `Bearer ${config.token}`;
  }
  return result;
}

function commandBlockBody(block: CommandBlock): DesktopCommandBlock {
  return {
    protocol_version: PROTOCOL_VERSION,
    id: block.id,
    sessionId: block.sessionId,
    workspaceId: block.workspaceId,
    agent: block.agent,
    command: block.command,
    cwd: block.cwd,
    startedAt: block.startedAt,
    endedAt: block.endedAt ?? null,
    exitCode: block.exitCode ?? null,
    status: block.status,
    outputPreview: block.outputPreview,
    previewUrl: block.previewUrl ?? null,
    changedFiles: block.changedFiles,
    attention: block.attention ?? null,
    source: block.source,
  };
}

function normalizeCommandBlock(value: unknown): CommandBlock | null {
  if (typeof value !== "object" || value === null) {
    return null;
  }
  const item = value as Record<string, unknown>;
  if (
    typeof item.id !== "string" ||
    typeof item.sessionId !== "string" ||
    typeof item.workspaceId !== "string" ||
    typeof item.agent !== "string" ||
    typeof item.command !== "string" ||
    typeof item.cwd !== "string" ||
    typeof item.startedAt !== "number" ||
    typeof item.status !== "string"
  ) {
    return null;
  }
  return {
    id: item.id,
    sessionId: item.sessionId,
    workspaceId: item.workspaceId,
    agent: item.agent as CommandBlock["agent"],
    command: item.command,
    cwd: item.cwd,
    startedAt: item.startedAt,
    endedAt: typeof item.endedAt === "number" ? item.endedAt : null,
    exitCode: typeof item.exitCode === "number" ? item.exitCode : null,
    status: item.status as CommandBlock["status"],
    outputPreview:
      typeof item.outputPreview === "string" ? item.outputPreview : "",
    previewUrl: typeof item.previewUrl === "string" ? item.previewUrl : null,
    changedFiles: Array.isArray(item.changedFiles)
      ? item.changedFiles.filter(
          (path): path is string => typeof path === "string",
        )
      : [],
    attention:
      typeof item.attention === "string"
        ? (item.attention as CommandBlock["attention"])
        : null,
    source:
      item.source === "manual" || item.source === "trigger"
        ? item.source
        : "shell-integration",
  };
}

export async function persistCommandBlock(block: CommandBlock): Promise<void> {
  const config = await ensureApprovalConnectionConfig();
  const response = await fetch(endpoint("/v1/desktop/command-blocks", config), {
    method: "POST",
    headers: headers(config),
    body: JSON.stringify(commandBlockBody(block)),
  });
  if (!response.ok) {
    throw new Error(`command block persist failed: HTTP ${response.status}`);
  }
}

export async function listCommandBlocks(options: {
  sessionId?: string | null;
  limit?: number;
} = {}): Promise<CommandBlock[]> {
  const config = await ensureApprovalConnectionConfig();
  const params = new URLSearchParams();
  if (options.sessionId) {
    params.set("sessionId", options.sessionId);
  }
  if (options.limit) {
    params.set("limit", String(options.limit));
  }
  const suffix = params.toString() ? `?${params}` : "";
  const response = await fetch(endpoint(`/v1/desktop/command-blocks${suffix}`, config), {
    headers: headers(config),
  });
  if (!response.ok) {
    throw new Error(`command block load failed: HTTP ${response.status}`);
  }
  const raw = (await response.json()) as unknown;
  return Array.isArray(raw)
    ? raw.map(normalizeCommandBlock).filter((block): block is CommandBlock => block !== null)
    : [];
}
