import { JSDOM } from "jsdom";

test("session tools exposes secondary actions through an accessible sheet", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { SessionToolsPanel } = await import("../session-tools");
  const actions: string[] = [];
  const panel = new SessionToolsPanel(requireRoot(), [
    { label: "Timeline", action: () => actions.push("timeline") },
    { label: "Snapshots", action: () => actions.push("snapshots") },
    { label: "Recordings", action: () => actions.push("recordings") },
    { label: "Files", action: () => actions.push("files") },
    { label: "Share", action: () => actions.push("share") }
  ]);
  requireRoot().append(panel.element);
  expect(requireRoot().textContent).toBe("MORE");
  click(dom, panel.element);
  const dialog = requireRoot().querySelector('[role="dialog"]');
  expect(dialog?.getAttribute("aria-modal")).toBe("true");
  expect(panel.element.getAttribute("aria-expanded")).toBe("true");
  expect(buttons(dialog)).toEqual([
    "Close",
    "Timeline",
    "Snapshots",
    "Recordings",
    "Files",
    "Share"
  ]);
  click(dom, button(dialog, "Files"));
  expect(actions).toEqual(["files"]);
  expect(requireRoot().querySelector('[role="dialog"]')).toBeNull();
  expect(panel.element.getAttribute("aria-expanded")).toBe("false");
  dom.window.close();
});

test("session tools closes with Escape", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { SessionToolsPanel } = await import("../session-tools");
  const panel = new SessionToolsPanel(requireRoot(), [{ label: "Timeline", action: () => {} }]);
  requireRoot().append(panel.element);
  click(dom, panel.element);
  const modal = requireRoot().querySelector(".share-modal");
  if (!(modal instanceof dom.window.HTMLElement)) {
    throw new Error("missing tools sheet");
  }
  modal.dispatchEvent(new dom.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  expect(requireRoot().querySelector(".share-modal")).toBeNull();
  expect(panel.element.getAttribute("aria-expanded")).toBe("false");
  dom.window.close();
});

function requireRoot(): HTMLElement {
  const root = document.getElementById("root");
  if (root === null) {
    throw new Error("missing root");
  }
  return root;
}

function buttons(root: Element | null): string[] {
  if (root === null) {
    throw new Error("missing root");
  }
  return Array.from(root.querySelectorAll("button")).map((el) => el.textContent ?? "");
}

function button(root: Element | null, label: string): HTMLButtonElement {
  if (root === null) {
    throw new Error("missing root");
  }
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
