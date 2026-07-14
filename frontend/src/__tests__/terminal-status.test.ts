import { JSDOM } from "jsdom";

test("terminal status renders loading, recovery, failure, and fleet exit", async () => {
  const dom = installDOM('<div id="status"></div>');
  const { TerminalStatus } = await import("../terminal-status");
  const root = document.getElementById("status");
  if (root === null) {
    throw new Error("missing status root");
  }
  let exits = 0;
  const status = new TerminalStatus(() => {
    exits += 1;
  });
  root.append(status.element);
  expect(root.textContent).toContain("Connecting terminal...");
  status.setConnected();
  expect(root.textContent).toContain("Terminal live");
  status.setDisconnected();
  expect(root.textContent).toContain("Disconnected. Retrying...");
  status.setRecovered({ mode: "snapshot", replay_bytes: 12 });
  expect(root.textContent).toContain("Recovered buffered output");
  const fleet = root.querySelector("button");
  if (!(fleet instanceof HTMLButtonElement)) {
    throw new Error("missing fleet button");
  }
  expect(fleet.tabIndex).toBe(0);
  fleet.click();
  expect(exits).toBe(1);
  dom.window.close();
});

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
  Object.defineProperty(globalThis, "HTMLOutputElement", {
    value: win.HTMLOutputElement,
    configurable: true
  });
  return dom;
}
