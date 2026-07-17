import { decodeText, RelayE2E } from "./e2e";

const eventRealm = typeof window === "undefined" ? globalThis : window;
const WebEvent = eventRealm.Event;
const WebCustomEvent = eventRealm.CustomEvent;
const WebEventTarget = eventRealm.EventTarget;

export type EventEnvelope<T = unknown> = {
  seq?: number;
  type: string;
  ts: string;
  payload: T;
};

type EventRecoveryPayload = {
  mode: "replay" | "snapshot";
  seq: number;
  replay_count: number;
};

export type ApprovalRequestedPayload = {
  id: string;
  session_id: string;
  agent: string;
  tool: string;
  scrubbed_input: string;
  file_path?: string;
  unified_diff?: string;
  risk_level: "low" | "medium" | "high";
  risk_reasons?: string[];
  expires_at: string;
};

export type ApprovalDecidedPayload = {
  id: string;
  session_id: string;
  verdict: string;
  reason?: string;
  decided_at?: number;
  expires_at?: string;
};

export type AnomalyRequestedPayload = {
  approval_id: string;
  session_id: string;
  agent?: string;
  rule_name: string;
  evidence: string;
  paused: boolean;
};

export type ToastPayload = {
  message: string;
  level?: string;
};

export class EventsWS extends WebEventTarget {
  private url = "";
  private ws: WebSocket | undefined;
  private reconnectTimer = 0;
  private attempts = 0;
  private stopped = false;
  private lastSeq = 0;
  private e2e: RelayE2E | undefined;

  setE2E(e2e: RelayE2E | undefined): void {
    this.e2e = e2e;
  }

  connect(url: string, lastSeq = 0): void {
    this.ws?.close();
    this.url = url;
    this.lastSeq = lastSeq;
    this.stopped = false;
    this.open();
  }

  close(): void {
    this.stopped = true;
    window.clearTimeout(this.reconnectTimer);
    const socket = this.ws;
    this.ws = undefined;
    socket?.close();
  }

  resume(): void {
    if (this.stopped || this.url === "") {
      return;
    }
    const state = this.ws?.readyState;
    if (state === WebSocket.OPEN || state === WebSocket.CONNECTING) {
      return;
    }
    window.clearTimeout(this.reconnectTimer);
    this.open();
  }

  private open(): void {
    if (
      this.stopped ||
      this.ws?.readyState === WebSocket.OPEN ||
      this.ws?.readyState === WebSocket.CONNECTING
    ) {
      return;
    }
    const socket = new WebSocket(this.url, ["onibi.events.v1"]);
    let messages = Promise.resolve();
    this.ws = socket;
    socket.addEventListener("open", () => void this.handleOpen(socket));
    socket.addEventListener("message", (event) => {
      messages = messages.then(() => this.handleMessage(socket, event)).catch(() => socket.close());
    });
    socket.addEventListener("close", () => {
      if (this.ws !== socket) {
        return;
      }
      this.ws = undefined;
      this.dispatchEvent(new WebEvent("close"));
      if (!this.stopped) {
        this.scheduleReconnect();
      }
    });
    socket.addEventListener("error", () => socket.close());
  }

  private async handleOpen(socket: WebSocket): Promise<void> {
    if (this.ws !== socket || this.stopped) {
      socket.close();
      return;
    }
    this.attempts = 0;
    const attach: { type: "attach"; last_seq: number; verify_token?: string } = {
      type: "attach",
      last_seq: this.lastSeq
    };
    if (this.e2e !== undefined) {
      this.e2e.startStream("ws:events");
      attach.verify_token = this.e2e.verifyToken();
      socket.send(await this.e2e.sealText(JSON.stringify(attach), "ws:events"));
    } else {
      socket.send(JSON.stringify(attach));
    }
    if (this.ws !== socket || this.stopped) {
      return;
    }
    this.dispatchEvent(new WebEvent("open"));
  }

  private scheduleReconnect(): void {
    window.clearTimeout(this.reconnectTimer);
    const delay = Math.min(8000, 250 * 2 ** this.attempts);
    this.attempts += 1;
    this.reconnectTimer = window.setTimeout(() => this.open(), delay);
  }

  private async handleMessage(socket: WebSocket, event: MessageEvent): Promise<void> {
    if (this.ws !== socket || this.stopped) {
      return;
    }
    if (typeof event.data !== "string") {
      this.reject(socket, "event frame must be text");
      return;
    }
    const raw =
      this.e2e === undefined
        ? event.data
        : decodeText((await this.e2e.open(event.data, "ws:events")).data);
    if (this.ws !== socket || this.stopped) {
      return;
    }
    const envelope = JSON.parse(raw) as EventEnvelope;
    if (envelope.type === "events.recovery") {
      const recovery = envelope.payload as EventRecoveryPayload;
      if (
        (recovery.mode !== "replay" && recovery.mode !== "snapshot") ||
        !Number.isSafeInteger(recovery.seq) ||
        recovery.seq < 0 ||
        (recovery.mode === "replay" && recovery.seq !== this.lastSeq)
      ) {
        this.reject(socket, "invalid event recovery checkpoint");
        return;
      }
      this.lastSeq = recovery.seq;
      this.dispatchEvent(
        new WebCustomEvent<EventRecoveryPayload>("recovered", { detail: recovery })
      );
      return;
    }
    if (!Number.isSafeInteger(envelope.seq) || envelope.seq < 0) {
      this.reject(socket, "invalid event sequence");
      return;
    }
    if (envelope.seq > 0) {
      if (envelope.seq !== this.lastSeq + 1) {
        this.reject(socket, "event sequence gap or replay");
        return;
      }
      this.lastSeq = envelope.seq;
    }
    this.dispatchEvent(new WebCustomEvent<EventEnvelope>("event", { detail: envelope }));
    this.dispatchEvent(new WebCustomEvent<EventEnvelope>(envelope.type, { detail: envelope }));
  }

  private reject(socket: WebSocket, reason: string): void {
    this.dispatchEvent(new WebCustomEvent<string>("protocolerror", { detail: reason }));
    if (this.ws === socket) {
      socket.close();
    }
  }
}
