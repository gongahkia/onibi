import { decodeText, RelayE2E } from "./e2e";

type SnapshotFrame = {
  type: "snapshot";
  seq: number;
  base64_data: string;
};

type ResizeFrame = {
  type: "resize";
  rows: number;
  cols: number;
};

type ServerFrame = SnapshotFrame | ResizeFrame;

export class TerminalWS extends EventTarget {
  private url = "";
  private sessionId = "";
  private lastSeq = 0;
  private ws: WebSocket | undefined;
  private reconnectTimer = 0;
  private attempts = 0;
  private stopped = false;
  private e2e: RelayE2E | undefined;

  setE2E(e2e: RelayE2E | undefined): void {
    this.e2e = e2e;
  }

  connect(url: string, sessionId: string, lastSeq = 0): void {
    this.url = url;
    this.sessionId = sessionId;
    this.lastSeq = lastSeq;
    this.stopped = false;
    this.open();
  }

  close(): void {
    this.stopped = true;
    window.clearTimeout(this.reconnectTimer);
    this.ws?.close();
  }

  sendText(data: string): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      void this.sendTyped("text", new TextEncoder().encode(data));
    }
  }

  sendBinary(data: Uint8Array | ArrayBuffer): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      void this.sendTyped("binary", data instanceof Uint8Array ? data : new Uint8Array(data));
    }
  }

  sendResize(rows: number, cols: number): void {
    this.sendText(JSON.stringify({ type: "resize", rows, cols }));
  }

  private open(): void {
    const socket = new WebSocket(this.url, ["onibi.pty.v1"]);
    socket.binaryType = "arraybuffer";
    this.ws = socket;
    socket.addEventListener("open", () => void this.handleOpen(socket));
    socket.addEventListener("message", (event) => void this.handleMessage(event));
    socket.addEventListener("close", () => {
      this.dispatchEvent(new Event("close"));
      if (!this.stopped) {
        this.scheduleReconnect();
      }
    });
    socket.addEventListener("error", () => socket.close());
  }

  private async handleOpen(socket: WebSocket): Promise<void> {
    this.attempts = 0;
    const attach: { type: "attach"; session_id: string; last_seq: number; verify_token?: string } =
      {
        type: "attach",
        session_id: this.sessionId,
        last_seq: this.lastSeq
      };
    if (this.e2e !== undefined) {
      this.e2e.startStream("ws:pty");
      attach.verify_token = this.e2e.verifyToken();
    }
    await this.sendTyped("text", new TextEncoder().encode(JSON.stringify(attach)), socket);
    this.dispatchEvent(new Event("open"));
  }

  private async sendTyped(
    type: "text" | "binary",
    data: Uint8Array,
    socket = this.ws
  ): Promise<void> {
    if (socket?.readyState !== WebSocket.OPEN) {
      return;
    }
    if (this.e2e === undefined) {
      socket.send(type === "text" ? decodeText(data) : data);
      return;
    }
    socket.send(await this.e2e.sealBytes(type, data, "ws:pty"));
  }

  private scheduleReconnect(): void {
    window.clearTimeout(this.reconnectTimer);
    const delay = Math.min(8000, 250 * 2 ** this.attempts);
    this.attempts += 1;
    this.dispatchEvent(new CustomEvent<number>("reconnecting", { detail: delay }));
    this.reconnectTimer = window.setTimeout(() => this.open(), delay);
  }

  private async handleMessage(event: MessageEvent): Promise<void> {
    if (this.e2e !== undefined) {
      if (typeof event.data !== "string") {
        this.ws?.close();
        return;
      }
      const frame = await this.e2e.open(event.data, "ws:pty");
      if (frame.type === "text") {
        this.handleText(decodeText(frame.data));
        return;
      }
      this.handleBinary(arrayBuffer(frame.data));
      return;
    }
    if (typeof event.data === "string") {
      this.handleText(event.data);
      return;
    }
    if (event.data instanceof Blob) {
      this.handleBinary(await event.data.arrayBuffer());
      return;
    }
    this.handleBinary(event.data as ArrayBuffer);
  }

  private handleText(raw: string): void {
    const frame = JSON.parse(raw) as ServerFrame;
    if (frame.type === "snapshot") {
      const data = decodeBase64(frame.base64_data);
      this.lastSeq = frame.seq;
      this.dispatchEvent(new CustomEvent<ArrayBuffer>("data", { detail: data }));
      return;
    }
    if (frame.type === "resize") {
      this.dispatchEvent(new CustomEvent<ResizeFrame>("resize", { detail: frame }));
    }
  }

  private handleBinary(data: ArrayBuffer): void {
    this.lastSeq += data.byteLength;
    this.dispatchEvent(new CustomEvent<ArrayBuffer>("data", { detail: data }));
  }
}

function decodeBase64(value: string): ArrayBuffer {
  const binary = window.atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
}

function arrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const out = new Uint8Array(bytes.byteLength);
  out.set(bytes);
  return out.buffer;
}
