import { JSDOM } from "jsdom";

test("intervention panel renders loading, pending failure, and confirmed input", async () => {
  const dom = installDOM('<main id="root"></main>');
  const { InterventionPanel } = await import("../intervention-panel");
  const root = document.getElementById("root");
  if (root === null) {
    throw new Error("missing root");
  }
  let resolvePost: ((response: Response) => void) | undefined;
  const posts: Array<Record<string, unknown>> = [];
  const panel = new InterventionPanel(
    root,
    "s1",
    async (_path, body) => {
      posts.push(body);
      return new Promise<Response>((resolve) => {
        resolvePost = resolve;
      });
    },
    async () => {
      throw new Error("offline");
    },
    () => {}
  );
  root.append(panel.element);
  panel.element.click();
  button(root, "Interrupt").click();
  expect(status(root)).toBe("Sending interrupt...");
  resolvePost?.(json({ ok: true, command_id: "c1", state: "pending" }));
  await waitFor(() => status(root) === "Interrupt awaits host acknowledgement.");
  expect(status(root)).toBe("Interrupt awaits host acknowledgement.");
  button(root, "Check status").click();
  await waitFor(() => status(root) === "Status unavailable. Check connection.");
  expect(status(root)).toBe("Status unavailable. Check connection.");

  const confirmedRoot = document.createElement("main");
  root.append(confirmedRoot);
  let response: Response | undefined;
  const confirmed = new InterventionPanel(
    confirmedRoot,
    "s1",
    async (_path, body) => {
      posts.push(body);
      return response ?? new Response("missing", { status: 500 });
    },
    async () => ({ ok: true, command_id: "c1", state: "succeeded" }),
    () => {}
  );
  confirmedRoot.append(confirmed.element);
  confirmed.element.click();
  const confirmedInput = confirmedRoot.querySelector("input");
  if (!(confirmedInput instanceof HTMLInputElement)) {
    throw new Error("missing confirmed input");
  }
  confirmedInput.value = "pwd";
  response = json({ ok: true, command_id: "c2", state: "succeeded" });
  buttonIn(confirmedRoot, "Send").click();
  await waitFor(() => status(confirmedRoot) === "Short input confirmed.");
  expect(status(confirmedRoot)).toBe("Short input confirmed.");
  expect(posts).toContainEqual({ session_id: "s1", action: "input", input: "pwd" });
  dom.window.close();
});

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
}

function status(root: ParentNode | null): string {
  return root?.querySelector(".intervention-status")?.textContent ?? "";
}

function button(root: ParentNode, label: string): HTMLButtonElement {
  return buttonIn(root, label);
}

function buttonIn(root: ParentNode | null, label: string): HTMLButtonElement {
  const found = Array.from(root?.querySelectorAll("button") ?? []).find(
    (el) => el.textContent === label
  );
  if (!(found instanceof HTMLButtonElement)) {
    throw new Error(`missing ${label} button`);
  }
  return found;
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
  Object.defineProperty(globalThis, "HTMLInputElement", {
    value: win.HTMLInputElement,
    configurable: true
  });
  return dom;
}
