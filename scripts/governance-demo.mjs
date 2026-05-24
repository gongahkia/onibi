#!/usr/bin/env node
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const demoRoot = join(root, ".kelpclaw", "demo");
const apiToken = "kelpclaw-demo-token";
const port = await freePort();
const apiBaseUrl = `http://127.0.0.1:${port}`;
const demoEnv = {
  ...process.env,
  KELPCLAW_ADMIN_TOKEN: apiToken,
  KELPCLAW_API_TOKEN: apiToken,
  KELPCLAW_API_URL: apiBaseUrl,
  KELPCLAW_AUTH_SIGNING_SECRET: "kelpclaw-demo-signing-secret",
  KELPCLAW_WORKFLOW_STORE: "memory",
  KELPCLAW_SECRET_STORE: "memory",
  KELPCLAW_AGENT_RUN_STORE: "memory",
  KELPCLAW_ARTIFACT_STORE: join(demoRoot, "artifacts"),
  KELPCLAW_PLANNER_MODE: "deterministic",
  NANOCLAW_RUNNER: "mock",
  PORT: String(port)
};

await mkdir(demoRoot, { recursive: true });
const server = spawn(process.execPath, ["apps/api/dist/server.js"], {
  cwd: root,
  env: demoEnv,
  stdio: ["ignore", "pipe", "pipe"]
});
let serverLog = "";
server.stdout.on("data", (chunk) => {
  serverLog += String(chunk);
});
server.stderr.on("data", (chunk) => {
  serverLog += String(chunk);
});

try {
  await waitForHealth(apiBaseUrl);
  const settingsPath = join(demoRoot, "claude-settings.local.json");
  const policyPath = join(demoRoot, "deny-rm-rf.policy.yml");
  await writeFile(
    policyPath,
    [
      "rules:",
      "  - id: deny-rm-rf",
      '    when: tool == "Bash" && args.command =~ "^rm -rf"',
      "    action: deny",
      ""
    ].join("\n"),
    "utf8"
  );

  const verifier = await runCli([
    "verify-claude-code",
    "--settings",
    settingsPath,
    "--title",
    "KelpClaw Governance Demo"
  ]);
  await runCli(["policy", "--file", policyPath]);
  const deniedRun = await fetchJson("/api/agent-runs", {
    method: "POST",
    body: {
      sourceAgent: "claude-code",
      sessionId: "demo.policy-deny",
      title: "KelpClaw Policy Denial Demo"
    }
  });
  const denied = await fetchJson(`/api/agent-runs/${deniedRun.run.id}/events`, {
    method: "POST",
    expectedStatuses: [403],
    body: {
      hookEvent: "PreToolUse",
      toolName: "Bash",
      toolUseId: "toolu.demo.deny-rm-rf",
      args: { command: "rm -rf /tmp/kelpclaw-demo" },
      status: "pending"
    }
  });
  const promoted = await runCli([
    "promote",
    "--run-id",
    verifier.runId,
    "--skill-name",
    "KelpClaw Governance Demo",
    "--capability",
    "kelpclaw-governance-demo"
  ]);
  const tbom = await runCli(["tbom-export", verifier.runId]);
  const replay = await runCli(["cross-agent-replay-smoke"]);
  const otlp = await maybeRunOtlpSmoke();
  const summary = {
    ok:
      verifier.ok === true &&
      denied.error === "POLICY_DENIED" &&
      promoted.ok === true &&
      tbom.ok === true &&
      replay.ok === true,
    apiBaseUrl,
    artifactRoot: demoEnv.KELPCLAW_ARTIFACT_STORE,
    claudeVerifier: {
      runId: verifier.runId,
      auditValid: verifier.audit?.verification?.valid === true,
      settingsPath: verifier.settingsPath
    },
    policyDenial: {
      runId: deniedRun.run.id,
      status: "denied",
      eventId: denied.event?.id,
      reason: denied.message
    },
    promotion: {
      skillId: promoted.skill?.id,
      skillArtifact: promoted.artifacts?.skill,
      workflowArtifact: promoted.artifacts?.workflow,
      tbomArtifact: promoted.artifacts?.tbom
    },
    tbom: {
      version: tbom.tbom?.kelpclawTbomVersion,
      auditChainHead: tbom.tbom?.auditChainHead,
      tools: tbom.tbom?.tools
    },
    crossAgentReplay: {
      ok: replay.ok,
      agents: replay.agents,
      eventCount: replay.eventCount
    },
    otlp
  };
  console.log(JSON.stringify(summary, null, 2));
  process.exitCode = summary.ok ? 0 : 1;
} finally {
  server.kill("SIGTERM");
}

async function maybeRunOtlpSmoke() {
  if (
    !(
      demoEnv.KELPCLAW_OTLP_TRACES_ENDPOINT ||
      demoEnv.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT ||
      demoEnv.KELPCLAW_OTLP_ENDPOINT ||
      demoEnv.OTEL_EXPORTER_OTLP_ENDPOINT
    )
  ) {
    return { skipped: true, reason: "no OTLP endpoint configured" };
  }
  return runCli(["otlp-smoke"]);
}

async function runCli(args) {
  const result = await runNode(["packages/cli/dist/index.js", ...args]);
  return JSON.parse(result.stdout);
}

async function fetchJson(path, options = {}) {
  const response = await fetch(new URL(path, apiBaseUrl), {
    method: options.method ?? "GET",
    headers: {
      "content-type": "application/json",
      authorization: `Bearer ${apiToken}`
    },
    body: options.body ? JSON.stringify(options.body) : undefined
  });
  const payload = await response.json();
  const expectedStatuses = options.expectedStatuses ?? [200, 201, 202];
  if (!expectedStatuses.includes(response.status)) {
    throw new Error(
      `Request ${path} failed with HTTP ${response.status}: ${JSON.stringify(payload)}`
    );
  }
  return payload;
}

async function runNode(args) {
  return new Promise((resolveRun, rejectRun) => {
    const child = spawn(process.execPath, args, {
      cwd: root,
      env: demoEnv,
      stdio: ["ignore", "pipe", "pipe"]
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", (chunk) => {
      stdout += String(chunk);
    });
    child.stderr.on("data", (chunk) => {
      stderr += String(chunk);
    });
    child.on("error", rejectRun);
    child.on("exit", (code) => {
      if (code === 0) {
        resolveRun({ stdout, stderr });
      } else {
        rejectRun(new Error(`node ${args.join(" ")} exited ${code}: ${stderr || stdout}`));
      }
    });
  });
}

async function waitForHealth(baseUrl) {
  const deadline = Date.now() + 15_000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(new URL("/health", baseUrl));
      if (response.ok) {
        return;
      }
      lastError = new Error(`health returned HTTP ${response.status}`);
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 250));
  }
  throw new Error(
    `KelpClaw demo API did not become healthy: ${
      lastError instanceof Error ? lastError.message : String(lastError)
    }\n${serverLog.slice(-2_000)}`
  );
}

async function freePort() {
  return new Promise((resolvePort, rejectPort) => {
    const socket = createServer();
    socket.once("error", rejectPort);
    socket.listen(0, "127.0.0.1", () => {
      const address = socket.address();
      socket.close(() => {
        if (address && typeof address === "object") {
          resolvePort(address.port);
        } else {
          rejectPort(new Error("Could not allocate a local port."));
        }
      });
    });
  });
}
