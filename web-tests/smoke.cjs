const { chromium, webkit } = require("playwright");
const AxeBuilder = require("@axe-core/playwright").default;
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
  snapshotUnavailable = false,
  snapshotDelay = 0,
  snapshotActive = 0,
  snapshotPeak = 0;
const actions = [],
  requests = [],
  receipts = new Map();
const controls = [
  ["zoom", "Zoom / restore pane", "Layout"],
  ["equalize", "Balance splits", "Layout"],
  ["resize_left", "Resize left", "Layout"],
  ["resize_right", "Resize right", "Layout"],
  ["resize_up", "Resize up", "Layout"],
  ["resize_down", "Resize down", "Layout"],
  ["scroll_top", "Oldest output", "Scrollback"],
  ["scroll_up", "Page up", "Scrollback"],
  ["scroll_down", "Page down", "Scrollback"],
  ["scroll_bottom", "Latest output", "Scrollback"],
  ["prompt_previous", "Previous prompt", "Scrollback"],
  ["prompt_next", "Next prompt", "Scrollback"],
  ["font_increase", "Larger on Mac", "Mac display"],
  ["font_decrease", "Smaller on Mac", "Mac display"],
  ["font_reset", "Reset Mac font", "Mac display"],
  ["fullscreen", "Toggle fullscreen", "Mac display"],
].map(([id, label, group]) => ({ id, label, group }));
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
  if (req.url === "/api/health")
    return json(res, 200, {
      diagnostics: {
        capabilities: {
          version: "1.3.1",
          known: true,
          control: true,
          rename: true,
        },
        checks: [
          {
            id: "process",
            state: "running",
            detail:
              "One Ghostty process found; responsiveness has not been tested.",
          },
          {
            id: "capture",
            state: snapshotUnavailable ? "capture_permission" : "ready",
            detail: "Capture permission status.",
          },
        ],
      },
      inventory: {
        state: unavailable ? "unavailable" : "ready",
        durationMs: 15,
      },
    });
  if (req.url === "/api/targets")
    return json(
      res,
      unavailable ? 503 : 200,
      unavailable
        ? { error: "Ghostty is unavailable.", code: "ghostty_unavailable" }
        : {
            targets,
            controls,
            capabilities: {
              version: "1.3.1",
              known: true,
              control: true,
              rename: true,
            },
          },
    );
  if (req.url.startsWith("/api/snapshot")) {
    snapshotActive++;
    snapshotPeak = Math.max(snapshotPeak, snapshotActive);
    res.on("close", () => snapshotActive--);
    if (snapshotDelay) await new Promise((r) => setTimeout(r, snapshotDelay));
    if (res.destroyed) return;
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
    if (["new_tab", "new_window", "split"].includes(data.action.kind)) {
      const t = {
        id: "surface-new-" + actions.length,
        windowId:
          data.action.windowId ||
          (data.action.kind === "split"
            ? targets.find((t) => t.id === data.action.targetId).windowId
            : "window-created"),
        windowTitle: "Personal",
        tabId:
          data.action.kind === "split"
            ? targets.find((t) => t.id === data.action.targetId).tabId
            : "tab-new-" + actions.length,
        tabTitle: "New tab",
        title: "New shell",
        cwd: "~",
        focused: true,
      };
      targets = targets.map((t) => ({ ...t, focused: false }));
      targets.push(t);
      result.targetId = t.id;
      focused = t.id;
    }
    if (data.action.kind.startsWith("rename_"))
      targets = targets.map((t) =>
        t.id === data.action.targetId
          ? {
              ...t,
              [data.action.kind === "rename_tab" ? "tabTitle" : "title"]:
                data.action.name,
            }
          : t,
      );
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
  const engine = process.env.ONIBI_BROWSER || "chromium";
  assert(["chromium", "webkit"].includes(engine));
  const browser = await { chromium, webkit }[engine].launch({
    ...(engine === "chromium" && process.env.ONIBI_CHROME_PATH
      ? { executablePath: process.env.ONIBI_CHROME_PATH }
      : {}),
    headless: true,
  });
  const context = await browser.newContext({
    viewport: { width: 390, height: 844 },
    deviceScaleFactor: 1,
  });
  const page = await context.newPage();
  const auditAccessibility = async (label) => {
    const { violations } = await new AxeBuilder({ page })
      .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
      .analyze();
    assert.deepEqual(
      violations.map((v) => ({
        rule: v.id,
        nodes: v.nodes.map((n) => ({
          target: n.target,
          issue: n.failureSummary,
        })),
      })),
      [],
      label,
    );
  };
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(url);
  await page
    .locator("#pair-message")
    .filter({ hasText: "one-time pairing" })
    .waitFor();
  await auditAccessibility("Pairing accessibility");
  await page.screenshot({
    path: path.join(output, "pair-mobile.png"),
    fullPage: true,
  });
  // WebKit follows macOS full-keyboard-access preferences for link tabbing.
  if (engine === "webkit") await page.locator(".skip-link").focus();
  else await page.keyboard.press("Tab");
  assert.equal(
    await page.evaluate(() => document.activeElement.className),
    "skip-link",
  );
  await page.keyboard.press("Enter");
  assert.equal(new URL(page.url()).hash, "#main");
  assert.equal(await page.evaluate(() => document.activeElement.id), "main");
  await page.goto(url + "/#token=one-time-secret");
  await page.click("#pair-submit");
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
  await auditAccessibility("Terminal accessibility");
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
  assert.equal(await page.textContent("#action-title"), "Input delivered");
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
  console.log(
    "Passed uncertain receipt recovery, including reload without replay or stored input.",
  );
  await page.click("#open-picker");
  await page.locator("#picker-list .target").nth(1).click();
  await page.locator("#focus-terminal").waitFor({ state: "visible" });
  await page.click("#focus-terminal");
  await page.locator("#terminal-output").waitFor({ state: "visible" });
  assert.equal(actions.at(-1).action.kind, "focus");
  assert.equal(actions.at(-1).action.targetId, "surface-logs");
  await page.fill("#compose", "logs draft");
  snapshotUnavailable = true;
  await page.click("#read-output");
  await page
    .locator("#snapshot-state")
    .filter({ hasText: "not live" })
    .waitFor();
  assert.equal(
    await page.textContent("#connection-label"),
    "Output unavailable",
  );
  assert.equal(await page.locator("#workspace-message").isVisible(), false);
  assert.equal(await page.locator("#send-input").isDisabled(), false);
  await page.click("#connection");
  await page.locator("#health-checks li").first().waitFor();
  assert.match(
    await page.textContent("#health-observation"),
    /Ghostty 1.3.1.*15 ms/,
  );
  assert.match(
    await page.textContent("#session-expiry"),
    /Browser session expires/,
  );
  await auditAccessibility("Readiness accessibility");
  await page.screenshot({
    path: path.join(output, "readiness-mobile.png"),
    fullPage: true,
  });
  await page.click("#close-health");
  await page.screenshot({
    path: path.join(output, "capture-unavailable-mobile.png"),
    fullPage: true,
  });
  ((snapshotUnavailable = false),
    (snapshotDelay = 0),
    (snapshotActive = 0),
    (snapshotPeak = 0));
  const inventoryBeforeRetry = requests.filter(
    (r) => r.url === "/api/targets",
  ).length;
  await page.click("#retry-connection");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent === "Connected",
  );
  assert.equal(
    requests.filter((r) => r.url === "/api/targets").length,
    inventoryBeforeRetry,
  );
  await page.locator('.key-strip [data-key="ctrl-c"]').click();
  await page.waitForFunction(() =>
    ["Done", "Key delivered", "Close requested"].includes(
      document.getElementById("action-title").textContent,
    ),
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
  await page.waitForFunction(() =>
    ["Done", "Key delivered", "Close requested"].includes(
      document.getElementById("action-title").textContent,
    ),
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "close_tab",
    windowId: "window-main",
    tabId: "tab-work",
    expectedTargets: ["surface-logs", "surface-work"],
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
  assert.equal(await page.inputValue("#compose"), "");
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  if (!(await page.locator("#target-picker").isVisible()))
    await page.click("#open-picker");
  await page.locator("#picker-list .target").last().click();
  assert.equal(await page.inputValue("#compose"), "");
  await page.click("#new-tab");
  await page.waitForFunction(
    () =>
      document.getElementById("destination-title").textContent === "New shell",
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "new_tab",
    windowId: "window-notes",
  });
  await page.fill("#compose", "preserve draft");
  const createdId = actions.at(-1).id;
  // Drafts stay with their exact pane, including when visiting another shell.
  await page.click("#open-picker");
  await page.fill("#picker-search", "notes");
  assert.equal(await page.locator("#picker-list .target").count(), 1);
  await page.locator("#picker-list .target").click();
  assert.equal(await page.inputValue("#compose"), "");
  await page.fill("#compose", "notes draft");
  await page.click("#open-controls");
  assert.equal(await page.locator('[data-control="zoom"]').isDisabled(), true);
  assert.equal(
    await page.locator('[data-rename="rename_pane"]').isDisabled(),
    true,
  );
  await page.click("#close-controls");
  await page.click("#open-picker");
  await page.fill("#picker-search", "New shell");
  await page.locator("#picker-list .target").click();
  assert.equal(await page.inputValue("#compose"), "preserve draft");
  await page.click("#open-picker");
  await page.fill("#picker-search", "");
  await page.click("#close-picker");
  await page.click("#open-controls");
  await page.screenshot({
    path: path.join(output, "controls-mobile.png"),
    fullPage: false,
  });
  const selected = receipts.get(createdId).result.targetId;
  await page.locator('[data-control="zoom"]').click();
  await page.waitForFunction(
    () => !document.querySelector('[data-control="zoom"]').disabled,
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "control",
    targetId: selected,
    control: "zoom",
  });
  await page.locator('[data-rename="rename_pane"]').click();
  const title = 'Agent · review <literal> "quoted"';
  await page.fill("#rename-input", title);
  await page.locator('#rename-form button[type="submit"]').click();
  await page.waitForFunction(
    (t) => document.getElementById("destination-title").textContent === t,
    title,
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "rename_pane",
    targetId: selected,
    name: title,
  });
  await page.click("#open-keyboard");
  await page.screenshot({
    path: path.join(output, "keyboard-mobile.png"),
    fullPage: false,
  });
  await page.locator('[data-letter="d"]').click();
  await page.waitForFunction(
    () => !document.querySelector('[data-letter="d"]').disabled,
  );
  assert.deepEqual(actions.at(-1).action, {
    kind: "key",
    targetId: selected,
    key: "ctrl-d",
  });
  await page.locator('[data-modifier="meta"]').click();
  await page.locator('[data-letter="b"]').click();
  await page.waitForFunction(
    () => !document.querySelector('[data-letter="b"]').disabled,
  );
  assert.equal(actions.at(-1).action.key, "meta-b");
  await page.locator('[data-key="f12"]').click();
  await page.waitForFunction(
    () => !document.querySelector('[data-key="f12"]').disabled,
  );
  assert.equal(actions.at(-1).action.key, "f12");
  await page.click("#close-keyboard");
  // Slow snapshots must never overlap during Follow, and input cancels a read.
  await page.waitForFunction(
    () => !document.getElementById("read-output").disabled,
  );
  snapshotDelay = 300;
  snapshotPeak = 0;
  let snapshotBefore = requests.filter((r) =>
    r.url.startsWith("/api/snapshot"),
  ).length;
  await page.click("#follow-output");
  await page.waitForTimeout(3200);
  assert(
    requests.filter((r) => r.url.startsWith("/api/snapshot")).length >=
      snapshotBefore + 2,
  );
  assert.equal(snapshotPeak, 1);
  await page.click("#follow-output");
  await page.waitForFunction(
    () => !document.getElementById("read-output").disabled,
  );
  snapshotBefore = requests.filter((r) =>
    r.url.startsWith("/api/snapshot"),
  ).length;
  await page.waitForTimeout(2800);
  assert.equal(
    requests.filter((r) => r.url.startsWith("/api/snapshot")).length,
    snapshotBefore,
  );
  snapshotUnavailable = true;
  await page.click("#follow-output");
  await page.waitForFunction(
    () =>
      document.getElementById("follow-output").getAttribute("aria-pressed") ===
      "false",
  );
  snapshotUnavailable = false;
  snapshotDelay = 0;
  await page.click("#read-output");
  await page.waitForFunction(
    () => !document.getElementById("read-output").disabled,
  );
  // Losing the Mac connection through another endpoint must pause Follow too.
  await page.click("#follow-output");
  await page.waitForFunction(
    () => !document.getElementById("read-output").disabled,
  );
  const loseHealth = (route) => route.abort("connectionfailed");
  await page.route("**/api/health", loseHealth);
  await page.click("#connection");
  await page
    .locator("#health-error")
    .filter({ hasText: "interrupted" })
    .waitFor();
  assert.equal(
    await page.locator("#follow-output").getAttribute("aria-pressed"),
    "false",
  );
  await page.unroute("**/api/health", loseHealth);
  await page.click("#close-health");
  await page.click("#open-picker");
  await page.click("#refresh-picker");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent === "Connected",
  );
  await page.click("#close-picker");
  console.log(
    "Passed pane drafts, search, focus guards, literal titles, expanded keys, and bounded Follow.",
  );
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
  // Small-screen sheets retain 44px touch targets and never overflow horizontally.
  for (const [opener, dialog, closer] of [
    ["open-picker", "target-picker", "close-picker"],
    ["open-controls", "controls-dialog", "close-controls"],
    ["open-keyboard", "keyboard-dialog", "close-keyboard"],
    ["connection", "health-dialog", "close-health"],
  ]) {
    await page.click("#" + opener);
    assert.equal(
      await page
        .locator("#" + dialog)
        .evaluate((el) => el.scrollWidth <= el.clientWidth),
      true,
    );
    const small = await page
      .locator("#" + dialog + " button")
      .evaluateAll((els) =>
        els
          .filter((el) => el.getClientRects().length)
          .filter(
            (el) =>
              el.getBoundingClientRect().width < 43.9 ||
              el.getBoundingClientRect().height < 43.9,
          )
          .map((el) => el.textContent),
      );
    assert.deepEqual(small, []);
    await auditAccessibility(dialog + " accessibility");
    await page.click("#" + closer);
  }
  await page.setViewportSize({ width: 844, height: 390 });
  await page.click("#open-keyboard");
  assert.equal(
    await page
      .locator("#keyboard-dialog")
      .evaluate((el) => el.scrollWidth <= el.clientWidth),
    true,
  );
  await page.locator('[data-key="f12"]').scrollIntoViewIfNeeded();
  assert.equal(await page.locator("#close-keyboard").isVisible(), true);
  await page.click("#close-keyboard");
  await page.screenshot({
    path: path.join(output, "terminal-landscape.png"),
    fullPage: true,
  });
  await page.setViewportSize({ width: 320, height: 760 });
  unavailable = true;
  await page.click("#open-picker");
  await page.click("#refresh-picker");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent ===
      "Ghostty unavailable",
  );
  await page.click("#close-picker");
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  assert.equal(await page.inputValue("#compose"), "preserve draft");
  // A successful capture and a healthy HTTP endpoint must not erase a failed
  // native inventory observation or enable terminal input.
  await page.click("#read-output");
  await page.waitForFunction(
    () => !document.getElementById("read-output").disabled,
  );
  await page.click("#connection");
  await page
    .locator("#health-observation")
    .filter({ hasText: "unavailable" })
    .waitFor();
  assert.equal(
    await page.textContent("#connection-label"),
    "Ghostty unavailable",
  );
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  await page.click("#close-health");
  unavailable = false;
  await page.click("#open-picker");
  await page.click("#refresh-picker");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent === "Connected",
  );
  await page.click("#close-picker");
  // Creation works with no existing windows. Close scopes name only what the user confirmed.
  targets = [];
  await page.click("#open-picker");
  await page.click("#refresh-picker");
  await page.waitForFunction(
    () =>
      document.getElementById("destination-title").textContent ===
      "Choose a terminal",
  );
  await page.click("#picker-new-window");
  await page.waitForFunction(
    () =>
      document.getElementById("destination-title").textContent === "New shell",
  );
  assert.deepEqual(actions.at(-1).action, { kind: "new_window" });
  let pane = receipts.get(actions.at(-1).id).result.targetId;
  for (const direction of ["left", "right", "up", "down"]) {
    await page.click("#open-controls");
    await page.locator(`[data-split="${direction}"]`).click();
    await page.waitForFunction(
      () => !document.getElementById("split-right").disabled,
    );
    assert.deepEqual(actions.at(-1).action, {
      kind: "split",
      targetId: pane,
      direction,
    });
    pane = receipts.get(actions.at(-1).id).result.targetId;
  }
  for (const kind of ["close_pane", "close_window"]) {
    await page.click("#open-controls");
    await page.locator(`[data-close="${kind}"]`).click();
    assert.equal(
      await page.locator("#close-pane-list li").count(),
      kind === "close_pane" ? 1 : 5,
    );
    assert.equal(
      await page.evaluate(() => document.activeElement.id),
      "cancel-close",
    );
    await page.click("#confirm-close");
    await page.waitForFunction(
      () => !document.getElementById("split-right").disabled,
    );
    assert.deepEqual(
      actions.at(-1).action,
      kind === "close_pane"
        ? { kind, targetId: pane, expectedTargets: [pane] }
        : {
            kind,
            windowId: "window-created",
            expectedTargets: targets
              .filter((t) => t.windowId === "window-created")
              .map((t) => t.id)
              .sort(),
          },
    );
  }
  // A canceled slow read cannot replace a newly selected pane's output.
  snapshotDelay = 1000;
  await page.click("#read-output");
  await page.click("#open-picker");
  await page.locator("#picker-list .target").first().click();
  await page.waitForTimeout(1200);
  assert.equal(await page.locator("#terminal-output").isVisible(), false);
  snapshotDelay = 0;
  await page.click("#focus-terminal");
  await page.waitForFunction(
    () => !document.getElementById("split-right").disabled,
  );
  console.log(
    "Passed empty inventory creation, four split directions, close scopes, and canceled stale reads.",
  );
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
      engine,
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
        "pane-specific in-memory drafts",
        "search and focus guards",
        "literal renaming and Ctrl/Alt/function keys",
        "non-overlapping Follow and error pause",
        "new window with empty inventory",
        "four split directions and close scopes",
        "canceled stale output",
        "44px sheet controls and landscape layout",
        "WCAG A/AA automated checks",
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
