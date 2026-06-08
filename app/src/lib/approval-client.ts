import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import type {
  Decision,
  ServerMessage,
} from "./contracts/generated";

export type ApprovalDecision = Decision;

export type ApprovalPendingMessage = Extract<
  ServerMessage,
  { type: "approval-pending" }
>;

export type ApprovalResolvedMessage = Extract<
  ServerMessage,
  { type: "approval-resolved" }
>;

export type DesktopCommandMessage = Extract<
  ServerMessage,
  { type: "desktop-command" }
>;

export type ApprovalRealtimeMessage = ServerMessage;

export interface ApprovalClientOptions {
  port?: number;
  token?: string;
}

export interface DecideApprovalOptions extends ApprovalClientOptions {
  approvalId: string;
  decision: ApprovalDecision;
  updatedInput?: unknown;
  reason?: string;
}

export function storedApprovalToken(): string | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }
  return window.localStorage.getItem("onibi.token") ?? undefined;
}

export function storedApprovalPort(): number | undefined {
  if (typeof window === "undefined") {
    return undefined;
  }
  const raw = window.localStorage.getItem("onibi.port");
  const port = raw ? Number.parseInt(raw, 10) : Number.NaN;
  return Number.isFinite(port) ? port : undefined;
}

export async function ensureApprovalConnectionConfig(): Promise<ApprovalClientOptions> {
  const existingToken = storedApprovalToken();
  const existingPort = storedApprovalPort();
  if (existingToken && existingPort) {
    return { token: existingToken, port: existingPort };
  }
  try {
    const config = await invoke<{ token: string; port: number }>(
      "approval_server_config",
    );
    if (config.token) {
      window.localStorage.setItem("onibi.token", config.token);
    }
    if (config.port) {
      window.localStorage.setItem("onibi.port", String(config.port));
    }
    return config;
  } catch {
    return { token: existingToken, port: existingPort };
  }
}

export async function decideApproval({
  port = 17893,
  token = storedApprovalToken(),
  approvalId,
  decision,
  updatedInput,
  reason,
}: DecideApprovalOptions): Promise<void> {
  const headers: Record<string, string> = {
    "content-type": "application/json",
  };
  if (token) {
    headers.authorization = `Bearer ${token}`;
  }
  const response = await fetch(`http://127.0.0.1:${port}/v1/approval/${approvalId}/decide`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      decision,
      updatedInput,
      reason,
    }),
  });
  if (!response.ok) {
    throw new Error(`approval decision failed: HTTP ${response.status}`);
  }
}

export async function subscribeApprovalEvents(
  options: ApprovalClientOptions,
  handler: (message: ApprovalRealtimeMessage) => void,
): Promise<() => void> {
  const disposers: Array<() => void> = [];
  let disposed = false;

  void listen<ApprovalRealtimeMessage>(
    "approval:realtime",
    (event) => handler(event.payload),
  )
    .then((unlisten: UnlistenFn) => {
      if (disposed) {
        unlisten();
      } else {
        disposers.push(unlisten);
      }
    })
    .catch(() => {
      // Outside Tauri, fall back to the local WebSocket path below.
    });

  const token = options.token ?? storedApprovalToken();
  if (
    token &&
    typeof WebSocket !== "undefined" &&
    typeof window !== "undefined" &&
    window.location.protocol !== "about:"
  ) {
    const port = options.port ?? storedApprovalPort() ?? 17893;
    const socket = new WebSocket(
      `ws://127.0.0.1:${port}/v1/realtime?token=${encodeURIComponent(token)}`,
    );
    socket.addEventListener("message", (event) => {
      try {
        const message = JSON.parse(event.data) as ApprovalRealtimeMessage;
        if (message.type === "ping" && socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "pong" }));
        }
        handler(message);
      } catch {
        // Ignore malformed daemon messages. The Rust side keeps the source of truth.
      }
    });
    disposers.push(() => socket.close());
  }

  return () => {
    disposed = true;
    for (const dispose of disposers) {
      dispose();
    }
  };
}
