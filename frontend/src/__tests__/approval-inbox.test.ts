import { JSDOM } from "jsdom";
import type { PendingApprovals } from "../approval-inbox";

test("approval inbox renders loading and failure states", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { ApprovalInboxPanel } = await import("../approval-inbox");
  let resolve: ((value: PendingApprovals) => void) | undefined;
  const panel = new ApprovalInboxPanel(
    requireRoot(),
    async () =>
      new Promise<PendingApprovals>((done) => {
        resolve = done;
      }),
    async () => new Response("ok", { status: 200 }),
    () => {}
  );
  panel.open();
  expect(requireRoot().textContent).toContain("loading approval inbox");
  resolve?.(status());
  await settle();
  expect(requireRoot().textContent).toContain("Bash");
  dom.window.close();

  const failed = installDOM('<main id="root"></main>');
  const unavailable = new ApprovalInboxPanel(
    requireRoot(),
    async () => {
      throw new Error("offline");
    },
    async () => new Response("ok", { status: 200 }),
    () => {}
  );
  unavailable.open();
  await settle();
  expect(requireRoot().textContent).toContain("approval inbox unavailable");
  failed.window.close();
});

test("approval inbox confirms its local action target", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { ApprovalInboxPanel } = await import("../approval-inbox");
  const calls: Array<{ path: string; body: Record<string, unknown> }> = [];
  const panel = new ApprovalInboxPanel(
    requireRoot(),
    async () => status(),
    async (path, body) => {
      calls.push({ path, body });
      return new Response("ok", { status: 200 });
    },
    () => {}
  );
  panel.open();
  await settle();
  const approval = card(requireRoot(), "approval-local");
  expect(approval.textContent).toContain("session: local-session");
  expect(approval.textContent).toContain("agent: codex");
  click(dom, button(approval, "Approve"));
  expect(requireRoot().textContent).toContain("Confirm approve");
  click(dom, button(requireRoot(), "Confirm approve"));
  await settle();
  expect(calls).toEqual([{ path: "/approval/approval-local", body: { verdict: "approve" } }]);
  dom.window.close();
});

test("approval inbox retains cached approvals and locks a pending decision", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { ApprovalInboxPanel } = await import("../approval-inbox");
  let fail = false;
  let calls = 0;
  let resolvePost: ((response: Response) => void) | undefined;
  const panel = new ApprovalInboxPanel(
    requireRoot(),
    async () => {
      if (fail) throw new Error("offline");
      return status();
    },
    async () => {
      calls += 1;
      return new Promise<Response>((resolve) => {
        resolvePost = resolve;
      });
    },
    () => {}
  );
  panel.open();
  await settle();
  fail = true;
  click(dom, button(requireRoot(), "Reload"));
  await settle();
  expect(requireRoot().textContent).toContain("approval data may be stale: reconnect then reload");
  expect(requireRoot().textContent).toContain("approval-local");
  click(dom, button(card(requireRoot(), "approval-local"), "Approve"));
  click(dom, button(requireRoot(), "Confirm approve"));
  click(dom, button(requireRoot(), "Approve"));
  expect(calls).toBe(1);
  expect(button(requireRoot(), "Approve").disabled).toBe(true);
  resolvePost?.(new Response("ok", { status: 200 }));
  await settle();
  dom.window.close();
});

function status(): PendingApprovals {
  return {
    approvals: [
      {
        id: "approval-local",
        session_id: "local-session",
        agent: "codex",
        tool: "Bash",
        expires_at: "2026-07-14T01:04:00Z"
      },
      {
        id: "approval-second",
        session_id: "second-session",
        agent: "claude",
        tool: "Write",
        expires_at: "2026-07-14T01:04:00Z"
      }
    ]
  };
}

function requireRoot(): HTMLElement {
  const root = document.getElementById("root");
  if (root === null) throw new Error("missing root");
  return root;
}

function card(root: HTMLElement, id: string): HTMLElement {
  const found = root.querySelector<HTMLElement>(`[data-approval-id="${id}"]`);
  if (found === null) throw new Error(`missing ${id}`);
  return found;
}

function button(root: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(root.querySelectorAll("button")).find((el) => el.textContent === label);
  if (!(found instanceof HTMLButtonElement)) throw new Error(`missing ${label} button`);
  return found;
}

function installDOM(markup: string): JSDOM {
  const dom = new JSDOM(markup, { url: "https://onibi.test/" });
  const win = dom.window;
  Object.defineProperty(globalThis, "window", { value: win, configurable: true });
  Object.defineProperty(globalThis, "document", { value: win.document, configurable: true });
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

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}
