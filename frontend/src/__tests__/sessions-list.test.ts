import { JSDOM } from "jsdom";
import type { SessionSummary } from "../sessions";

test("session dashboard attaches and double-confirms kill", async () => {
  const dom = installDOM('<main id="sessions"></main>');
  const { SessionsListView } = await import("../sessions");
  const root = requireRoot();
  const navigated: string[] = [];
  const posts: Array<{ path: string; body: Record<string, unknown> }> = [];
  const view = new SessionsListView(
    root,
    async () => [session()],
    (id) => navigated.push(id),
    async (path, body) => {
      posts.push({ path, body });
      return new Response("ok", { status: 200 });
    }
  );
  await view.load();
  click(dom, button(root, "Attach"));
  expect(navigated).toEqual(["s1"]);
  click(dom, button(root, "KILL"));
  expect(posts).toEqual([]);
  expect(root.textContent).toContain("tap KILL again");
  click(dom, button(root, "Confirm KILL"));
  await settle();
  expect(posts).toEqual([{ path: "/control", body: { session_id: "s1", action: "kill" } }]);
  dom.window.close();
});

test("session dashboard exposes recovery and disables attachment", async () => {
  const dom = installDOM('<main id="sessions"></main>');
  const { SessionsListView } = await import("../sessions");
  const root = requireRoot();
  const navigated: string[] = [];
  const view = new SessionsListView(
    root,
    async () => [
      { ...session(), recovery_state: "orphaned", recovery_reason: "tmux reconnect timed out" }
    ],
    (id) => navigated.push(id)
  );
  await view.load();
  const attach = button(root, "orphaned");
  expect(attach.disabled).toBe(true);
  expect(root.querySelector(".session-list-row")?.classList.contains("recovery-unhealthy")).toBe(
    true
  );
  expect(root.textContent).toContain("recovery orphaned: tmux reconnect timed out");
  click(dom, root.querySelector(".session-list-row")!);
  expect(navigated).toEqual([]);
  dom.window.close();
});

test("session dashboard refreshes pending approvals from events", async () => {
  const dom = installDOM('<main id="sessions"></main>');
  const { SessionsListView } = await import("../sessions");
  const root = requireRoot();
  let rows = [{ ...session(), pending_approvals_count: 0 }];
  let requests = 0;
  const view = new SessionsListView(
    root,
    async () => {
      requests += 1;
      return rows;
    },
    () => {}
  );
  await view.load();
  rows = [{ ...session(), pending_approvals_count: 2 }];
  view.handleEnvelope({ type: "approval.requested", ts: "2026-07-08T00:01:01Z", payload: {} });
  await settle();
  expect(requests).toBe(2);
  expect(root.textContent).toContain("2 pending");
  dom.window.close();
});

function session(): SessionSummary {
  return {
    id: "s1",
    agent: "claude",
    cwd: "/tmp/repo",
    started_at: "2026-07-08T00:00:00Z",
    last_activity: "2026-07-08T00:01:00Z",
    pending_approvals_count: 1
  };
}

function requireRoot(): HTMLElement {
  const root = document.getElementById("sessions");
  if (root === null) {
    throw new Error("missing sessions root");
  }
  return root;
}

function installDOM(markup: string): JSDOM {
  const dom = new JSDOM(markup, { url: "https://onibi.test/#/sessions" });
  const win = dom.window;
  Object.defineProperty(globalThis, "window", { value: win, configurable: true });
  Object.defineProperty(globalThis, "document", { value: win.document, configurable: true });
  Object.defineProperty(globalThis, "navigator", { value: win.navigator, configurable: true });
  Object.defineProperty(globalThis, "HTMLElement", { value: win.HTMLElement, configurable: true });
  Object.defineProperty(globalThis, "HTMLButtonElement", {
    value: win.HTMLButtonElement,
    configurable: true
  });
  return dom;
}

function click(dom: JSDOM, el: Element): void {
  el.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
}

function button(root: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(root.querySelectorAll("button")).find((el) => el.textContent === label);
  if (!(found instanceof HTMLButtonElement)) {
    throw new Error(`missing ${label} button`);
  }
  return found;
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}
