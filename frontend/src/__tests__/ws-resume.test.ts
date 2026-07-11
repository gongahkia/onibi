import { EventsWS } from "../events";
import { TerminalWS } from "../ws";

class FakeWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: FakeWebSocket[] = [];

  readonly url: string;
  readonly protocols?: string | string[];
  readyState = FakeWebSocket.CONNECTING;
  binaryType: BinaryType = "blob";
  readonly sent: unknown[] = [];
  private readonly listeners = new Map<string, Set<EventListenerOrEventListenerObject>>();

  constructor(url: string | URL, protocols?: string | string[]) {
    this.url = String(url);
    this.protocols = protocols;
    FakeWebSocket.instances.push(this);
  }

  send(data: string | Blob | ArrayBufferLike | ArrayBufferView): void {
    this.sent.push(data);
  }

  close(): void {
    this.readyState = FakeWebSocket.CLOSED;
    this.dispatchEvent(new Event("close"));
  }

  open(): void {
    this.readyState = FakeWebSocket.OPEN;
    this.dispatchEvent(new Event("open"));
  }

  addEventListener(type: string, listener: EventListenerOrEventListenerObject | null): void {
    if (listener === null) {
      return;
    }
    const listeners = this.listeners.get(type) ?? new Set<EventListenerOrEventListenerObject>();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: EventListenerOrEventListenerObject | null): void {
    if (listener === null) {
      return;
    }
    this.listeners.get(type)?.delete(listener);
  }

  dispatchEvent(event: Event): boolean {
    for (const listener of this.listeners.get(event.type) ?? []) {
      if (typeof listener === "function") {
        listener.call(this, event);
      } else {
        listener.handleEvent(event);
      }
    }
    return !event.defaultPrevented;
  }
}

beforeEach(() => {
  vi.useFakeTimers();
  FakeWebSocket.instances = [];
  Object.defineProperty(globalThis, "WebSocket", {
    value: FakeWebSocket,
    configurable: true
  });
  Object.defineProperty(window, "WebSocket", {
    value: FakeWebSocket,
    configurable: true
  });
});

afterEach(() => {
  vi.useRealTimers();
});

test("terminal ws resumes immediately and reattaches from latest snapshot seq", async () => {
  const client = new TerminalWS();
  client.connect("wss://onibi.test/ws/pty", "session-1", 7);
  const first = FakeWebSocket.instances[0];
  expect(first.protocols).toEqual(["onibi.pty.v1"]);
  first.open();
  await flush();
  expect(JSON.parse(first.sent[0] as string)).toEqual({
    type: "attach",
    session_id: "session-1",
    last_seq: 7
  });
  first.dispatchEvent(
    new MessageEvent("message", {
      data: JSON.stringify({
        type: "snapshot",
        seq: 42,
        base64_data: window.btoa("restored")
      })
    })
  );
  await flush();
  first.close();
  expect(FakeWebSocket.instances).toHaveLength(1);
  client.resume();
  expect(FakeWebSocket.instances).toHaveLength(2);
  vi.advanceTimersByTime(8000);
  expect(FakeWebSocket.instances).toHaveLength(2);
  const second = FakeWebSocket.instances[1];
  second.open();
  await flush();
  expect(JSON.parse(second.sent[0] as string)).toEqual({
    type: "attach",
    session_id: "session-1",
    last_seq: 42
  });
  client.resume();
  expect(FakeWebSocket.instances).toHaveLength(2);
  client.close();
});

test("events ws resumes closed sockets without duplicating open sockets", async () => {
  const client = new EventsWS();
  client.connect("wss://onibi.test/ws/events");
  const first = FakeWebSocket.instances[0];
  expect(first.protocols).toEqual(["onibi.events.v1"]);
  client.resume();
  expect(FakeWebSocket.instances).toHaveLength(1);
  first.open();
  await flush();
  client.resume();
  expect(FakeWebSocket.instances).toHaveLength(1);
  first.close();
  client.resume();
  expect(FakeWebSocket.instances).toHaveLength(2);
  vi.advanceTimersByTime(8000);
  expect(FakeWebSocket.instances).toHaveLength(2);
  client.resume();
  expect(FakeWebSocket.instances).toHaveLength(2);
  client.close();
});

function flush(): Promise<void> {
  return Promise.resolve();
}
