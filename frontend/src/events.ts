import { decodeText, RelayE2E } from "./e2e";

export type EventEnvelope<T = unknown> = {
  type: string;
  ts: string;
  payload: T;
};

export type ApprovalRequestedPayload = {
  id: string;
  session_id: string;
  agent: string;
  tool: string;
  scrubbed_input: string;
  budget_warning?: BudgetWarningPayload;
  file_path?: string;
  unified_diff?: string;
  risk_level: "low" | "medium" | "high";
  risk_reasons?: string[];
  expires_at: string;
};

export type BudgetWarningPayload = {
  scope: string;
  current_tokens: number;
  predicted_tokens: number;
  projected_tokens: number;
  limit_tokens: number;
  remaining_tokens: number;
  on_overrun: string;
  message: string;
};

export type ApprovalDecidedPayload = {
  id: string;
  session_id: string;
  verdict: string;
  reason?: string;
  decided_at?: number;
  expires_at?: string;
};

export type ToastPayload = {
  message: string;
  level?: string;
};

export class EventsWS extends EventTarget {
  private url = "";
  private ws: WebSocket | undefined;
  private reconnectTimer = 0;
  private attempts = 0;
  private stopped = false;
  private e2e: RelayE2E | undefined;

  setE2E(e2e: RelayE2E | undefined): void {
    this.e2e = e2e;
  }

  connect(url: string): void {
    this.url = url;
    this.stopped = false;
    this.open();
  }

  close(): void {
    this.stopped = true;
    window.clearTimeout(this.reconnectTimer);
    this.ws?.close();
  }

  private open(): void {
    const socket = new WebSocket(this.url, ["onibi.events.v1"]);
    this.ws = socket;
    socket.addEventListener("open", () => {
      this.attempts = 0;
      this.dispatchEvent(new Event("open"));
    });
    socket.addEventListener("message", (event) => void this.handleMessage(event));
    socket.addEventListener("close", () => {
      this.dispatchEvent(new Event("close"));
      if (!this.stopped) {
        this.scheduleReconnect();
      }
    });
    socket.addEventListener("error", () => socket.close());
  }

  private scheduleReconnect(): void {
    window.clearTimeout(this.reconnectTimer);
    const delay = Math.min(8000, 250 * 2 ** this.attempts);
    this.attempts += 1;
    this.reconnectTimer = window.setTimeout(() => this.open(), delay);
  }

  private async handleMessage(event: MessageEvent): Promise<void> {
    if (typeof event.data !== "string") {
      return;
    }
    const raw = this.e2e === undefined ? event.data : decodeText((await this.e2e.open(event.data, "ws:events")).data);
    const envelope = JSON.parse(raw) as EventEnvelope;
    this.dispatchEvent(new CustomEvent<EventEnvelope>("event", { detail: envelope }));
    this.dispatchEvent(new CustomEvent<EventEnvelope>(envelope.type, { detail: envelope }));
  }
}
