// Manual end-to-end check of Web Push through Google's real push service.
// Needs Google Chrome, a display and network access, so it is not part of
// npm test. Run: npm run test:push-live
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const { spawn, execFileSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const readline = require("node:readline");

const root = path.resolve(__dirname, "..");
const temp = fs.mkdtempSync(path.join(os.tmpdir(), "onibi-push-live-"));
const binary = path.join(temp, "fixture");

function startFixture() {
  const child = spawn(
    binary,
    ["--state", path.join(temp, "state"), "--notify", "--log"],
    { stdio: ["pipe", "pipe", "inherit"] },
  );
  const waiters = new Map();
  let sequence = 0;
  readline.createInterface({ input: child.stdout }).on("line", (line) => {
    const data = JSON.parse(line);
    const id = data.ready ? 0 : data.id;
    waiters.get(id)?.(data);
    waiters.delete(id);
  });
  const ready = new Promise((resolve) => waiters.set(0, resolve));
  const command = (data) =>
    new Promise((resolve) => {
      const id = ++sequence;
      waiters.set(id, resolve);
      child.stdin.write(JSON.stringify({ id, ...data }) + "\n");
    });
  return ready.then((info) => ({ ...info, command, child }));
}

async function waitForNotification(page, title, timeoutMs) {
  const end = Date.now() + timeoutMs;
  while (Date.now() < end) {
    const titles = await page.evaluate(async () => {
      const registration = await navigator.serviceWorker.getRegistration();
      return (await registration.getNotifications()).map((n) => n.title);
    });
    if (titles.includes(title)) return;
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error(`No notification titled "${title}" within ${timeoutMs} ms`);
}

(async () => {
  execFileSync(
    "go",
    ["build", "-tags", "browserfixture", "-o", binary, "./web-tests/fixture"],
    { cwd: root, stdio: "inherit" },
  );
  const fixture = await startFixture();
  // Push subscriptions need a real profile; incognito contexts refuse them.
  const context = await chromium.launchPersistentContext(
    path.join(temp, "profile"),
    {
      channel: "chrome",
      headless: false,
      ignoreHTTPSErrors: true,
      args: ["--ignore-certificate-errors"],
    },
  );
  try {
    await context.grantPermissions(["notifications"], {
      origin: fixture.url,
    });
    const page = await context.newPage();
    await page.goto(fixture.pairingURL);
    await page.click("#pair-submit");
    await page.locator("#workspace").waitFor({ state: "visible" });
    await page.click("#open-devices");
    await page
      .locator("#notify-toggle")
      .filter({ hasText: "Turn on notifications" })
      .click();
    await page
      .locator("#notify-status")
      .filter({ hasText: /^On\./ })
      .waitFor({ timeout: 90000 })
      .catch(async (error) => {
        throw new Error(
          "Subscribing failed: " + (await page.textContent("#notify-status")),
          { cause: error },
        );
      });
    const endpoint = await page.evaluate(async () => {
      const registration = await navigator.serviceWorker.getRegistration();
      return (await registration.pushManager.getSubscription()).endpoint;
    });
    assert.match(endpoint, /^https:\/\/fcm\.googleapis\.com\//);
    console.log("Subscribed through", new URL(endpoint).host);

    await page.click("#notify-test");
    await page
      .locator("#notify-status")
      .filter({ hasText: "Test sent" })
      .waitFor({ timeout: 30000 });
    await waitForNotification(page, "Onibi notifications are on", 60000);
    console.log("Test notification delivered by the push service.");

    await fixture.command({ attention: "active" });
    await new Promise((resolve) => setTimeout(resolve, 6000));
    await fixture.command({ attention: "input" });
    await waitForNotification(page, "build needs input", 60000);
    console.log("Watcher notification delivered for a session needing input.");
    console.log(
      JSON.stringify({ passed: true, endpointHost: new URL(endpoint).host }),
    );
  } finally {
    await context.close();
    fixture.child.kill("SIGKILL");
  }
})().catch((error) => {
  console.error(error);
  process.exit(1);
});
