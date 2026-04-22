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
      minimumSupportedRealtimeProtocolVersion: 2
    });

    expect(errors.at(-1)).toContain("Realtime protocol mismatch");
  });
});
