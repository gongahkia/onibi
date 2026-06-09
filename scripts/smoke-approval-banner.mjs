// End-to-end smoke: simulate an approval-pending realtime message and assert
// the ApprovalInlineBanner appears, then exercise Allow + verify the decide
// HTTP call. Mocks WebSocket via addInitScript, intercepts /v1/approval/...
// /decide via page.route, screenshots the result.
import { chromium } from "playwright";
import { mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const outDir = resolve(here, "../output/smoke");
mkdirSync(outDir, { recursive: true });

const TOKEN = "test-banner-token";
const PORT = 17893;
const PENDING = {
  type: "approval-pending",
  protocol_version: "1.0",
  approval_id: "01HBANNERE2E0000000000000",
  machine_id: "01HBANNERE2E0000MACHINE",
  session_id: "01HBANNERE2E0000SESSION",
  agent: "claude-code",
  tool: "Bash",
  input: { command: "rm -rf node_modules" },
  cwd: "/tmp/onibi-e2e",
};

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
const page = await ctx.newPage();

await page.addInitScript(
  ({ token, port }) => {
    window.localStorage.setItem("onibi.token", token);
    window.localStorage.setItem("onibi.port", String(port));
    window.localStorage.setItem("onibi.onboarding.dismissed", "1");
    class MockWebSocket extends EventTarget {
      static OPEN = 1;
      static CLOSED = 3;
      static instances = [];
      constructor(url) {
        super();
        this.url = url;
        this.readyState = 1;
        this.onopen = null;
        this.onmessage = null;
        this.onerror = null;
        this.onclose = null;
        MockWebSocket.instances.push(this);
        setTimeout(() => {
          const event = new Event("open");
          this.dispatchEvent(event);
          this.onopen?.(event);
        }, 0);
      }
      send() {}
      close() {
        this.readyState = 3;
        const event = new Event("close");
        this.dispatchEvent(event);
        this.onclose?.(event);
      }
    }
    window.WebSocket = MockWebSocket;
    window.__sendApproval = (message) => {
      if (MockWebSocket.instances.length === 0) {
        throw new Error("no MockWebSocket instance");
      }
      // broadcast to every subscriber — banner + bridge + future listeners
      const data = JSON.stringify(message);
      for (const target of MockWebSocket.instances) {
        if (target.readyState !== 1) continue;
        const event = new MessageEvent("message", { data });
        target.dispatchEvent(event);
        target.onmessage?.(event);
      }
    };
  },
  { token: TOKEN, port: PORT },
);

const decideCalls = [];
await page.route("**/*", async (route) => {
  const url = route.request().url();
  if (/\/v1\/approval\/.+\/decide/.test(url)) {
    decideCalls.push({
      url,
      body: route.request().postData(),
      headers: route.request().headers(),
    });
    await route.fulfill({ status: 200, body: "{}", contentType: "application/json" });
    return;
  }
  await route.continue();
});

page.on("console", (message) => {
  if (message.type() === "error" || message.text().includes("approval")) {
    console.log(`[browser ${message.type()}]`, message.text());
  }
});
page.on("pageerror", (err) => console.log("[browser pageerror]", err.message));

await page.goto("http://localhost:1420/", { waitUntil: "load", timeout: 15_000 });
await page.waitForTimeout(500);

const wsCount = await page.evaluate(
  () => (window.WebSocket?.instances ?? []).length,
);
console.log(`MockWebSocket instances after load: ${wsCount}`);

await page.evaluate((message) => window.__sendApproval(message), PENDING);

await page.waitForSelector('section.approval-banner', { timeout: 5_000 });
const agent = await page.locator(".approval-banner-agent").textContent();
const tool = await page.locator(".approval-banner-tool").textContent();
const cwd = await page.locator(".approval-banner-cwd").textContent();
const previewText = await page
  .locator('[aria-label="Bash command preview"]')
  .textContent();
const riskBadges = await page.locator(".approval-banner-risk-badge").allTextContents();

const want = (name, actual, expected) => {
  const ok = actual?.includes(expected);
  console.log(`${name}: ${ok ? "ok" : "FAIL"} (got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)})`);
  if (!ok) {
    process.exitCode = 1;
  }
};
want("agent label", agent, "claude-code");
want("tool label", tool, "Bash");
want("cwd label", cwd, "/tmp/onibi-e2e");
want("preview content", previewText, "rm -rf node_modules");
console.log("risk badges:", riskBadges);
if (!riskBadges.includes("Destructive delete")) {
  console.log("risk badge: FAIL (missing Destructive delete)");
  process.exitCode = 1;
}

await page.screenshot({ path: resolve(outDir, "approval-banner-pending.png"), fullPage: false });

const allowCount = await page.locator('button.approval-allow').count();
console.log(`Allow buttons found: ${allowCount}`);
const allowBtn = page.locator('button.approval-allow').first();
await allowBtn.scrollIntoViewIfNeeded();
console.log(`Allow button visible: ${await allowBtn.isVisible()}, enabled: ${await allowBtn.isEnabled()}`);
await allowBtn.evaluate((el) => (el).click());
console.log("Allow clicked");
await page.waitForTimeout(500);
console.log(`decide calls after wait: ${decideCalls.length}`);
await page.waitForFunction(
  () => document.querySelectorAll("section.approval-banner").length === 0,
  { timeout: 3_000 },
);

if (decideCalls.length !== 1) {
  console.log(`decide call count: FAIL (got ${decideCalls.length})`);
  process.exitCode = 1;
} else {
  const body = JSON.parse(decideCalls[0].body ?? "{}");
  if (body.decision !== "allow") {
    console.log(`decide body decision: FAIL (got ${body.decision})`);
    process.exitCode = 1;
  } else {
    console.log("decide call ok:", decideCalls[0].url, body);
  }
  const auth = decideCalls[0].headers.authorization;
  if (auth !== `Bearer ${TOKEN}`) {
    console.log(`decide call auth: FAIL (got ${auth})`);
    process.exitCode = 1;
  }
}

await page.screenshot({ path: resolve(outDir, "approval-banner-resolved.png"), fullPage: false });
await browser.close();
console.log(`Approval banner e2e ${process.exitCode ? "FAILED" : "PASSED"}; artifacts in ${outDir}`);
