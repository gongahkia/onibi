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

export class EventsWS extends EventTarget {
  private url = "";
  private ws: WebSocket | undefined;
  private reconnectTimer = 0;
  private attempts = 0;
  private stopped = false;

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
    socket.addEventListener("message", (event) => this.handleMessage(event));
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

  private handleMessage(event: MessageEvent): void {
    if (typeof event.data !== "string") {
      return;
    }
    const envelope = JSON.parse(event.data) as EventEnvelope;
    this.dispatchEvent(new CustomEvent<EventEnvelope>("event", { detail: envelope }));
    this.dispatchEvent(new CustomEvent<EventEnvelope>(envelope.type, { detail: envelope }));
  }
}
