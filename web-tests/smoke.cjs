const { chromium } = require("playwright");
const http = require("node:http");
const fs = require("node:fs");
const path = require("node:path");
const os = require("node:os");
const assert = require("node:assert/strict");
const root = path.resolve(__dirname, "../internal/web/assets");
const output =
  process.env.ONIBI_SCREENSHOT_DIR ||
  fs.mkdtempSync(path.join(os.tmpdir(), "onibi-browser-"));
fs.mkdirSync(output, { recursive: true });
const baseTargets = [
  {
    id: "surface-work",
    windowId: "window-main",
    windowTitle: "Development",
    tabId: "tab-work",
    tabTitle: "onibi",
    title: "onibi · shell",
    cwd: "~/code/onibi",
    focused: true,
  },
  {
    id: "surface-logs",
    windowId: "window-main",
    windowTitle: "Development",
    tabId: "tab-work",
    tabTitle: "onibi",
    title: "server · logs",
    cwd: "~/code/onibi",
    focused: false,
  },
  {
    id: "surface-notes",
    windowId: "window-notes",
    windowTitle: "Personal",
    tabId: "tab-notes",
    tabTitle: "Notes",
    title: "notes · shell",
    cwd: "~/notes",
    focused: false,
  },
];
let targets = structuredClone(baseTargets),
  authorized = false,
  focused = "surface-work",
  mode = "normal",
  receiptState = "completed",
  unavailable = false,
  snapshotUnavailable = false;
const actions = [],
  requests = [],
  receipts = new Map();
const text =
  '$ go test ./...\nok  github.com/gongahkia/onibi/internal/ghostty\nok  github.com/gongahkia/onibi/internal/web\n\n$ git status --short\n M internal/web/assets/app.js\n\n<img src=x onerror="window.injected=true">\n\n$ _';
const json = (res, status, body) => {
  res.writeHead(status, {
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
  });
  res.end(JSON.stringify(body));
};
const server = http.createServer(async (req, res) => {
  requests.push({
    method: req.method,
    url: req.url,
    referer: req.headers.referer,
  });
  let body = "";
  for await (const chunk of req) body += chunk;
  if (req.url === "/api/pair") {
    const data = JSON.parse(body);
    assert.equal(data.token, "one-time-secret");
    authorized = true;
    return json(res, 200, {
      csrf: "csrf-secret",
      expiresAt: new Date(Date.now() + 3600000).toISOString(),
    });
  }
  if (req.url === "/api/session")
    return json(
      res,
      authorized ? 200 : 401,
      authorized
        ? {
            csrf: "csrf-secret",
            expiresAt: new Date(Date.now() + 3600000).toISOString(),
          }
        : { error: "Not paired" },
    );
  if (req.url === "/api/logout") {
    authorized = false;
    return json(res, 200, { ok: true });
  }
  if (req.url === "/api/targets")
    return json(
      res,
      unavailable ? 503 : 200,
      unavailable
        ? { error: "Ghostty is unavailable.", code: "ghostty_unavailable" }
        : { targets },
    );
  if (req.url.startsWith("/api/snapshot")) {
    if (snapshotUnavailable)
      return json(res, 503, {
        error: "Enable Accessibility on your Mac to read output.",
        code: "snapshot_unavailable",
      });
    const id = new URL(req.url, "http://localhost").searchParams.get(
      "targetId",
    );
    if (id !== focused)
      return json(res, 409, {
        error: "Focus this exact terminal to read its visible output.",
        code: "focus_required",
      });
    return json(res, 200, {
      targetId: id,
      text,
      capturedAt: new Date().toISOString(),
    });
  }
  if (req.url === "/api/actions") {
    assert.equal(req.headers["x-onibi-csrf"], "csrf-secret");
    const data = JSON.parse(body);
    if (receipts.has(data.id)) return json(res, 200, receipts.get(data.id));
    actions.push(data);
    let result = { message: "Action confirmed on your Mac." };
    if (data.action.kind === "focus") {
      focused = data.action.targetId;
      targets = targets.map((t) => ({ ...t, focused: t.id === focused }));
    }
    if (data.action.kind === "new_tab") {
      const t = {
        id: "surface-new",
        windowId: data.action.windowId,
        windowTitle: "Personal",
        tabId: "tab-new",
        tabTitle: "New tab",
        title: "New shell",
        cwd: "~",
        focused: true,
      };
      targets.push(t);
      result.targetId = t.id;
      focused = t.id;
    }
    const receipt = { id: data.id, state: receiptState, result };
    if (receiptState === "uncertain")
      receipt.error = "Native action could not be confirmed.";
    receipts.set(data.id, receipt);
    if (mode === "lost-post") {
      req.socket.destroy();
      return;
    }
    return json(res, 200, receipt);
  }
  if (req.url.startsWith("/api/actions/")) {
    const receipt = receipts.get(req.url.split("/").pop());
    return json(
      res,
      receipt ? 200 : 404,
      receipt || { error: "Receipt unavailable" },
    );
  }
  const name = req.url === "/" ? "index.html" : req.url.slice(1).split("?")[0];
  const file = path.join(root, name);
  if (!file.startsWith(root) || !fs.existsSync(file)) {
    res.writeHead(404);
    return res.end();
  }
  const types = {
    ".html": "text/html",
    ".js": "text/javascript",
    ".css": "text/css",
    ".svg": "image/svg+xml",
    ".webmanifest": "application/manifest+json",
  };
  res.writeHead(200, {
    "Content-Type": types[path.extname(file)] || "text/plain",
    "Content-Security-Policy":
      "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
  });
  fs.createReadStream(file).pipe(res);
});
(async () => {
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  const url = `http://127.0.0.1:${server.address().port}`;
  const browser = await chromium.launch({
    ...(process.env.ONIBI_CHROME_PATH
      ? { executablePath: process.env.ONIBI_CHROME_PATH }
      : {}),
    headless: true,
  });
  const page = await browser.newPage({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 1,
  });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(url);
  await page
    .locator("#pair-message")
    .filter({ hasText: "one-time pairing" })
    .waitFor();
  await page.screenshot({
    path: path.join(output, "pair-mobile.png"),
    fullPage: true,
  });
  await page.keyboard.press("Tab");
  assert.equal(await page.evaluate(() => document.activeElement.className), "skip-link");
  await page.keyboard.press("Enter");
  assert.equal(new URL(page.url()).hash, "#main");
  assert.equal(await page.evaluate(() => document.activeElement.id), "main");
  await page.goto(url + "/#token=one-time-secret");
  await page.locator("#workspace").waitFor({ state: "visible" });
  await page
    .locator("#target-list .target")
    .first()
    .waitFor({ state: "attached" });
  assert.equal(new URL(page.url()).hash, "");
  assert(requests.every((r) => !String(r.referer).includes("one-time-secret")));
  assert.equal(await page.locator("#compose").isDisabled(), true);
  assert.equal(actions.length, 0);
  await page.click("#open-picker");
  await page.locator("#picker-list .target").first().click();
  await page.locator("#terminal-output").waitFor({ state: "visible" });
  assert.equal(await page.evaluate(() => window.injected), undefined);
  assert.equal(await page.locator("#terminal-output img").count(), 0);
  await page.screenshot({
    path: path.join(output, "terminal-mobile.png"),
    fullPage: true,
  });
  await page.fill("#compose", "printf hello");
  await page.click("#send-input");
  await page.waitForFunction(
    () => document.getElementById("compose").value === "",
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "input",
    targetId: "surface-work",
    text: "printf hello",
    enter: true,
  });
  await page.fill("#compose", "  \n\t");
  await page.click("#paste-input");
  await page.waitForFunction(
    () => document.getElementById("compose").value === "",
  );
  assert.equal(actions.at(-1).action.enter, false);
  assert.equal(actions.at(-1).action.text, "  \n\t");
  const beforeOversize = actions.length;
  const oversize = "😀".repeat(5000);
  await page.fill("#compose", oversize);
  await page.click("#send-input");
  await page
    .locator("#workspace-message")
    .filter({ hasText: "16 KiB" })
    .waitFor();
  assert.equal(actions.length, beforeOversize);
  assert.equal(await page.inputValue("#compose"), oversize);
  console.log(
    "Passed pairing, explicit destinations, inert output, input fidelity, and UTF-8 limits.",
  );
  // Substitute the intermediary response at the fetch boundary. Chromium can
  // transparently retry a real HTTP 408 before exposing it to application code.
  await page.evaluate(() => {
    const originalFetch = window.fetch;
    window.fetch = async (...args) => {
      const response = await originalFetch(...args);
      if (args[0] === "/api/actions" && args[1]?.method === "POST") {
        window.fetch = originalFetch;
        return new Response(
          JSON.stringify({
            code: "proxy_timeout",
            error: "Proxy request timeout.",
          }),
          { status: 408, headers: { "Content-Type": "application/json" } },
        );
      }
      return response;
    };
  });
  const beforeProxy = actions.length;
  const beforeProxyRequests = requests.length;
  await page.fill("#compose", "printf proxy-recovery");
  await page.click("#send-input");
  await page.waitForFunction(
    () => document.getElementById("compose").value === "",
  );
  assert.equal(actions.length, beforeProxy + 1);
  assert.equal(actions.at(-1).action.text, "printf proxy-recovery");
  const proxyRequests = requests.slice(beforeProxyRequests);
  assert.equal(
    proxyRequests.filter(
      (request) => request.method === "POST" && request.url === "/api/actions",
    ).length,
    1,
  );
  assert(
    proxyRequests.some(
      (request) =>
        request.method === "GET" &&
        request.url === "/api/actions/" + actions.at(-1).id,
    ),
  );
  assert.equal(await page.textContent("#action-title"), "Done");
  mode = "lost-post";
  receiptState = "uncertain";
  const before = actions.length;
  await page.fill("#compose", "make release");
  await page.click("#send-input");
  await page
    .locator("#action-title")
    .filter({ hasText: "Delivery needs checking" })
    .waitFor();
  assert.equal(actions.length, before + 1);
  assert.equal(await page.inputValue("#compose"), "make release");
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  await page.click("#check-receipt");
  await page.waitForTimeout(250);
  assert.equal(actions.length, before + 1);
  await page.screenshot({
    path: path.join(output, "uncertain-mobile.png"),
    fullPage: true,
  });
  const saved = await page.evaluate(() =>
    sessionStorage.getItem("onibi.pending-action.v1"),
  );
  assert(saved.includes(actions.at(-1).id));
  assert(!saved.includes("make release"));
  assert(!saved.includes("csrf-secret"));
  assert(!saved.includes("one-time-secret"));
  await page.reload();
  await page
    .locator("#action-title")
    .filter({ hasText: "Delivery needs checking" })
    .waitFor();
  assert.equal(actions.length, before + 1);
  assert.equal(await page.inputValue("#compose"), "");
  await page.click("#open-picker");
  await page.locator("#picker-list .target").first().click();
  await page.fill("#compose", "make release");
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  mode = "normal";
  receiptState = "completed";
  await page.click("#acknowledge-action");
  await page.waitForFunction(
    () => !document.getElementById("send-input").disabled,
  );
  console.log("Passed uncertain receipt recovery, including reload without replay or stored input.");
  await page.click("#open-picker");
  await page.locator("#picker-list .target").nth(1).click();
  await page.locator("#focus-terminal").waitFor({ state: "visible" });
  await page.click("#focus-terminal");
  await page.locator("#terminal-output").waitFor({ state: "visible" });
  assert.equal(actions.at(-1).action.kind, "focus");
  assert.equal(actions.at(-1).action.targetId, "surface-logs");
  snapshotUnavailable = true;
  await page.click("#read-output");
  await page
    .locator("#snapshot-state")
    .filter({ hasText: "not live" })
    .waitFor();
  assert.equal(await page.textContent("#connection-label"), "Connected");
  assert.equal(await page.locator("#send-input").isDisabled(), false);
  await page.screenshot({
    path: path.join(output, "capture-unavailable-mobile.png"),
    fullPage: true,
  });
  snapshotUnavailable = false;
  await page.locator('[data-key="ctrl-c"]').click();
  await page.waitForFunction(
    () => document.getElementById("action-title").textContent === "Done",
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "key",
    targetId: "surface-logs",
    key: "ctrl-c",
  });
  await page.click("#close-tab");
  assert((await page.textContent("#close-description")).includes("2 panes"));
  assert.equal(await page.locator("#close-pane-list li").count(), 2);
  const closeBefore = actions.length;
  await page.click("#cancel-close");
  assert.equal(actions.length, closeBefore);
  await page.click("#close-tab");
  await page.click("#confirm-close");
  await page.waitForFunction(
    () => document.getElementById("action-title").textContent === "Done",
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "close_tab",
    windowId: "window-main",
    tabId: "tab-work",
  });
  await page.fill("#compose", "preserve draft");
  targets = targets.filter((t) => t.id !== "surface-logs");
  await page.click("#open-picker");
  await page.click("#refresh-picker");
  await page.waitForFunction(
    () =>
      document.getElementById("destination-title").textContent ===
      "Choose a terminal",
  );
  assert.equal(await page.inputValue("#compose"), "preserve draft");
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  if (!(await page.locator("#target-picker").isVisible()))
    await page.click("#open-picker");
  await page.locator("#picker-list .target").last().click();
  await page.click("#new-tab");
  await page.waitForFunction(
    () =>
      document.getElementById("destination-title").textContent === "New shell",
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "new_tab",
    windowId: "window-notes",
  });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.screenshot({
    path: path.join(output, "terminal-desktop.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 320, height: 760 });
  assert.equal(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
    true,
  );
  await page.screenshot({
    path: path.join(output, "terminal-small.png"),
    fullPage: true,
  });
  unavailable = true;
  await page.click("#open-picker");
  await page.click("#refresh-picker");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent ===
      "Disconnected",
  );
  await page.click("#close-picker");
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  assert.equal(await page.inputValue("#compose"), "preserve draft");
  unavailable = false;
  await page.click("#open-picker");
  await page.click("#refresh-picker");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent === "Connected",
  );
  await page.click("#close-picker");
  await page.evaluate(() => {
    Storage.prototype.setItem = function () {
      throw new Error("Storage blocked");
    };
  });
  await page.fill("#compose", "printf storage-independent");
  await page.click("#send-input");
  await page.waitForFunction(
    () => document.getElementById("compose").value === "",
  );
  await page.emulateMedia({ reducedMotion: "reduce" });
  assert.equal(
    await page
      .locator("#send-input")
      .evaluate((el) => getComputedStyle(el).transitionDuration),
    "0s",
  );
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  assert.equal(await page.evaluate(() => sessionStorage.length), 0);
  assert.deepEqual(errors, []);
  await page.click("#logout");
  await page.locator("#pair-view").waitFor({ state: "visible" });
  assert.equal(await page.inputValue("#compose"), "");
  console.log(
    JSON.stringify({
      passed: true,
      assertions: [
        "fragment removal",
        "explicit selection",
        "inert terminal output",
        "send and paste fidelity",
        "UTF-8 input limit",
        "CSRF header",
        "proxy 408 receipt recovery without replay",
        "uncertain receipt recovery without replay",
        "reload receipt recovery",
        "no stored credentials or input",
        "storage unavailable",
        "focus required",
        "capture-independent control",
        "whole-tab confirmation",
        "removed target",
        "new tab explicit window",
        "mobile overflow",
        "offline draft preservation",
        "reduced motion",
        "logout",
        "no JS errors",
      ],
      screenshots: output,
      actions: actions.length,
    }),
  );
  await browser.close();
  server.close();
})().catch((error) => {
  console.error(error);
  server.close();
  process.exit(1);
});
