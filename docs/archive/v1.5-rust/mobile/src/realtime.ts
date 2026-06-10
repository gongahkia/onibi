import type { Dispatch, SetStateAction } from "react";
import type { Approval, Connection, RunEvent, ServerMessage, WsState } from "./types";
import { normalizeBaseUrl } from "./utils";

export function realtimeUrl(connection: Connection): string {
  const base = new URL(normalizeBaseUrl(connection.baseUrl));
  base.protocol = base.protocol === "https:" ? "wss:" : "ws:";
  base.pathname = "/v1/realtime";
  base.search = `token=${encodeURIComponent(connection.token)}`;
  return base.toString();
}

export function reconnectDelay(attempt: number): number {
  return Math.min(30_000, 1_000 * 2 ** Math.max(0, attempt));
}

export function connectionStateMessage(state: WsState, transport: string): string {
  switch (state) {
    case "connecting":
      return `Connecting through ${transport}...`;
    case "fallback":
      return `Trying fallback transport after ${transport} failed...`;
    case "closed":
      return `Offline on ${transport}. Cached approvals stay visible.`;
    case "idle":
      return `Waiting for realtime connection on ${transport}.`;
    default:
      return "";
  }
}

export function applyServerMessage(
  message: ServerMessage,
  setPending: Dispatch<SetStateAction<Approval[]>>,
  setRecent: Dispatch<SetStateAction<RunEvent[]>>,
  setTerminalOutput: Dispatch<SetStateAction<Record<string, string[]>>>,
  setSelectedSession: Dispatch<SetStateAction<string>>,
) {
  if (message.type === "approval-pending" && message.approval_id && message.session_id) {
    const approval: Approval = {
      approval_id: message.approval_id,
      machine_id: message.machine_id ?? "",
      session_id: message.session_id,
      agent: message.agent ?? "agent",
      tool: message.tool ?? "tool",
      input: message.input ?? {},
      cwd: message.cwd ?? "",
      metadata: message.metadata,
      created_at: typeof message.created_at === "number" ? message.created_at : Date.now(),
    };
    setPending((items) => [
      approval,
      ...items.filter((item) => item.approval_id !== approval.approval_id),
    ]);
  }
  if (message.type === "approval-resolved" && message.approval_id) {
    setPending((items) => items.filter((item) => item.approval_id !== message.approval_id));
  }
  if (message.type === "run-event" && message.session_id && message.kind) {
    setRecent((items) =>
      [
        {
          session_id: message.session_id ?? "",
          kind: message.kind ?? "event",
          payload: message.payload ?? {},
          ts: Date.now(),
        },
        ...items,
      ].slice(0, 50),
    );
  }
  if (message.type === "pty-output" && message.session_id && message.data) {
    setSelectedSession((current) => current || message.session_id || "");
    setTerminalOutput((items) => ({
      ...items,
      [message.session_id ?? ""]: [
        ...(items[message.session_id ?? ""] ?? []),
        message.data ?? "",
      ],
    }));
  }
}
