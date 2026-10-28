// Linux/WSL end-to-end checks against the production executable, embedded
// daemon, HTTP handler, SQLite and real multiplexers. No native/test backdoor.
"use strict";
const assert = require("node:assert/strict");
const fs = require("node:fs");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");
const { spawn, spawnSync } = require("node:child_process");
const { randomUUID, createHash } = require("node:crypto");
const { once } = require("node:events");

assert.equal(
  process.platform,
  "linux",
  "Live host tests run on Linux/WSL; macOS has separate native tests.",
);
const root = path.resolve(__dirname, "..");
const temp = fs.mkdtempSync(path.join(os.tmpdir(), "onibi-live-"));
const binary = path.join(temp, "onibi");
const terminal = path.join(__dirname, "live-terminal.cjs");
const quote = (s) => "'" + s.replaceAll("'", "'\"'\"'") + "'";
const env = { ...process.env, ONIBI_PLAIN: "1", TERM: "xterm-256color" };
for (const key of ["TMUX", "TMUX_PANE", "ZELLIJ", "ZELLIJ_SESSION_NAME"])
  delete env[key];
for (const key of [
  "XDG_DATA_HOME",
  "XDG_CONFIG_HOME",
  "XDG_CACHE_HOME",
  "XDG_RUNTIME_DIR",
  "ZELLIJ_SOCKET_DIR",
  "SCREENDIR",
]) {
  env[key] = path.join(temp, key);
  fs.mkdirSync(env[key], { mode: 0o700 });
}
const stateDir = path.join(env.XDG_DATA_HOME, "onibi");
fs.mkdirSync(stateDir, { mode: 0o700 });
const intakeSocket = path.join(env.XDG_RUNTIME_DIR, "onibi.sock");
const adminSocket = path.join(stateDir, "web-admin.sock");
const tmuxSocket = path.join(temp, "tmux.sock");
const muxes = new Map();
const targets = new Map();
let server,
  origin,
  cookie = "",
  csrf = "",
  log = "",
  browser,
  failures = 0;
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function run(file, args, options = {}) {
  const result = spawnSync(file, args, {
    cwd: temp,
    env,
    encoding: "utf8",
    timeout: 15000,
    ...options,
  });
  if (!options.allowFailure)
    assert.equal(
      result.status,
      0,
      result.error?.message || result.stderr || result.stdout,
    );
  return result;
}
const cli = (...args) => run(binary, args).stdout;
const cliFailure = (...args) => {
  const result = run(binary, args, { allowFailure: true });
  assert.notEqual(
    result.status,
    0,
    "command unexpectedly succeeded: " + args.join(" "),
  );
  assert(!result.error, result.error?.message);
  return result.stderr;
};
function socketRequest(socketPath, event) {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection(socketPath);
    let raw = "";
    socket.setTimeout(12000, () =>
      socket.destroy(new Error("Local RPC timeout")),
    );
    socket.on("connect", () => socket.write(JSON.stringify(event) + "\n"));
    socket.on("data", (chunk) => {
      raw += chunk;
      if (raw.includes("\n")) {
        socket.destroy();
        try {
          resolve(JSON.parse(raw.split("\n")[0]));
        } catch (error) {
          reject(error);
        }
      }
    });
    socket.on("error", reject);
    socket.on("end", () => {
      if (!raw.includes("\n"))
        reject(new Error("Local RPC ended without a response"));
    });
  });
}
const rpc = (event) => socketRequest(intakeSocket, event);
const admin = (operation) => socketRequest(adminSocket, { operation });
async function eventually(read, label, timeout = 15000) {
  let last;
  for (const end = Date.now() + timeout; Date.now() < end; await delay(100)) {
    try {
      const value = await read();
      if (value) return value;
    } catch (error) {
      last = error;
    }
  }
  throw new Error(
    "Timed out: " + label + (last ? " (" + last.message + ")" : ""),
  );
}
async function request(route, body, headers = {}) {
  const response = await fetch(origin + route, {
    method: body ? "POST" : "GET",
    headers: {
      Origin: origin,
      Cookie: cookie,
      "Content-Type": "application/json",
      "X-Onibi-CSRF": csrf,
      ...headers,
    },
    body: body ? JSON.stringify(body) : undefined,
    signal: AbortSignal.timeout(15000),
  });
  return {
    status: response.status,
    body: await response.json(),
    cookies: response.headers.getSetCookie(),
  };
}
async function api(route, body) {
  const response = await request(route, body);
  assert.equal(
    response.status,
    200,
    route + ": " + JSON.stringify(response.body),
  );
  return response.body;
}
async function pair() {
  const link = new URL((await admin("pair")).pairingURL);
  const token = new URLSearchParams(link.hash.slice(1)).get("token");
  const paired = await request("/api/pair", { token, name: "Live Linux test" });
  assert.equal(paired.status, 200, "pairing failed");
  cookie = paired.cookies
    .find((c) => c.startsWith("onibi_local_session="))
    ?.split(";")[0];
  assert(cookie, "missing HttpOnly session cookie");
  csrf = paired.body.csrf;
  assert.equal(
    (await request("/api/pair", { token })).status,
    401,
    "pairing link reused",
  );
}
async function start() {
  const child = spawn(
    binary,
    ["web", "--background", "--listen", "127.0.0.1:0"],
    { cwd: temp, env, stdio: ["ignore", "pipe", "pipe"] },
  );
  server = child;
  child.exited = once(child, "exit");
  const collect = (chunk) => {
    log = (log + chunk).slice(-32000);
  };
  child.stdout.on("data", collect);
  child.stderr.on("data", collect);
  const status = await eventually(async () => {
    if (child.exitCode !== null || child.signalCode !== null)
      throw new Error("web server exited during startup");
    const response = await admin("status");
    return response.status?.sessionsOnly && response.status;
  }, "production web startup");
  origin = status.origin;
  await eventually(
    async () => (await rpc({ type: "ping" })).text,
    "embedded daemon startup",
  );
  if (cookie)
    assert.equal(
      (await request("/api/session")).status,
      401,
      "temporary access survived restart",
    );
  cookie = csrf = "";
  await pair();
}
async function stop(signal = "SIGTERM") {
  if (!server) return;
  const child = server;
  server = null;
  if (child.exitCode === null && child.signalCode === null) child.kill(signal);
  const timer = setTimeout(() => child.kill("SIGKILL"), 10000);
  const [code, stoppedBy] = await child.exited;
  clearTimeout(timer);
  if (signal === "SIGTERM")
    assert.equal(code, 0, "web shutdown failed or timed out: " + stoppedBy);
}
function configure(kind) {
  // JSON is valid YAML. Every backend path/config is explicit and private.
  const multiplexer = { default: kind };
  for (const [name, backend] of muxes)
    multiplexer[name] = { bin: backend.bin, config: backend.config };
  fs.writeFileSync(
    path.join(stateDir, "config.yaml"),
    JSON.stringify({
      daemon: { liveness_interval: "1s" },
      shell: { default: "/bin/bash", login: false },
      multiplexer,
    }),
    { mode: 0o600 },
  );
}
async function action(session, id = randomUUID()) {
  const receipt = await api("/api/actions", { id, session });
  assert.equal(receipt.state, "completed", JSON.stringify(receipt));
  return receipt;
}
const capture = (id) =>
  api("/api/sessions/snapshot?sessionId=" + id + "&lines=1000");
const input = (id, text, enter = true, receipt) =>
  action({ kind: "input", sessionId: id, text, enter }, receipt);
const infos = async () => (await api("/api/sessions")).sessions;
async function expectOutput(id, text) {
  let latest;
  try {
    return await eventually(async () => {
      latest = await capture(id);
      return latest.text.includes(text) && latest;
    }, "captured " + text);
  } catch (error) {
    // Only the isolated fixture's output, never a user's terminal.
    error.message += "\nFixture output: " + JSON.stringify(latest?.text);
    throw error;
  }
}
function track(kind, id, name = "onibi-" + id) {
  targets.set(kind + ":" + name, { kind, name });
  return id;
}
function killTarget({ kind, name }) {
  const mux = muxes.get(kind);
  const args =
    kind === "tmux"
      ? ["kill-session", "-t", name]
      : kind === "zellij"
        ? ["kill-session", name]
        : ["-S", name, "-X", "quit"];
  run(mux.bin, args, { allowFailure: true });
}
async function lifecycle(kind) {
  configure(kind);
  const name = "live-" + kind;
  const args = ["space argument", 'quote"d', "single'quote", "日本語"];
  cli(
    "session",
    "new",
    "--name",
    name,
    "--cwd",
    temp,
    "--mux",
    kind,
    process.execPath,
    terminal,
    ...args,
  );
  const id = track(kind, (await infos()).find((s) => s.name === name).id);
  try {
    const ready = await expectOutput(id, "READY");
    assert(ready.text.includes("CWD:" + temp), "working directory changed");
    for (const arg of args)
      assert(
        ready.text.includes("ARG:" + JSON.stringify(arg)),
        "argument fidelity: " + arg,
      );
    await eventually(
      async () =>
        (await infos()).find((s) => s.id === id)?.attention.state === "input",
      "prompt attention",
    );
    assert.equal((await capture(id)).prompt.reason, "Yes/no prompt");
    const literal = "héllo 日本語 $HOME $(no_expand) ; | --flag";
    const receipt = randomUUID();
    await input(id, literal, true, receipt);
    await input(id, literal, true, receipt);
    let captured = await expectOutput(id, "INPUT:" + JSON.stringify(literal));
    assert.equal(
      captured.text.split("INPUT:" + JSON.stringify(literal)).length - 1,
      1,
      "receipt replay typed twice",
    );
    for (const key of ["1", "y", "up", "enter"])
      await action({ kind: "key", sessionId: id, key });
    await expectOutput(id, 'INPUT:"1y\\u001b[A"');
    await action({ kind: "interrupt", sessionId: id });
    await expectOutput(id, "INTERRUPTED");
    for (const literal of [
      "$HOME ${SHELL} $:ku: ^C",
      String.raw`backslash \\ \n \033 "double" 'single'`,
      "--help",
    ]) {
      await input(id, literal);
      await expectOutput(id, "INPUT:" + JSON.stringify(literal));
    }
    const longInput = "DIGEST:" + "日本語 $HOME ^C \\".repeat(700);
    assert(Buffer.byteLength(longInput) <= 16384);
    await input(id, longInput);
    await expectOutput(
      id,
      "SHA256:" + createHash("sha256").update(longInput).digest("hex"),
    );
    const denied = await request(
      "/api/actions",
      {
        id: randomUUID(),
        session: {
          kind: "input",
          sessionId: id,
          text: "UNAUTHORIZED",
          enter: true,
        },
      },
      { "X-Onibi-CSRF": "wrong" },
    );
    assert.equal(denied.status, 403);
    assert(!(await capture(id)).text.includes("UNAUTHORIZED"));
    const prefix = await rpc({
      type: "session_input",
      session: id.slice(0, 8),
      text: "wrong target",
      exact: true,
    });
    assert(prefix.rejected && prefix.reason, "exact-ID guard failed");
    cli("session", "rename", name, name + "-renamed");
    cliFailure("session", "rename", name + "-renamed", "invalid name");
    await stop();
    await start();
    assert.equal(
      (await infos()).find((s) => s.id === id)?.name,
      name + "-renamed",
    );
    await input(id, literal, true, receipt);
    assert.equal(
      (await capture(id)).text.split("INPUT:" + JSON.stringify(literal))
        .length - 1,
      1,
      "restart replay typed twice",
    );
    await stop("SIGKILL");
    await start();
    await input(id, "after crash");
    await expectOutput(id, 'INPUT:"after crash"');
    await input(id, "color");
    captured = await expectOutput(id, "COLORED");
    assert(!captured.text.includes("\x1b["));
    if (kind === "tmux") {
      assert(captured.ansi.includes("\x1b["));
      for (let i = 0; i < 2; i++) {
        await input(id, "bell");
        await eventually(
          async () =>
            (await infos()).find((s) => s.id === id)?.attention.state ===
            "bell",
          "repeated bell",
        );
        await input(id, "acknowledged " + i);
        await eventually(
          async () =>
            (await infos()).find((s) => s.id === id)?.attention.state ===
            "active",
          "bell acknowledgment",
        );
      }
    }
    await input(id, "history");
    captured = await expectOutput(id, "HISTORY-239");
    assert(captured.text.includes("HISTORY-0"), "scrollback lost");
    cliFailure("session", "kill", name + "-renamed");
    cli("session", "kill", "--yes", name + "-renamed");
    assert(!(await infos()).some((s) => s.id === id));
    assert(
      JSON.parse(cli("session", "list", "--all", "--json")).some(
        (s) => s.id === id && s.state === "ended",
      ),
    );
    console.log(
      "PASS " +
        kind +
        ": real CLI/HTTP input, keys, attention, receipts, restart/crash recovery, history, rename and confirmed kill",
    );
  } finally {
    killTarget({ kind, name: "onibi-" + id });
  }
}
async function adoption(kind) {
  configure(kind);
  const mux = muxes.get(kind);
  const name = "live-adopt-" + kind;
  targets.set(kind + ":" + name, { kind, name });
  let target = name;
  try {
    if (kind === "tmux")
      run(mux.bin, [
        "-f",
        mux.config,
        "new-session",
        "-d",
        "-s",
        name,
        process.execPath,
        terminal,
      ]);
    else if (kind === "screen")
      run(mux.bin, [
        "-c",
        mux.config,
        "-dmS",
        name,
        process.execPath,
        terminal,
      ]);
    else {
      run(mux.bin, [
        "--config",
        mux.config,
        "attach",
        "--create-background",
        name,
        "--",
        process.execPath,
        terminal,
      ]);
      const pane = await eventually(
        async () =>
          JSON.parse(
            run(mux.bin, ["--session", name, "action", "list-panes", "--json"])
              .stdout,
          ).find((p) => !p.is_plugin && !p.exited),
        "adopted pane startup",
      );
      target +=
        "|" +
        (String(pane.id).startsWith("terminal_")
          ? pane.id
          : "terminal_" + pane.id);
    }
    cli(
      "session",
      "adopt",
      "--name",
      "adopted-" + kind,
      "--cwd",
      temp,
      kind,
      target,
    );
    assert.match(
      cliFailure("session", "adopt", "--cwd", temp, kind, target),
      /already managed/,
    );
    const id = (await infos()).find((s) => s.name === "adopted-" + kind).id;
    await expectOutput(id, "READY");
    await input(id, "adopted input");
    await expectOutput(id, 'INPUT:"adopted input"');
    await input(id, "exit");
    await eventually(
      async () => !(await infos()).some((s) => s.id === id),
      "natural exit detection",
    );
    assert(
      JSON.parse(cli("session", "list", "--all", "--json")).some(
        (s) => s.id === id && s.state === "ended",
      ),
    );
    console.log(
      "PASS " +
        kind +
        ": adoption, duplicate rejection, input and natural exit persistence",
    );
  } finally {
    killTarget({ kind, name });
  }
}
async function realBrowser(kind) {
  configure(kind);
  const { chromium } = require("playwright");
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto((await admin("pair")).pairingURL);
  await page.click("#pair-submit");
  await page.waitForFunction(
    () =>
      document.getElementById("connection-label").textContent === "Connected",
  );
  assert.equal(await page.locator("#new-window").isHidden(), true);
  await page.click("#open-picker");
  await page.click("#picker-new-session");
  await page.fill(
    "#new-session-command",
    quote(process.execPath) + " " + quote(terminal),
  );
  await page.fill("#new-session-cwd", temp);
  await page.fill("#new-session-name", "browser-live");
  await page.click("#start-session");
  await page
    .locator("#destination-title")
    .filter({ hasText: "browser-live" })
    .waitFor();
  const id = track(
    kind,
    (await infos()).find((s) => s.name === "browser-live").id,
  );
  await expectOutput(id, "READY");
  await page.fill("#compose", "from browser 日本語");
  await page.click("#send-input");
  await expectOutput(id, 'INPUT:"from browser 日本語"');
  await page.click("#read-output");
  await page
    .locator("#terminal-output")
    .filter({ hasText: 'INPUT:"from browser 日本語"' })
    .waitFor();
  await page.click("#session-interrupt");
  await expectOutput(id, "INTERRUPTED");
  await page.click("#session-kill");
  await page.click("#confirm-close");
  await eventually(
    async () => !(await infos()).some((s) => s.id === id),
    "browser confirmed kill",
  );
  assert.deepEqual(errors, []);
  console.log(
    "PASS Chromium → production HTTP → real " +
      kind +
      ": create, Unicode input, output, interrupt and confirmed kill",
  );
  await browser.close();
  browser = null;
}
async function check(label, fn) {
  try {
    await fn();
  } catch (error) {
    failures++;
    console.error("FAIL " + label + ": " + error.stack);
  }
}

function windowsClient() {
  // Optional WSL interop check. This is Windows curl, not a Windows Onibi
  // executable or a substitute for testing an actual Windows browser.
  const response = run("curl.exe", [
    "--silent",
    "--show-error",
    "--noproxy",
    "*",
    "--max-time",
    "10",
    "--write-out",
    "\n%{http_code}",
    origin + "/api/session",
  ]).stdout.trim();
  const split = response.lastIndexOf("\n");
  assert.equal(response.slice(split + 1).trim(), "401");
  assert.equal(JSON.parse(response.slice(0, split)).code, "unauthorized");
  console.log(
    "PASS Windows curl.exe → WSL localhost: production authentication boundary reachable",
  );
}

(async () => {
  try {
    const required = process.env.ONIBI_LIVE_MUX?.split(",");
    const selected = required || ["tmux", "zellij", "screen"];
    for (const kind of selected) {
      assert(
        ["tmux", "zellij", "screen"].includes(kind),
        "Unknown multiplexer " + kind,
      );
      const found = run("sh", ["-c", 'command -v "$1"', "onibi-live", kind], {
        allowFailure: true,
      });
      if (found.status !== 0) {
        assert(!required, "Required multiplexer missing: " + kind);
        console.log(
          "SKIP " +
            kind +
            ": not installed (set ONIBI_LIVE_MUX to require backends)",
        );
        continue;
      }
      let bin = found.stdout.trim();
      const version = run(
        bin,
        kind === "tmux" ? ["-V"] : kind === "screen" ? ["-v"] : ["--version"],
      ).stdout.trim();
      const config = path.join(temp, kind + ".conf");
      fs.writeFileSync(
        config,
        kind === "zellij"
          ? 'default_shell "/bin/bash"\nsession_serialization false\nshow_startup_tips false\nshow_release_notes false\n'
          : kind === "screen"
            ? "startup_message off\ndefscrollback 3000\ndefutf8 on\n"
            : "",
        { mode: 0o600 },
      );
      if (kind === "tmux") {
        const wrapper = path.join(temp, "tmux-test");
        fs.writeFileSync(
          wrapper,
          "#!/bin/sh\nexec " +
            quote(bin) +
            " -S " +
            quote(tmuxSocket) +
            ' "$@"\n',
          { mode: 0o700 },
        );
        bin = wrapper;
      }
      muxes.set(kind, { bin, config });
      console.log("Testing " + version);
    }
    assert(muxes.size, "Install at least one supported multiplexer");
    configure(muxes.keys().next().value);
    run(
      "go",
      [
        "build",
        ...(process.env.ONIBI_LIVE_RACE === "1" ? ["-race"] : []),
        "-o",
        binary,
        "./cmd/onibi",
      ],
      // Keep the normal Go build/module cache; isolate only runtime state.
      { cwd: root, env: process.env, timeout: 300000 },
    );
    await start();
    if (process.env.ONIBI_LIVE_WINDOWS === "1")
      await check("Windows-to-WSL localhost", windowsClient);
    assert.match(cliFailure("start"), /already running/);
    assert.match(
      cliFailure("session", "new", "onibi-nonexistent-program"),
      /not found/,
    );
    assert((await api("/api/targets")).sessionsOnly);
    assert.equal((await api("/api/health")).inventory.state, "not_applicable");
    assert.equal(
      (await request("/api/sessions", null, { Cookie: "" })).status,
      401,
    );
    assert.equal(
      (
        await api("/api/actions", {
          id: randomUUID(),
          action: { kind: "new_window" },
        })
      ).state,
      "rejected",
    );
    for (const kind of muxes.keys()) {
      await check(kind + " lifecycle", () => lifecycle(kind));
      await check(kind + " adoption", () => adoption(kind));
    }
    if (process.env.ONIBI_LIVE_BROWSER === "1")
      await check("real browser", () => realBrowser(muxes.keys().next().value));
    else
      console.log(
        "SKIP production browser: set ONIBI_LIVE_BROWSER=1 (npm ci and Playwright Chromium required)",
      );
  } catch (error) {
    failures++;
    console.error(error.stack);
  } finally {
    await browser?.close().catch(() => {});
    await stop().catch((error) => {
      failures++;
      console.error(error.message);
    });
    // Only exact targets in the private test namespaces are ever terminated.
    for (const target of targets.values()) killTarget(target);
    if (muxes.has("tmux"))
      run(muxes.get("tmux").bin, ["kill-server"], { allowFailure: true });
    if (/WARNING: DATA RACE/.test(log)) {
      failures++;
      console.error("Race detector reported a production data race");
    }
    if (failures)
      console.error("Production diagnostics (no pairing links):\n" + log);
    fs.rmSync(temp, { recursive: true, force: true });
  }
  console.log("Live Linux/WSL checks complete; failures=" + failures);
  process.exitCode = failures ? 1 : 0;
})();
