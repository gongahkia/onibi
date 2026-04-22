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

export interface RealtimeDebugEvent {
  timestamp: number;
  level: "debug" | "info" | "warn" | "error";
  message: string;
}

export interface RealtimeClientOptions {
  connection: ConnectionConfig;
  onStateChange: (state: RealtimeConnectionState) => void;
  onMessage: (message: RealtimeServerMessage) => void;
  onError: (message: string) => void;
  onDebugEvent?: (event: RealtimeDebugEvent) => void;
}

const RECONNECT_BASE_DELAY_MS = 700;
const RECONNECT_MAX_DELAY_MS = 8000;
const WEB_REALTIME_PROTOCOL_VERSION = 1;

export class RealtimeClient {
  private readonly options: RealtimeClientOptions;
  private socket: WebSocket | null = null;
  private reconnectAttempt = 0;
  private shouldReconnect = true;
  private authenticated = false;
  private queuedMessages: RealtimeClientMessage[] = [];
  private lastCloseCode: number | null = null;
  private lastCloseReason: string | null = null;

  constructor(options: RealtimeClientOptions) {
    this.options = options;
  }

  get reconnectAttempts(): number {
    return this.reconnectAttempt;
  }

  get lastClose(): { code: number; reason: string } | null {
    if (this.lastCloseCode === null) return null;
    return { code: this.lastCloseCode, reason: this.lastCloseReason ?? "" };
  }

  private emitDebug(level: RealtimeDebugEvent["level"], message: string): void {
    this.options.onDebugEvent?.({ timestamp: Date.now(), level, message });
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

    if (message.type === "auth" || message.type === "unsubscribe") {
      return;
    }

    if (message.type === "subscribe" || message.type === "request_buffer") {
      this.queuedMessages = this.queuedMessages.filter((queuedMessage) => {
        return !(queuedMessage.type === message.type && queuedMessage.sessionId === message.sessionId);
      });
    }

    if (message.type === "resize") {
      this.queuedMessages = this.queuedMessages.filter((queuedMessage) => {
        return !(queuedMessage.type === "resize" && queuedMessage.sessionId === message.sessionId);
      });
    }

    this.queuedMessages.push(message);
    if (this.queuedMessages.length > 512) {
      this.queuedMessages.shift();
    }
  }

  private openSocket(): void {
    const url = this.websocketURL(this.options.connection.baseURL);
    this.options.onStateChange(this.reconnectAttempt > 0 ? "reconnecting" : "connecting");
    this.emitDebug("info", `opening ${url} (attempt ${this.reconnectAttempt + 1})`);

    const socket = new WebSocket(url);
    this.socket = socket;
    this.authenticated = false;

    socket.onopen = () => {
      this.options.onStateChange("authenticating");
      this.emitDebug("info", "socket open, sending auth");
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
        this.emitDebug("error", "invalid JSON from server");
        this.options.onError("Received invalid realtime payload.");
        return;
      }

      if (message.type === "auth_ok") {
        const serverProtocolVersion = message.realtimeProtocolVersion;
        const minSupportedProtocolVersion = message.minimumSupportedRealtimeProtocolVersion;
        if (
          typeof minSupportedProtocolVersion === "number" &&
          WEB_REALTIME_PROTOCOL_VERSION < minSupportedProtocolVersion
        ) {
          const compatibilityError = `Realtime protocol mismatch: web=${WEB_REALTIME_PROTOCOL_VERSION} min_supported=${minSupportedProtocolVersion}`;
          this.emitDebug("error", compatibilityError);
          this.options.onError(compatibilityError);
          this.disconnect();
          return;
        } else if (
          typeof serverProtocolVersion === "number" &&
          serverProtocolVersion !== WEB_REALTIME_PROTOCOL_VERSION
        ) {
          this.emitDebug(
            "warn",
            `Realtime protocol differs: web=${WEB_REALTIME_PROTOCOL_VERSION} host=${serverProtocolVersion}`
          );
        }

        this.authenticated = true;
        this.reconnectAttempt = 0;
        this.options.onStateChange("authenticated");
        this.emitDebug("info", `authenticated (host ${message.hostVersion ?? "?"})`);
        this.flushQueuedMessages();
      } else if (message.type === "error") {
        const code = message.code ?? "error";
        const detail = message.message ?? "unknown";
        this.emitDebug("error", `server error ${code}: ${detail}`);
        this.options.onError(`${code}: ${detail}`);
      } else {
        this.emitDebug("debug", `recv ${message.type}`);
      }

      this.options.onMessage(message);
    };

    socket.onerror = () => {
      this.emitDebug("error", "socket error event");
      this.options.onError("Realtime websocket error.");
    };

    socket.onclose = (event) => {
      this.lastCloseCode = event.code;
      this.lastCloseReason = event.reason;
      this.authenticated = false;
      this.socket = null;
      this.emitDebug("warn", `socket closed code=${event.code} reason=${event.reason || "(none)"}`);
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
