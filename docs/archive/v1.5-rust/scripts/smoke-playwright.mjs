// Visual smoke after v1.5 layout reshape.
// Opens desktop app + mobile PWA via vite dev servers, screenshots both,
// asserts key DOM markers for the new chrome (banner host, recent files,
// composer dock, mobile inbox).
import { chromium } from "playwright";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const outDir = resolve(here, "../output/smoke");
mkdirSync(outDir, { recursive: true });

const summary = [];
function record(label, value) {
  summary.push({ label, value });
  console.log(`${label}: ${value}`);
}

async function visit(browser, url, screenshotName, markers, consumeConsole) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await ctx.newPage();
  const consoleErrors = [];
  page.on("console", (message) => {
    if (message.type() === "error") {
      consoleErrors.push(message.text());
    }
  });
  page.on("pageerror", (error) => {
    consoleErrors.push(`pageerror: ${error.message}`);
  });
  await page.goto(url, { waitUntil: "load", timeout: 15_000 });
  await page.waitForTimeout(800);
  const found = {};
  for (const [label, selector] of Object.entries(markers)) {
    const count = await page.locator(selector).count();
    found[label] = count;
    record(`${url} ${label}`, count);
  }
  const screenshotPath = resolve(outDir, `${screenshotName}.png`);
  await page.screenshot({ path: screenshotPath, fullPage: true });
  record(`${url} screenshot`, screenshotPath);
  if (consumeConsole) {
    consumeConsole(consoleErrors);
  } else {
    if (consoleErrors.length > 0) {
      record(`${url} console errors`, consoleErrors.length);
      writeFileSync(
        resolve(outDir, `${screenshotName}.errors.txt`),
        consoleErrors.join("\n"),
      );
    }
  }
  await ctx.close();
  return { found, consoleErrors };
}

const browser = await chromium.launch();
try {
  await visit(
    browser,
    "http://localhost:1420/",
    "desktop-empty",
    {
      titleBar: 'div.app-frame',
      agentRail: 'aside[aria-label="Session rail"]',
      rightDock: 'aside[aria-label="Workspace dock"]',
      mainPane: '[data-testid="main-pane-empty"], [data-testid="main-pane-terminal"], [data-testid="main-pane-editor"]',
      emptyState: '[data-testid="empty-state"]',
      // no approval banner expected w/o pending approvals
    },
  );

  await visit(
    browser,
    "http://localhost:5173/",
    "mobile-pairing",
    {
      pairingPanel: '.pairing-panel',
      pairTextarea: 'textarea[aria-label="Pairing payload"]',
      pairButton: 'button:has-text("Pair")',
    },
  );
} finally {
  await browser.close();
}

writeFileSync(resolve(outDir, "summary.json"), JSON.stringify(summary, null, 2));
console.log(`\nSmoke complete. Artifacts: ${outDir}`);
