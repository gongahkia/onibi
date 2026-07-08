import { JSDOM } from "jsdom";
import type { ShareViewer } from "../share";

test("share panel creates QR link and revokes active viewer", async () => {
  const dom = installDOM();
  const { SharePanel } = await import("../share");
  const posts: Array<{ path: string; body: Record<string, unknown> }> = [];
  let viewers: ShareViewer[] = [viewer()];
  const panel = new SharePanel(
    document.body,
    "s1",
    async () => ({ viewers }),
    async (path, body) => {
      posts.push({ path, body });
      if (path === "/share/revoke") {
        viewers = [];
        return jsonResponse({ ok: true });
      }
      return jsonResponse({
        url: "https://phone.local/pair/token#/s/s1",
        qr_png_data: "data:image/png;base64,abc",
        session_id: "s1",
        role: "viewer",
        expires_at: "2026-07-08T01:00:00Z",
        ttl: "30m0s",
        max_viewers: 1
      });
    },
    () => {}
  );
  panel.open();
  await settle();
  expect(document.body.textContent).toContain("viewer-phone");
  submit(dom, document.querySelector("form"));
  await settle();
  expect(posts[0]).toEqual({
    path: "/share",
    body: { session_id: "s1", ttl: "30m", max_viewers: 1 }
  });
  await waitFor(() => document.querySelector(".share-qr") !== null);
  expect(document.querySelector<HTMLImageElement>(".share-qr")?.src).toBe(
    "data:image/png;base64,abc"
  );
  expect(document.querySelector<HTMLInputElement>(".share-url")?.value).toBe(
    "https://phone.local/pair/token#/s/s1"
  );
  click(dom, button("Revoke"));
  await settle();
  expect(posts[1]).toEqual({
    path: "/share/revoke",
    body: { session_id: "s1", viewer_id: "viewer-1234567890" }
  });
  await waitFor(() => document.body.textContent?.includes("no active viewers") === true);
  expect(document.body.textContent).toContain("no active viewers");
  dom.window.close();
});

function viewer(): ShareViewer {
  return {
    id: "viewer-1234567890",
    label: "viewer-phone",
    created_at: "2026-07-08T00:00:00Z",
    last_seen_at: "2026-07-08T00:01:00Z",
    expires_at: "2026-07-08T01:00:00Z"
  };
}

function installDOM(): JSDOM {
  const dom = new JSDOM("<!doctype html><body></body>", { url: "https://onibi.test/s/s1" });
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

function jsonResponse(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200 });
}

function submit(dom: JSDOM, form: Element | null): void {
  if (!(form instanceof dom.window.HTMLFormElement)) {
    throw new Error("missing form");
  }
  form.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true }));
}

function click(dom: JSDOM, el: Element): void {
  el.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
}

function button(label: string): HTMLButtonElement {
  const found = Array.from(document.querySelectorAll("button")).find(
    (el) => el.textContent === label
  );
  if (!(found instanceof HTMLButtonElement)) {
    throw new Error(`missing ${label} button`);
  }
  return found;
}

async function settle(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

async function waitFor(done: () => boolean): Promise<void> {
  for (let i = 0; i < 10; i += 1) {
    if (done()) {
      return;
    }
    await settle();
  }
  throw new Error("condition not met");
}
