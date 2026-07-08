import { JSDOM } from "jsdom";

import { refreshPushSubscription } from "../push";

test("refresh replaces subscription when VAPID public key changes", async () => {
  const keyA = new Uint8Array([1, 2, 3, 4]);
  const keyB = new Uint8Array([5, 6, 7, 8]);
  const dom = installDOM();
  const oldSubscription = new FakeSubscription("old", keyA);
  let current: FakeSubscription | null = oldSubscription;
  const subscribedKeys: number[][] = [];
  const posts: Array<{ path: string; body: Record<string, unknown> }> = [];
  const pushManager = {
    getSubscription: async () => current as PushSubscription | null,
    subscribe: async (options: PushSubscriptionOptionsInit) => {
      const key = new Uint8Array(options.applicationServerKey as BufferSource);
      subscribedKeys.push(Array.from(key));
      current = new FakeSubscription("new", key);
      return current as PushSubscription;
    }
  };
  installPushEnvironment(dom, pushManager, keyB);
  window.localStorage.setItem("onibi-push-enabled", "1");

  await refreshPushSubscription(async (path, body) => {
    posts.push({ path, body });
    return new Response(null, { status: 204 });
  });

  expect(subscribedKeys).toEqual([Array.from(keyB)]);
  expect(oldSubscription.unsubscribed).toBe(true);
  expect(posts).toEqual([
    {
      path: "/push/subscribe",
      body: {
        endpoint: "https://push.example.invalid/new",
        keys: { p256dh: "new-p256dh", auth: "new-auth" }
      }
    }
  ]);
  dom.window.close();
});

class FakeSubscription {
  unsubscribed = false;

  constructor(
    private readonly id: string,
    private readonly applicationServerKey: Uint8Array
  ) {}

  get options(): PushSubscriptionOptions {
    return {
      applicationServerKey: this.applicationServerKey,
      userVisibleOnly: true
    };
  }

  async unsubscribe(): Promise<boolean> {
    this.unsubscribed = true;
    return true;
  }

  toJSON(): PushSubscriptionJSON {
    return {
      endpoint: `https://push.example.invalid/${this.id}`,
      keys: {
        p256dh: `${this.id}-p256dh`,
        auth: `${this.id}-auth`
      }
    };
  }
}

function installDOM(): JSDOM {
  const dom = new JSDOM("<!doctype html><body></body>", { url: "https://onibi.test/s/s1" });
  Object.defineProperty(globalThis, "window", { value: dom.window, configurable: true });
  Object.defineProperty(globalThis, "document", { value: dom.window.document, configurable: true });
  Object.defineProperty(globalThis, "navigator", {
    value: dom.window.navigator,
    configurable: true
  });
  return dom;
}

function installPushEnvironment(
  dom: JSDOM,
  pushManager: Pick<PushManager, "getSubscription" | "subscribe">,
  publicKey: Uint8Array
): void {
  const win = dom.window;
  Object.defineProperty(win, "matchMedia", {
    value: () => ({ matches: true }),
    configurable: true
  });
  Object.defineProperty(win, "Notification", {
    value: { permission: "granted" },
    configurable: true
  });
  Object.defineProperty(globalThis, "Notification", {
    value: win.Notification,
    configurable: true
  });
  Object.defineProperty(win, "PushManager", {
    value: function PushManager() {},
    configurable: true
  });
  Object.defineProperty(win.navigator, "serviceWorker", {
    value: { ready: Promise.resolve({ pushManager }) },
    configurable: true
  });
  Object.defineProperty(globalThis, "fetch", {
    value: async () =>
      new Response(JSON.stringify({ key: base64URL(win, publicKey) }), {
        status: 200,
        headers: { "Content-Type": "application/json" }
      }),
    configurable: true
  });
}

function base64URL(win: Window, bytes: Uint8Array): string {
  let raw = "";
  for (const byte of bytes) {
    raw += String.fromCharCode(byte);
  }
  return win.btoa(raw).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}
