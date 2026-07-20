import { JSDOM } from "jsdom";
import type { FleetStatus } from "../fleet-hosts";

test("fleet host view renders loading and failure states", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { FleetHostsPanel } = await import("../fleet-hosts");
  let resolve: ((value: FleetStatus) => void) | undefined;
  const panel = new FleetHostsPanel(
    requireRoot(),
    async () =>
      new Promise<FleetStatus>((done) => {
        resolve = done;
      })
  );
  panel.open();
  expect(requireRoot().textContent).toContain("loading fleet hosts");
  resolve?.(status());
  await settle();
  expect(requireRoot().textContent).toContain("Work Mac");
  dom.window.close();

  const failed = installDOM('<main id="root"></main>');
  const unavailable = new FleetHostsPanel(requireRoot(), async () => {
    throw new Error("offline");
  });
  unavailable.open();
  await settle();
  expect(requireRoot().textContent).toContain("fleet status unavailable");
  failed.window.close();
});

test("fleet host view shows health, transport, sessions, recovery, and approvals", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { FleetHostsPanel } = await import("../fleet-hosts");
  const panel = new FleetHostsPanel(requireRoot(), async () => status());
  panel.open();
  await settle();
  const work = button(requireRoot(), "Work Mac");
  expect(work.classList.contains("fleet-host-row")).toBe(true);
  expect(work.textContent).toContain("recovering");
  expect(requireRoot().textContent).toContain("Build Mac");
  expect(requireRoot().textContent).toContain("stale");
  expect(requireRoot().textContent).toContain("Phone");
  expect(requireRoot().textContent).toContain("healthy");
  expect(requireRoot().textContent).toContain("Old Mac");
  expect(requireRoot().textContent).toContain("unreachable");
  click(dom, work);
  expect(requireRoot().textContent).toContain("operational state");
  expect(requireRoot().textContent).toContain("transport");
  expect(requireRoot().textContent).toContain("https://work.tail.ts.net");
  expect(requireRoot().textContent).toContain("recovery reconnecting: websocket resumed");
  expect(requireRoot().textContent).toContain("pending approvals (1)");
  expect(requireRoot().textContent).toContain("claude / Bash");
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
        capabilities: ["session.read", "session.control"],
        state: "active",
        registered_at: "2026-07-14T00:00:00Z",
        last_seen_at: "2026-07-14T00:59:00Z"
      },
      {
        id: "host-build-mac",
        display_name: "Build Mac",
        endpoint: { kind: "mesh", url: "https://build.tail.ts.net" },
        protocol_version: 1,
        binary_version: "v1.2.3",
        capabilities: [],
        state: "stale",
        registered_at: "2026-07-14T00:00:00Z"
      },
      {
        id: "host-phone",
        display_name: "Phone",
        endpoint: { kind: "relay", url: "https://phone.example.test" },
        protocol_version: 1,
        binary_version: "v1.2.3",
        capabilities: [],
        state: "active",
        registered_at: "2026-07-14T00:00:00Z",
        last_seen_at: "2026-07-14T00:59:00Z"
      },
      {
        id: "host-old-mac",
        display_name: "Old Mac",
        endpoint: { kind: "mesh", url: "https://old.tail.ts.net" },
        protocol_version: 1,
        binary_version: "v1.2.3",
        capabilities: [],
        state: "revoked",
        registered_at: "2026-07-14T00:00:00Z"
      }
    ],
    sessions: [
      {
        id: "remote:host-work-mac",
        host_id: "host-work-mac",
        agent: "claude",
        state: "working",
        last_activity: "2026-07-14T00:59:00Z",
        pending_approvals: 1,
        recovery_state: "reconnecting",
        recovery_reason: "websocket resumed",
        recovery_updated_at: "2026-07-14T00:59:00Z"
      }
    ],
    pending_approvals: [
      {
        id: "approval-1",
        session_id: "remote:host-work-mac",
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
  const found = Array.from(root.querySelectorAll("button")).find((el) =>
    el.textContent?.includes(label)
  );
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
