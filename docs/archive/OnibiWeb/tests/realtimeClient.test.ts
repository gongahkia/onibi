import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { RealtimeClient } from "../src/api/realtimeClient";
import type {
  RealtimeClientMessage,
  RealtimeServerMessage
} from "../src/types";

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readonly url: string;
  readyState = MockWebSocket.CONNECTING;
  sentFrames: string[] = [];

  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: string): void {
    this.sentFrames.push(String(data));
  }

  close(): void {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.(new CloseEvent("close"));
  }

  serverOpen(): void {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.(new Event("open"));
  }

  serverMessage(message: RealtimeServerMessage): void {
    this.onmessage?.({
      data: JSON.stringify(message)
    } as MessageEvent);
  }

  serverBinary(data: ArrayBuffer): void {
    this.onmessage?.({
      data
    } as MessageEvent);
  }

  serverClose(): void {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.(new CloseEvent("close"));
  }
}

function decodeFrames(socket: MockWebSocket): RealtimeClientMessage[] {
  return socket.sentFrames.map((payload) => JSON.parse(payload) as RealtimeClientMessage);
}

describe("RealtimeClient", () => {
  const originalWebSocket = globalThis.WebSocket;

  beforeEach(() => {
    MockWebSocket.instances = [];
    vi.useFakeTimers();
    globalThis.WebSocket = MockWebSocket as unknown as typeof WebSocket;
  });

  afterEach(() => {
    globalThis.WebSocket = originalWebSocket;
    vi.useRealTimers();
  });

  it("drops stale disconnected unsubscribe and de-duplicates queued subscription frames", () => {
    const client = new RealtimeClient({
      connection: {
        baseURL: "http://127.0.0.1:8787",
        token: "token"
      },
      onStateChange: () => undefined,
      onMessage: () => undefined,
      onError: () => undefined
    });

    client.connect();
    const socket = MockWebSocket.instances[0];
    expect(socket).toBeDefined();

    client.send({ type: "subscribe", sessionId: "s1" });
    client.send({ type: "subscribe", sessionId: "s1" });
    client.send({ type: "request_buffer", sessionId: "s1" });
    client.send({ type: "request_buffer", sessionId: "s1" });
    client.send({ type: "unsubscribe", sessionId: "s1" });

    socket.serverOpen();
    socket.serverMessage({ type: "auth_ok" });

    const frames = decodeFrames(socket);
    expect(frames.map((frame) => frame.type)).toEqual(["auth", "subscribe", "request_buffer"]);
    expect(frames.filter((frame) => frame.type === "subscribe")).toHaveLength(1);
    expect(frames.filter((frame) => frame.type === "request_buffer")).toHaveLength(1);
    expect(frames.filter((frame) => frame.type === "unsubscribe")).toHaveLength(0);
  });

  it("flushes queued session messages after reconnect authentication", () => {
    const states: string[] = [];
    const client = new RealtimeClient({
      connection: {
        baseURL: "http://127.0.0.1:8787",
        token: "token"
      },
      onStateChange: (state) => {
        states.push(state);
      },
      onMessage: () => undefined,
      onError: () => undefined
    });

    client.connect();
    const socket1 = MockWebSocket.instances[0];
    socket1.serverOpen();
    socket1.serverMessage({ type: "auth_ok" });
    socket1.serverClose();

    client.send({ type: "subscribe", sessionId: "s1" });
    client.send({ type: "request_buffer", sessionId: "s1" });

    vi.advanceTimersByTime(700);
    const socket2 = MockWebSocket.instances[1];
    expect(socket2).toBeDefined();

    socket2.serverOpen();
    socket2.serverMessage({ type: "auth_ok" });

    const frames = decodeFrames(socket2);
    expect(frames.map((frame) => frame.type)).toEqual(["auth", "subscribe", "request_buffer"]);
    expect(states).toContain("reconnecting");
    expect(states.at(-1)).toBe("authenticated");
  });

  it("surfaces realtime protocol incompatibility on auth", () => {
    const errors: string[] = [];
    const client = new RealtimeClient({
      connection: {
        baseURL: "http://127.0.0.1:8787",
        token: "token"
      },
      onStateChange: () => undefined,
      onMessage: () => undefined,
      onError: (message) => {
        errors.push(message);
      }
    });

    client.connect();
    const socket = MockWebSocket.instances[0];
    socket.serverOpen();
    socket.serverMessage({
      type: "auth_ok",
      realtimeProtocolVersion: 2,
      minimumSupportedRealtimeProtocolVersion: 4
    });

    expect(errors.at(-1)).toContain("Realtime protocol mismatch");
  });

  it("decodes binary realtime output batches", () => {
    const messages: RealtimeServerMessage[] = [];
    const client = new RealtimeClient({
      connection: {
        baseURL: "http://127.0.0.1:8787",
        token: "token"
      },
      onStateChange: () => undefined,
      onMessage: (message) => {
        messages.push(message);
      },
      onError: () => undefined
    });

    client.connect();
    const socket = MockWebSocket.instances[0];
    socket.serverOpen();
    socket.serverMessage({ type: "auth_ok" });

    socket.serverBinary(makeOutputBatchFrame("session-1", "cursor-1", new Uint8Array([104, 105])));

    const outputMessage = messages.find((message) => message.type === "output_batch");
    expect(outputMessage?.sessionId).toBe("session-1");
    expect(outputMessage?.batch?.header.endCursor).toBe("cursor-1");
    expect([...((outputMessage?.batch?.data ?? new Uint8Array()))]).toEqual([104, 105]);
  });
});

function makeOutputBatchFrame(sessionId: string, cursor: string, data: Uint8Array): ArrayBuffer {
  const header = new TextEncoder().encode(
    JSON.stringify({
      type: "output_batch",
      sessionId,
      stream: "stdout",
      timestamp: "1970-01-01T00:00:00Z",
      startCursor: cursor,
      endCursor: cursor,
      chunkIds: [cursor],
      byteCount: data.byteLength,
      droppedByteCount: 0,
      truncated: false
    })
  );
  const frame = new Uint8Array(1 + 4 + header.byteLength + data.byteLength);
  frame[0] = 0x01;
  frame[1] = (header.byteLength >> 24) & 0xff;
  frame[2] = (header.byteLength >> 16) & 0xff;
  frame[3] = (header.byteLength >> 8) & 0xff;
  frame[4] = header.byteLength & 0xff;
  frame.set(header, 5);
  frame.set(data, 5 + header.byteLength);
  return frame.buffer;
}
