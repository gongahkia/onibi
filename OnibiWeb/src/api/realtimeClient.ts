import type {
  ConnectionConfig,
  RealtimeClientMessage,
  RealtimeServerMessage
} from "../types";

export type RealtimeConnectionState =
  | "disconnected"
  | "connecting"
  | "authenticating"
  | "authenticated"
  | "reconnecting";

export interface RealtimeClientOptions {
  connection: ConnectionConfig;
  onStateChange: (state: RealtimeConnectionState) => void;
  onMessage: (message: RealtimeServerMessage) => void;
  onError: (message: string) => void;
}

const RECONNECT_BASE_DELAY_MS = 700;
const RECONNECT_MAX_DELAY_MS = 8000;

export class RealtimeClient {
  private readonly options: RealtimeClientOptions;
  private socket: WebSocket | null = null;
  private reconnectAttempt = 0;
  private shouldReconnect = true;
  private authenticated = false;
  private queuedMessages: RealtimeClientMessage[] = [];

  constructor(options: RealtimeClientOptions) {
    this.options = options;
  }

  connect(): void {
    this.shouldReconnect = true;
    this.openSocket();
  }

  disconnect(): void {
    this.shouldReconnect = false;
    this.authenticated = false;
    this.queuedMessages = [];
    this.options.onStateChange("disconnected");
    if (this.socket) {
      this.socket.close();
      this.socket = null;
    }
  }

  send(message: RealtimeClientMessage): void {
    if (this.socket?.readyState === WebSocket.OPEN && this.authenticated) {
      this.sendNow(message);
      return;
    }

    if (message.type !== "auth") {
      this.queuedMessages.push(message);
      if (this.queuedMessages.length > 512) {
        this.queuedMessages.shift();
      }
    }
  }

  private openSocket(): void {
    const url = this.websocketURL(this.options.connection.baseURL);
    this.options.onStateChange(this.reconnectAttempt > 0 ? "reconnecting" : "connecting");

    const socket = new WebSocket(url);
    this.socket = socket;
    this.authenticated = false;

    socket.onopen = () => {
      this.options.onStateChange("authenticating");
      this.sendNow({
        type: "auth",
        token: this.options.connection.token
      });
    };

    socket.onmessage = (event) => {
      let message: RealtimeServerMessage;
      try {
        message = JSON.parse(String(event.data)) as RealtimeServerMessage;
      } catch {
        this.options.onError("Received invalid realtime payload.");
        return;
      }

      if (message.type === "auth_ok") {
        this.authenticated = true;
        this.reconnectAttempt = 0;
        this.options.onStateChange("authenticated");
        this.flushQueuedMessages();
      } else if (message.type === "error") {
        const code = message.code ?? "error";
        const detail = message.message ?? "unknown";
        this.options.onError(`${code}: ${detail}`);
      }

      this.options.onMessage(message);
    };

    socket.onerror = () => {
      this.options.onError("Realtime websocket error.");
    };

    socket.onclose = () => {
      this.authenticated = false;
      this.socket = null;
      this.options.onStateChange("disconnected");
      if (this.shouldReconnect) {
        this.scheduleReconnect();
      }
    };
  }

  private scheduleReconnect(): void {
    this.reconnectAttempt += 1;
    const delay = Math.min(
      RECONNECT_BASE_DELAY_MS * Math.pow(2, this.reconnectAttempt - 1),
      RECONNECT_MAX_DELAY_MS
    );
    window.setTimeout(() => {
      if (!this.shouldReconnect) {
        return;
      }
      this.openSocket();
    }, delay);
  }

  private flushQueuedMessages(): void {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN || !this.authenticated) {
      return;
    }

    for (const message of this.queuedMessages) {
      this.sendNow(message);
    }
    this.queuedMessages = [];
  }

  private sendNow(message: RealtimeClientMessage): void {
    if (!this.socket || this.socket.readyState !== WebSocket.OPEN) {
      return;
    }
    this.socket.send(JSON.stringify(message));
  }

  private websocketURL(baseURL: string): string {
    const target = new URL("/api/v2/realtime", baseURL.endsWith("/") ? baseURL : `${baseURL}/`);
    target.protocol = target.protocol === "https:" ? "wss:" : "ws:";
    return target.toString();
  }
}
