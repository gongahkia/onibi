import { JSDOM } from "jsdom";

test("terminal status renders loading and recovery", async () => {
  const dom = installDOM('<div id="status"></div>');
  const { TerminalStatus } = await import("../terminal-status");
  const root = document.getElementById("status");
  if (root === null) {
    throw new Error("missing status root");
  }
  const status = new TerminalStatus();
  root.append(status.element);
  expect(root.textContent).toContain("Connecting terminal...");
  status.setConnected();
  expect(root.textContent).toContain("Terminal live");
  status.setDisconnected();
  expect(root.textContent).toContain("Disconnected. Retrying...");
  status.setRecovered({ mode: "snapshot", replay_bytes: 12 });
  expect(root.textContent).toContain("Recovered buffered output");
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
