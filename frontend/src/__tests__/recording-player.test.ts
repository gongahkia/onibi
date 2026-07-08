import { JSDOM } from "jsdom";
import type { Options, Player, Source } from "asciinema-player";
import type { RecordingItem } from "../recording-player";

test("renders recordings, mounts player with speed, and copies transcript", async () => {
  const dom = installDOM('<main id="recordings"></main>');
  const { RecordingPlayerPanel } = await import("../recording-player");
  const root = requireRoot();
  const copied: string[] = [];
  const toasts: string[] = [];
  const calls: Array<{ src: Source; options?: Options }> = [];
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText: async (text: string) => copied.push(text) },
    configurable: true
  });
  const panel = new RecordingPlayerPanel(
    root,
    async () => ({ recordings: [recording()] }),
    async () => cast(),
    (message) => toasts.push(message),
    async () => (src, _container, options) => {
      calls.push({ src, options });
      return fakePlayer();
    }
  );
  panel.toggle();
  await settle();
  expect(root.querySelector(".recording-row")?.textContent).toContain("s1");
  click(dom, root.querySelector(".recording-row"));
  await settle();
  expect(calls).toHaveLength(1);
  expect(calls[0].options?.speed).toBe(1);
  const speed = root.querySelector(".recording-speed");
  if (!(speed instanceof dom.window.HTMLSelectElement)) {
    throw new Error("missing speed select");
  }
  speed.value = "2";
  speed.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  await settle();
  expect(calls).toHaveLength(2);
  expect(calls[1].options?.speed).toBe(2);
  click(dom, button(root, "Copy transcript"));
  await settle();
  expect(copied).toEqual(["hello\nworld"]);
  expect(toasts).toContain("Transcript copied.");
  dom.window.close();
});

test("extracts plain transcript from asciicast output events", async () => {
  const { transcriptFromCast } = await import("../recording-player");
  expect(transcriptFromCast(cast())).toBe("hello\nworld");
});

function recording(): RecordingItem {
  return {
    id: "s1",
    session_id: "s1",
    name: "s1.cast",
    created_at: "2026-07-08T00:00:00Z",
    duration_seconds: 1.5,
    size_bytes: 128,
    url: "/recordings/s1.cast"
  };
}

function cast(): string {
  return [
    '{"version":2,"width":80,"height":24,"timestamp":200,"title":"s1"}',
    '[0.1,"o","\\u001b[31mhello\\u001b[0m\\r\\n"]',
    '[1.5,"o","world"]'
  ].join("\n");
}

function fakePlayer(): Player {
  return {
    el: document.createElement("div"),
    dispose() {},
    getCurrentTime: () => 0,
    getDuration: () => 1.5,
    play: async () => {},
    pause: async () => {},
    seek: async () => {},
    addEventListener: () => {}
  };
}

function requireRoot(): HTMLElement {
  const root = document.getElementById("recordings");
  if (root === null) {
    throw new Error("missing recordings root");
  }
  return root;
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
  return dom;
}

function click(dom: JSDOM, el: Element | null): void {
  if (!(el instanceof dom.window.HTMLElement)) {
    throw new Error("missing element");
  }
  el.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
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
