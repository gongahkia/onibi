import { JSDOM } from "jsdom";
import type { ApprovalRequestedPayload } from "../events";

test("renders approval JSON and posts approve and deny decisions", async () => {
  const dom = installDOM('<main id="approvals"></main>');
  const { ApprovalOverlay } = await import("../approval");
  const root = document.getElementById("approvals");
  if (root === null) {
    throw new Error("missing approvals root");
  }
  const calls: Array<{ path: string; body: Record<string, string> }> = [];
  const overlay = new ApprovalOverlay(root);
  overlay.setPostJSON(async (path, body) => {
    calls.push({ path, body });
    return new Response("ok", { status: 200 });
  });
  overlay.handleEnvelope({
    type: "approval.requested",
    ts: "2026-07-08T00:00:00Z",
    payload: approvalPayload()
  });
  expect(root.querySelector(".approval-card")?.textContent).toContain("shell");
  expect(root.textContent).toContain("target: not reported");
  expect(root.querySelector(".approval-input")?.hasAttribute("open")).toBe(false);
  expect(root.textContent).toContain('"cmd"');
  button(root, "Approve").click();
  await settle();
  expect(root.querySelector(".approval-status")?.textContent).toBe("Done.");
  button(root, "Deny").click();
  await settle();
  expect(calls).toEqual([
    { path: "/approval/ap-1", body: { verdict: "approve" } },
    { path: "/approval/ap-1", body: { verdict: "deny" } }
  ]);
  overlay.handleEnvelope({
    type: "approval.decided",
    ts: "2026-07-08T00:00:01Z",
    payload: { id: "ap-1", session_id: "s-1", verdict: "approve" }
  });
  expect(root.querySelector(".approval-card")).toBeNull();
  dom.window.close();
});

test("shows compact loading, failed, and successful approval states", async () => {
  const dom = installDOM('<main id="approvals"></main>');
  const { ApprovalOverlay } = await import("../approval");
  const root = document.getElementById("approvals");
  if (root === null) {
    throw new Error("missing approvals root");
  }
  const overlay = new ApprovalOverlay(root);
  overlay.handleEnvelope({
    type: "approval.requested",
    ts: "2026-07-08T00:00:00Z",
    payload: {
      ...approvalPayload(),
      id: "ap-loading",
      file_path: "/tmp/onibi-test.txt",
      unified_diff: smallUnifiedDiff()
    }
  });
  expect(root.textContent).toContain("target: /tmp/onibi-test.txt");
  expect(root.textContent).not.toContain("Auto-approve");
  expect(root.querySelector(".approval-input")?.tagName).toBe("DETAILS");
  expect(root.querySelector(".approval-diff")?.textContent).toBe("Loading diff...");

  overlay.setPostJSON(async () => {
    throw new TypeError("offline");
  });
  button(root, "Approve").click();
  expect(root.querySelector(".approval-status")?.textContent).toBe("Sending...");
  await settle();
  expect(root.querySelector(".approval-status")?.textContent).toBe("Connection failed. Retry.");

  overlay.setPostJSON(async () => new Response("ok", { status: 200 }));
  button(root, "Deny").click();
  await settle();
  expect(root.querySelector(".approval-status")?.textContent).toBe("Done.");
  dom.window.close();
});

test("renders unified diff approval and paginates large diffs", async () => {
  const dom = installDOM('<main id="approvals"></main>');
  const { ApprovalOverlay } = await import("../approval");
  const root = document.getElementById("approvals");
  if (root === null) {
    throw new Error("missing approvals root");
  }
  const overlay = new ApprovalOverlay(root);
  overlay.handleEnvelope({
    type: "approval.requested",
    ts: "2026-07-08T00:00:00Z",
    payload: {
      ...approvalPayload(),
      id: "ap-diff",
      tool: "Write",
      unified_diff: smallUnifiedDiff()
    }
  });
  const diff = root.querySelector(".approval-diff");
  await waitFor(() => diff?.textContent?.includes("new") === true);
  expect(diff?.textContent).toContain("new");
  overlay.handleEnvelope({
    type: "approval.requested",
    ts: "2026-07-08T00:00:01Z",
    payload: {
      ...approvalPayload(),
      id: "ap-large-diff",
      tool: "FileEdit",
      unified_diff: largeUnifiedDiff()
    }
  });
  const summaries = Array.from(root.querySelectorAll(".approval-diff-summary"));
  expect(summaries.some((el) => el.textContent?.includes("205 line diff"))).toBe(true);
  expect(root.textContent).toContain("Show more (205 lines)");
  dom.window.close();
});

function approvalPayload(): ApprovalRequestedPayload {
  return {
    id: "ap-1",
    session_id: "s-1",
    agent: "codex",
    tool: "shell",
    scrubbed_input: '{"cmd":"rm -rf /tmp/onibi-test"}',
    risk_level: "low",
    expires_at: "2026-07-08T00:05:00Z"
  };
}

function smallUnifiedDiff(): string {
  return ["--- a/file.txt", "+++ b/file.txt", "@@ -1 +1 @@", "-old", "+new"].join("\n");
}

function largeUnifiedDiff(): string {
  return Array.from({ length: 205 }, (_, i) => `+line ${i}`).join("\n");
}

function installDOM(markup: string): JSDOM {
  const dom = new JSDOM(markup, { url: "https://onibi.test/" });
  const win = dom.window;
  Object.defineProperty(globalThis, "window", { value: win, configurable: true });
  Object.defineProperty(globalThis, "document", { value: win.document, configurable: true });
  Object.defineProperty(globalThis, "navigator", { value: win.navigator, configurable: true });
  Object.defineProperty(globalThis, "HTMLElement", { value: win.HTMLElement, configurable: true });
  Object.defineProperty(globalThis, "HTMLButtonElement", {
    value: win.HTMLButtonElement,
    configurable: true
  });
  Object.defineProperty(win, "matchMedia", {
    value: () => ({ matches: false }),
    configurable: true
  });
  Object.defineProperty(win.HTMLElement.prototype, "scrollIntoView", {
    value() {},
    configurable: true
  });
  Object.defineProperty(win.navigator, "vibrate", { value() {}, configurable: true });
  return dom;
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

async function waitFor(assertion: () => boolean): Promise<void> {
  for (let i = 0; i < 100; i += 1) {
    if (assertion()) {
      return;
    }
    await new Promise((resolve) => window.setTimeout(resolve, 10));
  }
  throw new Error("condition did not settle");
}
