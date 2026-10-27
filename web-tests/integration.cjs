// Exercises production HTTP/authentication/assets/SQLite with a simulated
// Ghostty backend. The fixture exposes no test-control HTTP routes.
const { chromium, webkit } = require("playwright");
const AxeBuilder = require("@axe-core/playwright").default;
const assert = require("node:assert/strict");
const { spawn, execFileSync } = require("node:child_process");
const { once } = require("node:events");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const readline = require("node:readline");
const { randomUUID } = require("node:crypto");

const root = path.resolve(__dirname, "..");
const temp = fs.mkdtempSync(path.join(os.tmpdir(), "onibi-integration-"));
const binary = path.join(temp, "fixture");
const screenshotDir = process.env.ONIBI_SCREENSHOT_DIR || temp;
fs.mkdirSync(screenshotDir, { recursive: true });
let fixture,
  browser,
  sequence = 0;

async function start(port = 0) {
  const child = spawn(
    binary,
    ["--state", path.join(temp, "state"), "--port", String(port)],
    { stdio: ["pipe", "pipe", "pipe"] },
  );
  const waiters = new Map();
  let stderr = "";
  child.stderr.on("data", (chunk) => {
    stderr += chunk;
  });
  const exited = once(child, "exit");
  const ready = new Promise((resolve, reject) => {
    waiters.set(0, { resolve, reject });
  });
  const lines = readline.createInterface({ input: child.stdout });
  lines.on("line", (line) => {
    const data = JSON.parse(line);
    const id = data.ready ? 0 : data.id;
    waiters.get(id)?.resolve(data);
    waiters.delete(id);
  });
  child.on("error", (error) => {
    for (const waiter of waiters.values()) waiter.reject(error);
    waiters.clear();
  });
  child.on("exit", () => {
    for (const waiter of waiters.values())
      waiter.reject(new Error("Fixture exited: " + stderr));
    waiters.clear();
    lines.close();
  });
  const command = (data = {}) =>
    new Promise((resolve, reject) => {
      const id = ++sequence;
      waiters.set(id, { resolve, reject });
      child.stdin.write(JSON.stringify({ id, ...data }) + "\n");
    });
  const stop = async () => {
    if (child.exitCode === null && child.signalCode === null)
      child.kill("SIGKILL");
    await exited;
  };
  // Keep cleanup available even if startup fails.
  fixture = { command, stop };
  return (fixture = { command, stop, ...(await ready) });
}

async function eventually(read, predicate, label) {
  const end = Date.now() + 15000;
  while (Date.now() < end) {
    if (predicate(await read())) return;
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("Timed out: " + label);
}

async function api(page, route, body, csrf = true) {
  return page.evaluate(
    async ({ route, body, csrf }) => {
      const headers = {};
      if (body) {
        headers["Content-Type"] = "application/json";
        if (csrf)
          headers["X-Onibi-CSRF"] = (
            await (await fetch("/api/session")).json()
          ).csrf;
      }
      const response = await fetch(route, {
        method: body ? "POST" : "GET",
        headers,
        body: body ? JSON.stringify(body) : undefined,
      });
      return { status: response.status, body: await response.json() };
    },
    { route, body, csrf },
  );
}

async function selectWork(page) {
  await page.click("#open-picker");
  await page
    .locator("#picker-list .target")
    .filter({ hasText: "Work shell" })
    .click();
}

(async () => {
  execFileSync(
    "go",
    ["build", "-tags", "browserfixture", "-o", binary, "./web-tests/fixture"],
    { cwd: root, stdio: "pipe", env: { ...process.env, GOMAXPROCS: "2" } },
  );
  await start();
  const engine = process.env.ONIBI_BROWSER || "chromium";
  assert(["chromium", "webkit"].includes(engine));
  browser = await { chromium, webkit }[engine].launch({
    headless: true,
    ...(engine === "chromium" && process.env.ONIBI_CHROME_PATH
      ? { executablePath: process.env.ONIBI_CHROME_PATH }
      : {}),
  });
  const context = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: 390, height: 844 },
  });
  const page = await context.newPage();
  page.setDefaultTimeout(20000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  const response = await page.goto(fixture.pairingURL);
  await page.click("#pair-submit");
  assert.match(
    response.headers()["content-security-policy"],
    /default-src 'none'/,
  );
  assert.equal(response.headers()["cache-control"], "no-store");
  await page.locator("#workspace").waitFor({ state: "visible" });
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent === "Connected",
  );
  assert.equal(new URL(page.url()).hash, "");
  assert.equal(await page.evaluate(() => document.cookie), "");
  const beforeCapabilities = await fixture.command();
  const report = await api(page, "/api/capabilities");
  assert.equal(report.status, 200);
  assert.equal(report.body.nativeBackend, false);
  assert(
    report.body.features.filter((f) => f.implemented).every((f) => f.automated),
  );
  assert(report.body.features.every((f) => !f.observation));
  assert.equal(
    report.body.features.find((f) => f.id === "mouse").implemented,
    false,
  );
  await page.click("#connection");
  await page.click("#open-capabilities");
  await page
    .locator("#capability-summary")
    .filter({ hasText: "Simulated backend" })
    .waitFor();
  await page.getByText("Not implemented", { exact: true }).first().click();
  assert.match(
    await page.textContent("#capability-list"),
    /Agent task and approval state/,
  );
  assert.equal(
    (await fixture.command()).actions?.length || 0,
    beforeCapabilities.actions?.length || 0,
  );
  const capabilityAudit = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  assert.deepEqual(capabilityAudit.violations, [], "capability accessibility");
  await page.screenshot({
    path: path.join(screenshotDir, "capabilities.png"),
    fullPage: true,
  });
  await page.click("#close-capabilities");
  await page.click("#close-health");
  const cookies = await context.cookies();
  assert.equal(cookies.length, 1);
  assert.equal(cookies[0].name, "__Host-onibi_session");
  assert.equal(cookies[0].httpOnly, true);
  assert.equal(cookies[0].secure, true);
  assert.equal(cookies[0].sameSite, "Strict");
  const token = new URLSearchParams(
    new URL(fixture.pairingURL).hash.slice(1),
  ).get("token");
  assert.equal(
    (await api(page, "/api/pair", { token }, false)).status,
    401,
    "pairing token is single use",
  );
  const input = {
    kind: "input",
    targetId: "pane-work",
    text: "must not run",
    enter: true,
  };
  assert.equal(
    (
      await api(
        page,
        "/api/actions",
        { id: randomUUID(), action: input },
        false,
      )
    ).status,
    403,
  );
  const foreign = await context.request.post(fixture.url + "/api/actions", {
    headers: { Origin: "https://foreign.invalid" },
    data: { id: randomUUID(), action: input },
  });
  assert.equal(foreign.status(), 403);
  assert.equal((await foreign.json()).code, "invalid_origin");
  assert.equal((await fixture.command()).actions, null);
  await selectWork(page);
  await page.locator("#terminal-output").waitFor({ state: "visible" });
  assert.match(await page.textContent("#terminal-output"), /<img src=x/);
  assert.equal(await page.locator("#terminal-output img").count(), 0);
  const literal = 'printf "%s" "$(echo untouched)"\n🦊 <>&';
  await page.fill("#compose", literal);
  await page.click("#paste-input");
  await page.waitForFunction(
    () => document.getElementById("compose").value === "",
  );
  assert.deepEqual((await fixture.command()).actions.at(-1), {
    kind: "input",
    targetId: "pane-work",
    text: literal,
  });

  // Lose the HTTP response after execution, then recover its durable receipt.
  let lostID;
  const loseResponse = async (route) => {
    lostID = route.request().postDataJSON().id;
    await route.fetch();
    await route.abort("connectionfailed");
  };
  await page.route("**/api/actions", loseResponse);
  await page.fill("#compose", "response lost once");
  await page.click("#send-input");
  await page.waitForFunction(
    () => document.getElementById("compose").value === "",
  );
  await page.unroute("**/api/actions", loseResponse);
  let stats = await fixture.command();
  assert.equal(stats.actions.length, 2);
  assert.equal(stats.actions[1].enter, true);
  assert.equal(
    (await api(page, "/api/actions/" + lostID)).body.state,
    "completed",
  );
  const replay = await api(page, "/api/actions", {
    id: lostID,
    action: stats.actions[1],
  });
  assert.equal(replay.body.state, "completed");
  assert.equal((await fixture.command()).actions.length, 2);
  assert.equal(
    (await api(page, "/api/actions", { id: lostID, action: input })).status,
    409,
  );
  console.log(
    "Passed real HTTPS pairing, secure cookies, origin/CSRF, literal input, and lost-response receipt recovery.",
  );

  // Persistent sessions stay readable and controllable while Ghostty is down.
  await fixture.command({ unavailable: true });
  await page.click("#open-picker");
  await page.click("#refresh-picker");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent ===
      "Ghostty unavailable",
  );
  await page.waitForFunction(() => document.title.startsWith("(1) "));
  const sessionButton = page
    .locator("#picker-list .target")
    .filter({ hasText: "build" });
  assert.match(await sessionButton.textContent(), /Needs input/);
  await sessionButton.click();
  await page
    .locator("#terminal-output")
    .filter({ hasText: "200 requested" })
    .waitFor();
  assert.equal(await page.locator("#terminal-output img").count(), 0);
  assert.equal(
    await page.textContent("#terminal-output .ansi-fg-1"),
    "<img src=x onerror=alert(1)>",
  );
  assert.equal(
    await page
      .locator("#terminal-output .ansi-bold")
      .evaluate((el) => getComputedStyle(el).color),
    "rgb(255, 200, 0)",
  );
  assert.match(await page.textContent("#terminal-output"), /\[y\/N\]$/);
  assert.doesNotMatch(await page.textContent("#terminal-output"), /\x1b/);
  assert.equal(await page.textContent("#snapshot-kind"), "Scrollback + screen");
  assert.equal(await page.locator("#new-tab").isHidden(), true);
  assert.equal(await page.locator("#open-controls").isHidden(), true);
  assert.equal(await page.locator("#session-kill").isVisible(), true);
  const sessionAudit = await new AxeBuilder({ page })
    .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
    .analyze();
  assert.deepEqual(sessionAudit.violations, [], "session accessibility");
  await page.screenshot({
    path: path.join(screenshotDir, "session.png"),
    fullPage: true,
  });
  await page.click("#open-picker");
  await page.click("#picker-new-session");
  assert.deepEqual(
    await page.locator("#new-session-folders button").allTextContents(),
    ["/fixture/repo"],
  );
  assert.equal(await page.inputValue("#new-session-cwd"), "/fixture/repo");
  await page.screenshot({
    path: path.join(screenshotDir, "new-session-dialog.png"),
  });
  await page.click("#cancel-new-session");
  await page.click("#reader-history");
  await page
    .locator("#terminal-output")
    .filter({ hasText: "1000 requested" })
    .waitFor();
  await page.locator("#quick-replies").waitFor({ state: "visible" });
  assert.equal(await page.textContent("#reply-reason"), "Yes/no prompt");
  assert.deepEqual(
    await page.locator("#reply-list button span").allTextContents(),
    ["Yes", "No"],
  );
  await page.fill("#compose", "kept draft");
  await page.locator("#reply-list button", { hasText: "Yes" }).click();
  assert.equal(await page.locator("#quick-replies").isHidden(), true);
  await eventually(
    () => fixture.command(),
    (s) => s.sessionActions.length === 1,
    "quick reply",
  );
  assert.equal(await page.inputValue("#compose"), "kept draft");
  await page.fill("#compose", "");
  assert.deepEqual((await fixture.command()).sessionActions.at(-1), {
    kind: "input",
    sessionId: "0123456789abcdef",
    text: "y",
    enter: true,
  });
  await page.locator("#send-input").waitFor({ state: "attached" });
  await page.waitForFunction(
    () => !document.querySelector('.key-strip [data-key="ctrl-c"]').disabled,
  );
  await page.click('.key-strip [data-key="ctrl-c"]');
  await eventually(
    () => fixture.command(),
    (s) => s.sessionActions.at(-1).kind === "key",
    "session key",
  );
  assert.equal((await fixture.command()).sessionActions.at(-1).key, "ctrl-c");
  await page.waitForFunction(
    () => !document.getElementById("session-kill").disabled,
  );
  await page.click("#session-kill");
  await page.locator("#close-confirmation").waitFor({ state: "visible" });
  assert.match(await page.textContent("#close-title"), /End “build”/);
  await page.click("#confirm-close");
  await page
    .locator("#output-empty-title")
    .filter({ hasText: "That session has ended" })
    .waitFor();
  const afterKill = await fixture.command();
  assert.deepEqual(afterKill.sessions, []);
  assert.equal(afterKill.sessionActions.length, 3);
  assert.equal(afterKill.actions.length, 2, "no Ghostty action was sent");
  await page.click("#open-picker");
  await page.click("#picker-new-session");
  await page.locator("#new-session-dialog").waitFor({ state: "visible" });
  assert.equal(await page.inputValue("#new-session-command"), "shell");
  await page.fill("#new-session-command", "codex  --model gpt-5");
  await page.fill("#new-session-cwd", "/fixture/repo");
  await page.fill("#new-session-name", "review");
  await page.click("#start-session");
  await page
    .locator("#destination-title")
    .filter({ hasText: "review" })
    .waitFor();
  assert.deepEqual((await fixture.command()).sessionActions.at(-1), {
    kind: "new",
    sessionId: "",
    program: "codex",
    args: ["--model", "gpt-5"],
    cwd: "/fixture/repo",
    name: "review",
  });
  await page.screenshot({
    path: path.join(screenshotDir, "new-session.png"),
    fullPage: true,
  });
  await page.waitForFunction(() => !document.title.startsWith("("));
  await fixture.command({ unavailable: false, advance: 61 });
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent === "Connected",
    null,
    { timeout: 30000 },
  );
  await selectWork(page);
  await page.locator("#terminal-output").waitFor({ state: "visible" });
  console.log(
    "Passed persistent sessions without Ghostty: attention, unfocused history, input, keys, and confirmed kill.",
  );

  await fixture.command({ captureDenied: true });
  await page.click("#read-output");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent ===
      "Output unavailable",
  );
  await page.fill("#compose", "draft stays with work");
  assert.equal(await page.locator("#send-input").isEnabled(), true);
  const health = (await api(page, "/api/health")).body;
  assert.equal(health.inventory.state, "ready");
  assert.equal(health.capture.code, "capture_permission");
  console.log("Verified independent capture permission failure.");
  await fixture.command({ captureDenied: false, unavailable: true });
  // Ghostty fails between the last successful inventory and an input action.
  // Its rejected receipt must immediately reconcile readiness and keep the draft.
  await page.click("#send-input");
  await page
    .locator("#action-title")
    .filter({ hasText: "Action wasn’t sent" })
    .waitFor();
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent ===
      "Ghostty unavailable",
  );
  assert.equal((await fixture.command()).actions.length, 2);
  await page.click("#read-output");
  await page.waitForFunction(
    () => !document.getElementById("read-output").disabled,
  );
  assert.equal(
    await page.textContent("#connection-label"),
    "Ghostty unavailable",
  );
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  assert.equal(await page.inputValue("#compose"), "draft stays with work");
  stats = await fixture.command();
  assert.equal((await api(page, "/api/targets")).status, 503);
  assert.equal(
    (await fixture.command()).inventories,
    stats.inventories,
    "backoff avoids native polling",
  );
  await fixture.command({ unavailable: false, advance: 61, version: "1.3.0" });
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent === "Connected",
  );
  await page.click("#open-controls");
  assert.equal(
    await page.locator('[data-rename="rename_tab"]').isDisabled(),
    true,
  );
  assert.equal(await page.locator("#title-capability").isVisible(), true);
  await page.click("#close-controls");
  const denied = await api(page, "/api/actions", {
    id: randomUUID(),
    action: { kind: "rename_tab", targetId: "pane-work", name: "old version" },
  });
  assert.equal(denied.body.state, "rejected");
  assert.equal((await fixture.command()).actions.length, 2);
  await fixture.command({ version: "1.3.1" });
  await page.click("#new-tab");
  await page.waitForFunction(
    () =>
      document.getElementById("destination-title").textContent === "New shell",
  );
  assert.equal(
    (await fixture.command()).actions.at(-1).windowId,
    "window-main",
  );
  await page.click("#close-tab");
  const createdID = (await fixture.command()).targets.find(
    (t) => t.title === "New shell",
  ).id;
  await fixture.command({ op: "external_split", targetId: createdID });
  await page.click("#confirm-close");
  await page
    .locator("#action-title")
    .filter({ hasText: "Action wasn’t sent" })
    .waitFor();
  await page.waitForFunction(
    () => !document.getElementById("close-tab").disabled,
  );
  assert.equal(
    (await fixture.command()).actions.length,
    3,
    "changed close scope must not execute",
  );
  await page.click("#close-tab");
  assert.equal(await page.locator("#close-pane-list li").count(), 2);
  await page.click("#confirm-close");
  await page.waitForFunction(
    () =>
      document.getElementById("destination-title").textContent ===
      "Choose a terminal",
  );
  assert.equal(await page.textContent("#action-title"), "Close confirmed");
  assert.equal(
    (await fixture.command()).actions.at(-1).expectedTargets.length,
    2,
  );
  await selectWork(page);
  assert.equal(await page.inputValue("#compose"), "draft stays with work");
  console.log(
    "Passed independent capture health, native backoff/recovery, version guards, and real create/close inventory reconciliation.",
  );

  // Crash after the receipt is claimed and execution begins. Restart on the
  // same HTTPS origin/database: old cookies revoke, unfinished work is uncertain.
  await fixture.command({ hold: true });
  const countBeforeCrash = (await fixture.command()).actions.length;
  await page.fill("#compose", "never replay after crash");
  await page.click("#send-input");
  await eventually(
    () => fixture.command(),
    (s) => s.actions.length === countBeforeCrash + 1,
    "held action entered backend",
  );
  const pendingRaw = await page.evaluate(() =>
    sessionStorage.getItem("onibi.pending-action.v1"),
  );
  const pending = JSON.parse(pendingRaw);
  assert(!pendingRaw.includes("never replay"));
  assert(!pendingRaw.includes(token));
  assert(!pendingRaw.includes("csrf"));
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  const port = new URL(fixture.url).port;
  await fixture.stop();
  await start(port);
  assert.equal((await api(page, "/api/session")).status, 401);
  await page.reload();
  await page.locator("#pair-view").waitFor({ state: "visible" });
  const nextToken = new URLSearchParams(
    new URL(fixture.pairingURL).hash.slice(1),
  ).get("token");
  await page.fill("#pair-token", nextToken);
  await page.click("#pair-submit");
  await page
    .locator("#action-title")
    .filter({ hasText: "Delivery needs checking" })
    .waitFor();
  assert.equal(
    (await api(page, "/api/actions/" + pending.id)).body.state,
    "uncertain",
  );
  assert.equal(
    (await fixture.command()).actions,
    null,
    "restart must not replay",
  );
  assert.equal(
    await page.inputValue("#compose"),
    "",
    "drafts remain memory-only",
  );
  await selectWork(page);
  await page.fill("#compose", "new explicit action");
  assert.equal(await page.locator("#send-input").isDisabled(), true);
  await page.click("#acknowledge-action");
  await page.waitForFunction(
    () => !document.getElementById("send-input").disabled,
  );
  await fixture.command({ advance: 13 * 60 * 60 });
  await page.click("#connection");
  await page.locator("#pair-view").waitFor({ state: "visible" });
  assert.match(await page.textContent("#pair-message"), /expired/);
  assert.equal(await page.locator("#health-dialog").isVisible(), false);
  assert.equal((await fixture.command()).actions, null);
  // Explicit remembered access, named device revocation, and a real process
  // restart. These use production cookies/SQLite; only Ghostty is simulated.
  await fixture.command({ advance: -13 * 60 * 60 });
  const fresh = await fixture.command({ op: "pair" });
  await page.goto(fresh.pairingURL);
  await page.waitForFunction(() => location.hash === "");
  assert.equal(await page.locator("#remember-browser").isChecked(), false);
  assert.equal(await page.locator("#workspace").isVisible(), false);
  await page.fill("#device-name", "My iPhone");
  await page.check("#remember-browser");
  await page.click("#pair-submit");
  await page.locator("#workspace").waitFor({ state: "visible" });
  const deviceCookie = (await context.cookies()).find(
    (c) => c.name === "__Host-onibi_device",
  );
  assert(
    deviceCookie &&
      deviceCookie.httpOnly &&
      deviceCookie.secure &&
      deviceCookie.sameSite === "Strict",
  );
  await page.screenshot({
    path: path.join(screenshotDir, "remembered-browser.png"),
    fullPage: true,
  });
  const rememberedID = (await api(page, "/api/session")).body.device.id;
  const spareContext = await browser.newContext({
    ignoreHTTPSErrors: true,
    viewport: { width: 320, height: 740 },
  });
  const spare = await spareContext.newPage();
  await spare.goto((await fixture.command({ op: "pair" })).pairingURL);
  await spare.fill("#device-name", "Spare browser");
  await spare.click("#pair-submit");
  await spare.locator("#workspace").waitFor({ state: "visible" });
  await page.click("#open-devices");
  await page
    .locator("#devices-list")
    .filter({ hasText: "Spare browser" })
    .waitFor();
  await page.screenshot({
    path: path.join(screenshotDir, "devices.png"),
    fullPage: true,
  });
  assert.deepEqual(
    (
      await new AxeBuilder({ page })
        .withTags(["wcag2a", "wcag2aa", "wcag21aa"])
        .analyze()
    ).violations,
    [],
    "device accessibility",
  );

  await page
    .getByRole("button", {
      name: "Revoke access for Spare browser",
      exact: true,
    })
    .click();
  assert.match(await page.textContent("#revoke-name"), /Spare browser/);
  await page.click("#confirm-revoke");
  await page.waitForFunction(
    () =>
      !document
        .getElementById("devices-list")
        .textContent.includes("Spare browser"),
  );
  assert.equal((await api(spare, "/api/session")).status, 401);
  await spareContext.close();
  await fixture.stop();
  await start(port);
  await page.reload();
  await page.locator("#workspace").waitFor({ state: "visible" });
  assert.equal((await api(page, "/api/session")).body.device.id, rememberedID);
  assert.equal(
    (await fixture.command()).actions,
    null,
    "remembered reconnection must not send commands",
  );
  assert.equal(await page.evaluate(() => localStorage.length), 0);
  await fixture.command({ advance: 31 * 24 * 60 * 60 });
  await page.reload();
  await page.locator("#pair-view").waitFor({ state: "visible" });
  await page.click("#resume-browser");
  await page
    .locator("#pair-message")
    .filter({ hasText: "no active remembered access" })
    .waitFor();
  assert.equal((await fixture.command()).actions, null);
  console.log(
    "Passed named devices, opt-in remembrance, individual revocation, restart recovery, and fixed 30-day expiry.",
  );
  assert.deepEqual(errors, []);
  console.log(
    JSON.stringify({
      passed: true,
      engine,
      integration: "production Go HTTPS server",
      checks: [
        "secure cookies",
        "single-use pairing",
        "CSRF and origin",
        "literal send/paste",
        "durable deduplication",
        "lost response",
        "persistent sessions without Ghostty",
        "independent readiness",
        "backoff",
        "version guard",
        "create/close",
        "crash recovery without replay",
        "restart revocation",
        "12-hour session expiry",
        "memory-only drafts",
        "remembered devices and revocation",
        "fixed 30-day expiry",
        "capability and device accessibility",
        "no JS errors",
      ],
    }),
  );
})()
  .catch((error) => {
    // Assertion messages intentionally omit fixture pairing links and cookies.
    console.error(error);
    process.exitCode = 1;
  })
  .finally(async () => {
    await browser?.close();
    await fixture?.stop();
    fs.rmSync(temp, { recursive: true, force: true });
  });
