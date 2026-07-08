import { JSDOM } from "jsdom";
import type { SessionsStatusPayload } from "../agents-feed";

test("agents feed renders state counts and navigates rows", async () => {
  const dom = installDOM('<div id="toolbar"></div>');
  const { AgentsFeed } = await import("../agents-feed");
  const navigated: string[] = [];
  const feed = new AgentsFeed(
    async () => statusPayload(),
    (id) => navigated.push(id)
  );
  document.getElementById("toolbar")?.append(feed.element);
  await feed.load();
  expect(feed.element.textContent).toContain("AGT 3");
  click(dom, feed.element.querySelector("button")!);
  expect(feed.element.textContent).toContain("claude s1");
  const row = feed.element.querySelector(".agents-feed-row");
  if (row === null) {
    throw new Error("missing feed row");
  }
  click(dom, row);
  expect(navigated).toEqual(["s1"]);
  dom.window.close();
});

test("agents feed accepts sessions status websocket payload", async () => {
  const dom = installDOM('<div id="toolbar"></div>');
  const { AgentsFeed } = await import("../agents-feed");
  const feed = new AgentsFeed(
    async () => emptyPayload(),
    () => {}
  );
  document.getElementById("toolbar")?.append(feed.element);
  feed.handleEnvelope({
    type: "sessions.status",
    ts: "2026-07-08T00:00:00Z",
    payload: statusPayload()
  });
  expect(feed.element.textContent).toContain("AGT 3");
  const dots = feed.element.querySelectorAll(".agent-dot:not([hidden])");
  expect(dots.length).toBe(3);
  dom.window.close();
});

function statusPayload(): SessionsStatusPayload {
  return {
    generated_at: "2026-07-08T00:01:00Z",
    counts: {
      idle: 1,
      working: 1,
      "awaiting-approval": 1,
      blocked: 0
    },
    sessions: [
      session("s1", "claude", "awaiting-approval", "2026-07-08T00:01:00Z", 1),
      session("s2", "codex", "working", "2026-07-08T00:02:00Z", 0),
      session("s3", "shell", "idle", "2026-07-08T00:00:00Z", 0)
    ]
  };
}

function emptyPayload(): SessionsStatusPayload {
  return {
    generated_at: "",
    counts: {
      idle: 0,
      working: 0,
      "awaiting-approval": 0,
      blocked: 0
    },
    sessions: []
  };
}

function session(
  id: string,
  agent: string,
  state: SessionsStatusPayload["sessions"][number]["state"],
  lastActivity: string,
  pending: number
): SessionsStatusPayload["sessions"][number] {
  return {
    id,
    agent,
    state,
    last_activity: lastActivity,
    pending_approvals_count: pending,
    role_required: "owner"
  };
}

function installDOM(markup: string): JSDOM {
  const dom = new JSDOM(markup, { url: "https://onibi.test/s/s1" });
  const win = dom.window;
  Object.defineProperty(globalThis, "window", { value: win, configurable: true });
  Object.defineProperty(globalThis, "document", { value: win.document, configurable: true });
  Object.defineProperty(globalThis, "navigator", { value: win.navigator, configurable: true });
  Object.defineProperty(globalThis, "HTMLElement", { value: win.HTMLElement, configurable: true });
  Object.defineProperty(globalThis, "HTMLButtonElement", {
    value: win.HTMLButtonElement,
    configurable: true
  });
  Object.defineProperty(globalThis, "Node", { value: win.Node, configurable: true });
  return dom;
}

function click(dom: JSDOM, el: Element): void {
  el.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
}
