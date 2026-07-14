import { JSDOM } from "jsdom";
import type { FleetStatus } from "../fleet-hosts";

test("approval inbox renders loading and failure states", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { ApprovalInboxPanel } = await import("../approval-inbox");
  let resolve: ((value: FleetStatus) => void) | undefined;
  const panel = new ApprovalInboxPanel(
    requireRoot(),
    async () =>
      new Promise<FleetStatus>((done) => {
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

test("approval inbox shows exact provenance and confirms its action target", async () => {
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
  const remote = card(requireRoot(), "approval-remote");
  expect(remote.textContent).toContain("host: Work Mac / host-work-mac");
  expect(remote.textContent).toContain("session: remote:host-work-mac");
  expect(remote.textContent).toContain("agent: claude");
  const local = card(requireRoot(), "approval-local");
  expect(local.textContent).toContain("host: this hub");
  const unknown = card(requireRoot(), "approval-unknown");
  expect(unknown.textContent).toContain("host: not reported");
  expect(button(unknown, "Approve").disabled).toBe(true);
  click(dom, button(remote, "Approve"));
  expect(requireRoot().textContent).toContain("Confirm approve");
  click(dom, button(requireRoot(), "Confirm approve"));
  await settle();
  expect(calls).toEqual([{ path: "/approval/approval-remote", body: { verdict: "approve" } }]);
  dom.window.close();
});

function status(): FleetStatus {
  return {
    generated_at: "2026-07-14T01:00:00Z",
    hosts: [
      {
        id: "host-work-mac",
        display_name: "Work Mac",
        endpoint: { kind: "mesh", url: "https://work.tail.ts.net" },
        protocol_version: 1,
        binary_version: "v1.2.3",
        capabilities: [],
        state: "active",
        registered_at: "2026-07-14T00:00:00Z",
        last_seen_at: "2026-07-14T00:59:00Z"
      }
    ],
    sessions: [
      {
        id: "remote:host-work-mac",
        host_id: "host-work-mac",
        agent: "claude",
        state: "awaiting-approval",
        last_activity: "2026-07-14T00:59:00Z",
        pending_approvals: 1,
        remote: true
      },
      {
        id: "local-session",
        agent: "codex",
        state: "awaiting-approval",
        last_activity: "2026-07-14T00:59:00Z",
        pending_approvals: 1
      }
    ],
    pending_approvals: [
      {
        id: "approval-remote",
        host_id: "host-work-mac",
        session_id: "remote:host-work-mac",
        agent: "claude",
        tool: "Bash",
        state: "pending",
        created_at: "2026-07-14T00:59:00Z",
        expires_at: "2026-07-14T01:04:00Z"
      },
      {
        id: "approval-local",
        session_id: "local-session",
        agent: "codex",
        tool: "Write",
        state: "pending",
        created_at: "2026-07-14T00:59:00Z",
        expires_at: "2026-07-14T01:04:00Z"
      },
      {
        id: "approval-unknown",
        host_id: "host-unreported",
        session_id: "local-session",
        agent: "codex",
        tool: "Write",
        state: "pending",
        created_at: "2026-07-14T00:59:00Z",
        expires_at: "2026-07-14T01:04:00Z"
      }
    ]
  };
}

function requireRoot(): HTMLElement {
  const root = document.getElementById("root");
  if (root === null) {
    throw new Error("missing root");
  }
  return root;
}

function card(root: HTMLElement, id: string): HTMLElement {
  const found = root.querySelector<HTMLElement>(`[data-approval-id="${id}"]`);
  if (found === null) {
    throw new Error(`missing ${id}`);
  }
  return found;
}

function button(root: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(root.querySelectorAll("button")).find((el) => el.textContent === label);
  if (!(found instanceof HTMLButtonElement)) {
    throw new Error(`missing ${label} button`);
  }
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
