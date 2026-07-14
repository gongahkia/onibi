import { JSDOM } from "jsdom";
import type { FleetStatus } from "../fleet-hosts";

test("fleet home renders loading and failure states", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { FleetHomeView } = await import("../fleet-home");
  let resolve: ((value: FleetStatus) => void) | undefined;
  const home = new FleetHomeView(
    requireRoot(),
    async () =>
      new Promise<FleetStatus>((done) => {
        resolve = done;
      }),
    () => {},
    () => {},
    () => {},
    () => {},
    document.createElement("div")
  );
  const loading = home.load();
  expect(requireRoot().textContent).toContain("loading fleet home");
  resolve?.(status());
  await loading;
  expect(requireRoot().textContent).toContain("pending approvals (1)");
  dom.window.close();

  const failed = installDOM('<main id="root"></main>');
  const unavailable = new FleetHomeView(
    requireRoot(),
    async () => {
      throw new Error("offline");
    },
    () => {},
    () => {},
    () => {},
    () => {},
    document.createElement("div")
  );
  await unavailable.load();
  expect(requireRoot().textContent).toContain("fleet home unavailable");
  failed.window.close();
});

test("fleet home prioritizes approvals, host attention, and live session state", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { FleetHomeView } = await import("../fleet-home");
  const hosts: string[] = [];
  const sessions: string[] = [];
  let inbox = 0;
  const home = new FleetHomeView(
    requireRoot(),
    async () => status(),
    (session) => sessions.push(session.id),
    (id) => hosts.push(id),
    () => {
      inbox++;
    },
    () => {},
    document.createElement("div")
  );
  await home.load();
  const content = requireRoot().textContent ?? "";
  expect(content.indexOf("pending approvals (1)")).toBeLessThan(content.indexOf("host attention"));
  expect(content.indexOf("host attention")).toBeLessThan(content.indexOf("active sessions"));
  click(dom, button(requireRoot(), "review approval inbox"));
  click(dom, button(requireRoot(), "Work Mac / stale"));
  click(dom, button(requireRoot(), "claude / awaiting-approval / session-1"));
  expect(inbox).toBe(1);
  expect(hosts).toEqual(["host-work-mac"]);
  expect(sessions).toEqual(["session-1"]);
  home.handleEnvelope({
    type: "sessions.status",
    ts: "2026-07-14T01:00:00Z",
    payload: {
      generated_at: "2026-07-14T01:00:00Z",
      counts: { idle: 0, working: 1, "awaiting-approval": 0, blocked: 0 },
      sessions: [
        {
          id: "session-2",
          agent: "codex",
          state: "working",
          last_activity: "2026-07-14T01:00:00Z",
          pending_approvals_count: 0,
          role_required: "owner"
        }
      ]
    }
  });
  expect(requireRoot().textContent).toContain("codex / working / session-2");
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
        state: "stale",
        registered_at: "2026-07-14T00:00:00Z"
      }
    ],
    sessions: [
      {
        id: "session-1",
        agent: "claude",
        state: "awaiting-approval",
        last_activity: "2026-07-14T00:59:00Z",
        pending_approvals: 1
      }
    ],
    pending_approvals: [
      {
        id: "approval-1",
        session_id: "session-1",
        agent: "claude",
        tool: "Bash",
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
