import { JSDOM } from "jsdom";

test("emits terminal escape sequences from soft keys", async () => {
  const dom = installDOM('<div id="softkeys"></div>');
  const { SoftKeyBar } = await import("../softkeys");
  const root = document.getElementById("softkeys");
  if (root === null) {
    throw new Error("missing softkeys root");
  }
  const sent: number[][] = [];
  let pageUp = 0;
  let focused = 0;
  new SoftKeyBar({
    root,
    sendBytes: (data) => sent.push(Array.from(data)),
    sendText: () => {},
    pageUp: () => { pageUp += 1; },
    pageDown: () => {},
    focus: () => { focused += 1; },
    getTheme: () => "ghostty-default",
    setTheme: () => {},
    decreaseFontSize: () => {},
    increaseFontSize: () => {}
  });
  pointerDown(dom, button(root, "Esc"));
  pointerDown(dom, button(root, "Ctrl"));
  pointerDown(dom, button(root, "→"));
  pointerDown(dom, button(root, "Alt"));
  pointerDown(dom, button(root, "Tab"));
  pointerDown(dom, button(root, "PgUp"));
  expect(sent).toEqual([[0x1b], [0x1b, 0x5b, 0x31, 0x3b, 0x35, 0x43], [0x1b, 0x09]]);
  expect(pageUp).toBe(1);
  expect(focused).toBeGreaterThan(0);
  dom.window.close();
});

function installDOM(markup: string): JSDOM {
  const dom = new JSDOM(markup, { url: "https://onibi.test/" });
  const win = dom.window;
  Object.defineProperty(globalThis, "window", { value: win, configurable: true });
  Object.defineProperty(globalThis, "document", { value: win.document, configurable: true });
  Object.defineProperty(globalThis, "navigator", { value: win.navigator, configurable: true });
  Object.defineProperty(globalThis, "HTMLElement", { value: win.HTMLElement, configurable: true });
  Object.defineProperty(globalThis, "HTMLButtonElement", { value: win.HTMLButtonElement, configurable: true });
  Object.defineProperty(globalThis, "HTMLCanvasElement", { value: win.HTMLCanvasElement, configurable: true });
  Object.defineProperty(win.HTMLCanvasElement.prototype, "getContext", { value: canvasContext, configurable: true });
  Object.defineProperty(win.HTMLElement.prototype, "setPointerCapture", { value() {}, configurable: true });
  Object.defineProperty(win.HTMLElement.prototype, "releasePointerCapture", { value() {}, configurable: true });
  Object.defineProperty(win.navigator, "clipboard", { value: undefined, configurable: true });
  return dom;
}

function canvasContext(): CanvasRenderingContext2D {
  return {
    canvas: document.createElement("canvas"),
    measureText: () => ({ width: 8 })
  } as unknown as CanvasRenderingContext2D;
}

function pointerDown(dom: JSDOM, el: HTMLElement): void {
  const event = new dom.window.Event("pointerdown", { bubbles: true, cancelable: true });
  Object.defineProperty(event, "pointerId", { value: 1 });
  el.dispatchEvent(event);
}

function button(root: HTMLElement, label: string): HTMLButtonElement {
  const found = Array.from(root.querySelectorAll("button")).find((el) => el.textContent === label);
  if (!(found instanceof HTMLButtonElement)) {
    throw new Error(`missing ${label} button`);
  }
  return found;
}
