import { JSDOM } from "jsdom";
import type { SessionsStatusPayload } from "../session-status";

test("session picker renders loading, failure, and prioritized sessions", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { SessionPickerPanel } = await import("../session-picker");
  let resolve: ((value: SessionsStatusPayload) => void) | undefined;
  const selected: string[] = [];
  const picker = new SessionPickerPanel(
    requireRoot(),
    async () =>
      new Promise<SessionsStatusPayload>((done) => {
        resolve = done;
      }),
    (session) => selected.push(session.id)
  );
  picker.open();
  expect(requireRoot().textContent).toContain("loading sessions");
  resolve?.(status());
  await settle();
  const rows = requireRoot().querySelectorAll(".session-picker-row");
  expect(rows[0]?.textContent).toContain("claude / awaiting-approval");
  click(dom, rows[0]!);
  expect(selected).toEqual(["await"]);
  dom.window.close();

  const failed = installDOM('<main id="root"></main>');
  const unavailable = new SessionPickerPanel(
    requireRoot(),
    async () => {
      throw new Error("offline");
    },
    () => {}
  );
  unavailable.open();
  await settle();
  expect(requireRoot().textContent).toContain("session picker unavailable");
  failed.window.close();
});

function status(): SessionsStatusPayload {
  return {
    generated_at: "2026-07-14T01:00:00Z",
    counts: { idle: 1, working: 1, "awaiting-approval": 1, blocked: 0, recovering: 0, failed: 0 },
    sessions: [
      session("idle", "shell", "idle", "2026-07-14T01:00:00Z"),
      session("working", "codex", "working", "2026-07-14T00:59:00Z"),
      session("await", "claude", "awaiting-approval", "2026-07-14T00:58:00Z")
    ]
  };
}

function session(
  id: string,
  agent: string,
  state: SessionsStatusPayload["sessions"][number]["state"],
  lastActivity: string
): SessionsStatusPayload["sessions"][number] {
  return {
    id,
    agent,
    state,
    last_activity: lastActivity,
    pending_approvals_count: 0,
    role_required: "owner"
  };
}

function requireRoot(): HTMLElement {
  const root = document.getElementById("root");
  if (root === null) {
    throw new Error("missing root");
  }
  return root;
}

function installDOM(markup: string): JSDOM {
  const dom = new JSDOM(markup, { url: "https://onibi.test/" });
  const win = dom.window;
  Object.defineProperty(globalThis, "window", { value: win, configurable: true });
  Object.defineProperty(globalThis, "document", { value: win.document, configurable: true });
  Object.defineProperty(globalThis, "HTMLElement", { value: win.HTMLElement, configurable: true });
  return dom;
}

function click(dom: JSDOM, el: Element): void {
  el.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}
