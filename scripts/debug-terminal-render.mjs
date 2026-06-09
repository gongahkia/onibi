// Reproduce the empty-terminal case in vite dev so I can inspect the live DOM.
// Inject a fake session into the Zustand store, mock pty subscribe/replay/spawn,
// then dump computed sizes + class lists of the terminal layout chain.
import { chromium } from "playwright";
import { writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const outDir = resolve(here, "../output/smoke");
mkdirSync(outDir, { recursive: true });

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
const page = await ctx.newPage();

await page.addInitScript(() => {
  window.localStorage.setItem("onibi.onboarding.dismissed", "1");
});

page.on("console", (msg) => {
  if (msg.type() === "error" || msg.text().includes("onibi") || msg.text().includes("pty")) {
    console.log(`[browser ${msg.type()}]`, msg.text());
  }
});
page.on("pageerror", (err) => console.log("[pageerror]", err.message));

await page.goto("http://localhost:1420/", { waitUntil: "load", timeout: 15_000 });
await page.waitForTimeout(800);

// Expose the session store + bridge mocks by importing them via dynamic import
// against the vite dev server (modules are cached so this hits the same store).
const injected = await page.evaluate(async () => {
  try {
    const sessionsMod = await import("/src/lib/sessions.ts");
    const tauriBridgeMod = await import("/src/lib/tauri-bridge.ts");
    window.__sessionsMod = sessionsMod;
    const fakeWorkspace = {
      id: "workspace:/repo",
      path: "/repo",
      name: "elegant-elefant-internship",
    };
    const fakeSession = {
      id: "pty-debug-1",
      agent: "claude-code",
      workspaceId: fakeWorkspace.id,
      title: "Claude Code",
      status: "running",
      createdAt: Date.now(),
      pendingApprovals: [],
      cwd: "/repo",
    };
    sessionsMod.useSessionStore.setState({
      hydrated: true,
      workspaces: [fakeWorkspace],
      sessions: [fakeSession],
      activeSessionId: fakeSession.id,
      terminalLayout: {
        type: "leaf",
        paneId: "pane-debug-1",
        sessionId: fakeSession.id,
      },
      activeTerminalPaneId: "pane-debug-1",
      activeWorkspaceId: fakeWorkspace.id,
    });
    return { ok: true };
  } catch (err) {
    return { ok: false, error: String(err) };
  }
});
console.log("injection:", injected);

await page.waitForTimeout(800);

const probe = await page.evaluate(() => {
  function rect(sel) {
    const el = document.querySelector(sel);
    if (!el) return { sel, missing: true };
    const r = el.getBoundingClientRect();
    return {
      sel,
      tag: el.tagName.toLowerCase(),
      classes: el.className,
      width: Math.round(r.width),
      height: Math.round(r.height),
    };
  }
  function rectAll(sel) {
    return Array.from(document.querySelectorAll(sel)).map((el, i) => {
      const r = el.getBoundingClientRect();
      return {
        sel: `${sel}[${i}]`,
        classes: el.className,
        dataActive: el.getAttribute("data-active"),
        width: Math.round(r.width),
        height: Math.round(r.height),
      };
    });
  }
  return {
    mainPane: rect("main.main-pane"),
    terminalSurface: rect(".terminal-surface"),
    workspaceBody: rect(".workspace-terminal-body"),
    terminalPane: rect(".terminal-pane"),
    terminalBodies: rect(".terminal-pane-bodies"),
    terminalBody: rectAll(".terminal-pane-body"),
    terminalViewShell: rect(".terminal-view-shell"),
    terminalView: rect(".terminal-view"),
    xtermScreen: rect(".xterm-screen"),
    composer: rect(".pane-composer-dock"),
    terminalPaneInnerHTML: document.querySelector(".terminal-pane")?.innerHTML.slice(0, 1200),
  };
});

console.log(JSON.stringify(probe, null, 2));
await page.screenshot({ path: resolve(outDir, "debug-terminal.png"), fullPage: false });
await browser.close();
console.log("done");
