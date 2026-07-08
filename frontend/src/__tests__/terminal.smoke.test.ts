import { JSDOM } from "jsdom";

test("mounts xterm and renders written bytes", async () => {
  const dom = installDOM('<div id="term"></div>');
  const { createTerminal } = await import("../terminal");
  const root = document.getElementById("term");
  if (root === null) {
    throw new Error("missing terminal root");
  }
  const { term } = createTerminal(root);
  await writeTerminal(term, new TextEncoder().encode("onibi ready\r\n$ pwd"));
  expect(root.querySelector(".xterm")).not.toBeNull();
  expect(term.buffer.active.getLine(0)?.translateToString(true)).toContain("onibi ready");
  expect(term.buffer.active.getLine(1)?.translateToString(true)).toContain("$ pwd");
  term.dispose();
  dom.window.close();
});

function installDOM(markup: string): JSDOM {
  const dom = new JSDOM(markup, { url: "https://onibi.test/s/abc" });
  const win = dom.window;
  Object.defineProperty(globalThis, "window", { value: win, configurable: true });
  Object.defineProperty(globalThis, "document", { value: win.document, configurable: true });
  Object.defineProperty(globalThis, "navigator", { value: win.navigator, configurable: true });
  Object.defineProperty(globalThis, "HTMLElement", { value: win.HTMLElement, configurable: true });
  Object.defineProperty(globalThis, "HTMLButtonElement", {
    value: win.HTMLButtonElement,
    configurable: true
  });
  Object.defineProperty(globalThis, "HTMLCanvasElement", {
    value: win.HTMLCanvasElement,
    configurable: true
  });
  Object.defineProperty(globalThis, "getComputedStyle", {
    value: win.getComputedStyle.bind(win),
    configurable: true
  });
  Object.defineProperty(win, "matchMedia", { value: matchMedia, configurable: true });
  Object.defineProperty(win, "requestAnimationFrame", {
    value: (callback: FrameRequestCallback) => win.setTimeout(() => callback(Date.now()), 0),
    configurable: true
  });
  Object.defineProperty(win, "cancelAnimationFrame", {
    value: (id: number) => win.clearTimeout(id),
    configurable: true
  });
  Object.defineProperty(win.HTMLElement.prototype, "clientWidth", {
    get: () => 800,
    configurable: true
  });
  Object.defineProperty(win.HTMLElement.prototype, "clientHeight", {
    get: () => 240,
    configurable: true
  });
  Object.defineProperty(win.HTMLCanvasElement.prototype, "getContext", {
    value: canvasContext,
    configurable: true
  });
  return dom;
}

function matchMedia(query: string): MediaQueryList {
  return {
    matches: false,
    media: query,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent: () => false
  };
}

function canvasContext(): CanvasRenderingContext2D {
  return {
    canvas: document.createElement("canvas"),
    clearRect() {},
    fillRect() {},
    getImageData: () => ({ data: new Uint8ClampedArray(4) }),
    putImageData() {},
    createImageData: () => ({ data: new Uint8ClampedArray(4) }),
    drawImage() {},
    save() {},
    restore() {},
    scale() {},
    translate() {},
    beginPath() {},
    rect() {},
    clip() {},
    fillText() {},
    strokeText() {},
    measureText: () => ({ width: 8 }),
    setTransform() {},
    resetTransform() {},
    createLinearGradient: () => ({ addColorStop() {} })
  } as unknown as CanvasRenderingContext2D;
}

function writeTerminal(
  term: { write(data: Uint8Array, callback: () => void): void },
  data: Uint8Array
): Promise<void> {
  return new Promise((resolve) => term.write(data, resolve));
}
