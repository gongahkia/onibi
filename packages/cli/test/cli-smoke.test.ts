import { mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  compatibilityReport,
  exportAuditBundle,
  replayDiff,
  runCrossAgentReplaySmoke,
  runOtlpSmoke,
  runSkill,
  verifyClaudeCode
} from "../src/index.js";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  process.exitCode = undefined;
});

describe("kelp-claw smoke commands", () => {
  it("summarizes equivalent replay shapes across agent sources", () => {
    const result = runCrossAgentReplaySmoke();

    expect(result).toMatchObject({
      ok: true,
      skillName: "kelpclaw-replay-smoke",
      agents: ["claude-code", "codex-cli", "goose"],
      eventCount: 2,
      tools: ["Bash", "Read"]
    });
    expect(result.agentTags).toEqual([
      ["claude-code", "claude-code"],
      ["codex-cli", "codex-cli"],
      ["goose", "goose"]
    ]);
  });

  it("posts an OTLP trace with one span per smoke tool call", async () => {
    const requests: {
      readonly url: string;
      readonly headers: Record<string, string>;
      readonly body: Record<string, unknown>;
    }[] = [];
    vi.stubEnv("DD_API_KEY", "dd-test-key");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string | URL | Request, init?: RequestInit) => {
        requests.push({
          url: String(url),
          headers: init?.headers as Record<string, string>,
          body: JSON.parse(String(init?.body)) as Record<string, unknown>
        });
        return new Response("", { status: 202 });
      })
    );

    const result = await runOtlpSmoke([
      "--endpoint",
      "http://collector.test/v1/traces",
      "--header",
      "x-smoke=local",
      "--run-id",
      "agent-run.test",
      "--skill-id",
      "skill.test",
      "--promoted-at",
      "2026-05-24T00:00:00.000Z"
    ]);

    expect(result).toMatchObject({
      ok: true,
      endpoint: "http://collector.test/v1/traces",
      statusCode: 202,
      traceCount: 1,
      spanCount: 2,
      runId: "agent-run.test",
      skillId: "skill.test",
      headerNames: ["DD-API-KEY", "x-smoke"]
    });
    expect(requests).toHaveLength(1);
    expect(requests[0]?.headers).toMatchObject({
      "DD-API-KEY": "dd-test-key",
      "x-smoke": "local"
    });
    expect(spanNames(requests[0]?.body)).toEqual(["Bash PostToolUse", "Read PostToolUse"]);
  });

  it("verifies installed Claude Code hook settings and audit chain", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-cli-"));
    const settingsPath = join(tempDir, "settings.local.json");
    const requests: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string | URL | Request) => {
        requests.push(String(url));
        if (String(url).endsWith("/api/agent-runs")) {
          return jsonResponse({ ok: true, run: { id: "agent-run.verify" } }, 201);
        }
        if (String(url).endsWith("/events")) {
          return jsonResponse({ ok: true, event: { id: `agent-step.${requests.length}` } }, 201);
        }
        if (String(url).endsWith("/audit/verify")) {
          return jsonResponse({ ok: true, verification: { valid: true } }, 200);
        }
        return jsonResponse({ ok: false }, 404);
      })
    );

    try {
      const result = await verifyClaudeCode(["--settings", settingsPath]);
      const settings = JSON.parse(await readFile(settingsPath, "utf8")) as {
        readonly hooks?: Record<string, unknown>;
      };

      expect(result.ok).toBe(true);
      expect(result.settings).toMatchObject({ installed: true, eventCount: 12 });
      expect(Object.keys(settings.hooks ?? {})).toContain("PreToolUse");
      expect(requests).toHaveLength(4);
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("reports SKILL.md compatibility with detected tools and baseline policy", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-skill-"));
    const skillPath = join(tempDir, "SKILL.md");
    await writeFile(
      skillPath,
      `---
name: local-audit-skill
tools: [Bash, Read, Write]
---

# Local Audit Skill

Use Bash, Read, and Write to inspect and create files.
`,
      "utf8"
    );

    try {
      await expect(compatibilityReport([skillPath])).resolves.toEqual({
        runnable: true,
        toolsDetected: ["Bash", "Read", "Write"],
        requiredSecrets: [],
        network: "none",
        sandboxProfile: "safe-local",
        policyFindings: []
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("fetches GitHub shorthand skill references from raw GitHub", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string | URL | Request) => {
        expect(String(url)).toBe(
          "https://raw.githubusercontent.com/acme/skills/main/security/SKILL.md"
        );
        return new Response(
          `---
name: github-skill
tools:
  - WebFetch
---

# GitHub Skill

Fetch https://example.com/status.
`,
          { status: 200 }
        );
      })
    );

    await expect(
      compatibilityReport(["github:acme/skills/security/SKILL.md"])
    ).resolves.toMatchObject({
      runnable: true,
      toolsDetected: ["WebFetch"],
      network: "declared",
      sandboxProfile: "networked"
    });
  });

  it("runs a SKILL.md into local audit artifacts and exports a static bundle", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-run-skill-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const runsDir = join(tempDir, "runs");
    const bundleDir = join(tempDir, "bundle");
    await writeFile(
      skillPath,
      `---
name: bundle-skill
tools: [Read]
---

# Bundle Skill

Read a file and report a deterministic audit result.
`,
      "utf8"
    );
    await writeFile(inputPath, '{"path":"README.md"}\n', "utf8");

    try {
      const run = await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--run-id",
        "skill-run.test",
        "--runs-dir",
        runsDir
      ]);
      expect(run).toMatchObject({
        ok: true,
        runId: "skill-run.test",
        status: "succeeded"
      });
      await expect(
        readFile(join(runsDir, "skill-run.test", "audit.jsonl"), "utf8")
      ).resolves.toContain("skill.run.completed");

      const bundle = await exportAuditBundle([
        "skill-run.test",
        "--runs-dir",
        runsDir,
        "--out",
        bundleDir
      ]);
      expect(bundle.files).toEqual([
        "skill.json",
        "workflow.json",
        "bom.json",
        "audit.jsonl",
        "policy-decisions.json",
        "compatibility.json",
        "result.json",
        "index.html"
      ]);
      await expect(readFile(join(bundleDir, "index.html"), "utf8")).resolves.toContain(
        "KelpClaw Audit Bundle"
      );
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("diffs deterministic replay shape across requested agents", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-replay-diff-"));
    const skillPath = join(tempDir, "SKILL.md");
    await writeFile(
      skillPath,
      `---
name: replay-skill
tools: [Bash, Read]
---

# Replay Skill

\`\`\`bash
printf "kelpclaw\\n"
\`\`\`
`,
      "utf8"
    );

    try {
      const result = await replayDiff([
        "--skill",
        skillPath,
        "--agents",
        "claude-code,codex-cli,goose"
      ]);
      expect(result).toMatchObject({
        ok: true,
        agents: ["claude-code", "codex-cli", "goose"],
        same: {
          toolSequence: true,
          normalizedHashes: true,
          outputs: true,
          policyDecisions: true
        },
        differences: []
      });
      expect(result.runs[0]?.tools).toEqual(["Bash", "Read"]);
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("runs a SKILL.md through a configured live agent command", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-live-skill-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const fakeAgentPath = join(tempDir, "fake-agent.mjs");
    const runsDir = join(tempDir, "runs");
    await writeFile(
      skillPath,
      `---
name: live-skill
tools: [Bash, Write]
---

# Live Skill

Create a generated artifact from input.
`,
      "utf8"
    );
    await writeFile(inputPath, '{"message":"hello"}\n', "utf8");
    await writeFile(
      fakeAgentPath,
      `import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
let stdin = "";
process.stdin.on("data", chunk => stdin += chunk);
process.stdin.on("end", () => {
  mkdirSync("artifacts", { recursive: true });
  writeFileSync(join("artifacts", "result.txt"), stdin.includes("hello") ? "hello" : "missing");
  console.log(JSON.stringify({ toolName: "Bash", args: { command: "printf hello" }, result: { exitCode: 0 } }));
  console.log(JSON.stringify({ toolName: "Write", args: { filePath: "artifacts/result.txt" }, result: { bytes: 5 } }));
});
`,
      "utf8"
    );

    try {
      const run = await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--agent",
        "codex-cli",
        "--agent-command",
        process.execPath,
        "--agent-arg",
        fakeAgentPath,
        "--run-id",
        "skill-run.live",
        "--runs-dir",
        runsDir
      ]);
      expect(run).toMatchObject({
        ok: true,
        runId: "skill-run.live",
        status: "succeeded",
        mode: "live",
        agent: "codex-cli"
      });
      const agentRun = JSON.parse(
        await readFile(join(runsDir, "skill-run.live", "agent-run.json"), "utf8")
      ) as {
        readonly observedSteps?: readonly { readonly tool?: string }[];
        readonly generatedArtifacts?: readonly string[];
      };
      expect(agentRun.observedSteps?.map((step) => step.tool)).toEqual(["Bash", "Write"]);
      expect(agentRun.generatedArtifacts).toEqual(["result.txt"]);
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("blocks planned policy denials before invoking a live agent", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-planned-block-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const fakeAgentPath = join(tempDir, "fake-agent.mjs");
    const runsDir = join(tempDir, "runs");
    await writeFile(
      skillPath,
      `---
name: planned-block
tools: [Bash]
---

# Planned Block

\`\`\`bash
rm -rf /tmp/kelpclaw-planned-block
\`\`\`
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(
      fakeAgentPath,
      `import { writeFileSync } from "node:fs";
writeFileSync("should-not-run.txt", "ran");
`,
      "utf8"
    );

    try {
      const run = await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--agent",
        "codex-cli",
        "--agent-command",
        process.execPath,
        "--agent-arg",
        fakeAgentPath,
        "--run-id",
        "skill-run.planned-block",
        "--runs-dir",
        runsDir,
        "--enforce-policy"
      ]);
      expect(run).toMatchObject({
        ok: false,
        status: "blocked",
        mode: "live"
      });
      await expect(
        readFile(join(runsDir, "skill-run.planned-block", "agent-run.json"), "utf8")
      ).rejects.toThrow();
      expect(process.exitCode).toBe(1);
    } finally {
      process.exitCode = undefined;
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("blocks live execution when a PreToolUse hook is denied", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-hook-block-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const fakeAgentPath = join(tempDir, "fake-agent.mjs");
    const runsDir = join(tempDir, "runs");
    await writeFile(
      skillPath,
      `---
name: hook-block
tools: [Bash]
---

# Hook Block

Run a shell command chosen at runtime.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(
      fakeAgentPath,
      `import { mkdirSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
const hook = process.env.KELPCLAW_SKILL_HOOK_COMMAND;
const pre = spawnSync(hook, {
  input: JSON.stringify({ hookEvent: "PreToolUse", toolName: "Bash", args: { command: "rm -rf /tmp/unsafe" } }),
  shell: true,
  encoding: "utf8"
});
if (pre.status !== 0) {
  process.exit(pre.status ?? 1);
}
mkdirSync("artifacts", { recursive: true });
writeFileSync("artifacts/unsafe.txt", "should not exist");
`,
      "utf8"
    );

    try {
      const run = await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--agent",
        "codex-cli",
        "--agent-command",
        process.execPath,
        "--agent-arg",
        fakeAgentPath,
        "--run-id",
        "skill-run.hook-block",
        "--runs-dir",
        runsDir,
        "--enforce-policy"
      ]);
      expect(run).toMatchObject({
        ok: false,
        status: "blocked",
        mode: "live"
      });
      const agentRun = JSON.parse(
        await readFile(join(runsDir, "skill-run.hook-block", "agent-run.json"), "utf8")
      ) as {
        readonly hookEvents?: readonly { readonly status?: string; readonly hookEvent?: string }[];
        readonly enforcement?: { readonly hookBlocked?: boolean; readonly source?: string };
        readonly generatedArtifacts?: readonly string[];
      };
      expect(agentRun.enforcement).toMatchObject({
        hookBlocked: true,
        source: "hook-pretool"
      });
      expect(agentRun.hookEvents).toEqual([
        expect.objectContaining({ hookEvent: "PreToolUse", status: "denied" })
      ]);
      expect(agentRun.generatedArtifacts).toEqual([]);
    } finally {
      process.exitCode = undefined;
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("records replay-diff by running configured agent commands", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-recorded-replay-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const fakeAgentPath = join(tempDir, "fake-agent.mjs");
    const runsDir = join(tempDir, "runs");
    await writeFile(
      skillPath,
      `---
name: recorded-replay-skill
tools: [Bash]
---

# Recorded Replay Skill

Run a deterministic shell-shaped operation.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(
      fakeAgentPath,
      `process.stdin.resume();
process.stdin.on("end", () => {
  console.log(JSON.stringify({ toolName: "Bash", args: { command: "printf replay" }, result: { stdout: "replay" } }));
});
`,
      "utf8"
    );

    try {
      const result = await replayDiff([
        "--recorded",
        "--skill",
        skillPath,
        "--input",
        inputPath,
        "--agents",
        "codex-cli,custom-agent",
        "--agent-command",
        process.execPath,
        "--agent-arg",
        fakeAgentPath,
        "--runs-dir",
        runsDir
      ]);
      expect(result).toMatchObject({
        ok: true,
        agents: ["codex-cli", "custom-agent"],
        same: {
          toolSequence: true,
          normalizedHashes: true,
          outputs: true,
          policyDecisions: true
        },
        differences: []
      });
      expect(result.runs.map((run) => run.tools)).toEqual([["Bash"], ["Bash"]]);
      expect(result.runs.every((run) => run.runId?.startsWith("replay-diff."))).toBe(true);
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("uses hook events as recorded replay canonical tool sequence", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-hook-replay-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const fakeAgentPath = join(tempDir, "fake-agent.mjs");
    const runsDir = join(tempDir, "runs");
    await writeFile(
      skillPath,
      `---
name: hook-replay
tools: [Read]
---

# Hook Replay

Read a runtime-selected file.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(
      fakeAgentPath,
      `import { spawnSync } from "node:child_process";
const hook = process.env.KELPCLAW_SKILL_HOOK_COMMAND;
spawnSync(hook, {
  input: JSON.stringify({ hookEvent: "PreToolUse", toolName: "Read", args: { filePath: "input.json" } }),
  shell: true,
  encoding: "utf8"
});
spawnSync(hook, {
  input: JSON.stringify({ hookEvent: "PostToolUse", toolName: "Read", args: { filePath: "input.json" }, result: { content: "{}" } }),
  shell: true,
  encoding: "utf8"
});
console.log(JSON.stringify({ toolName: "Bash", args: { command: "printf ignored" } }));
`,
      "utf8"
    );

    try {
      const result = await replayDiff([
        "--recorded",
        "--skill",
        skillPath,
        "--input",
        inputPath,
        "--agents",
        "codex-cli,custom-agent",
        "--agent-command",
        process.execPath,
        "--agent-arg",
        fakeAgentPath,
        "--runs-dir",
        runsDir
      ]);
      expect(result.ok).toBe(true);
      expect(result.runs.map((run) => run.tools)).toEqual([["Read"], ["Read"]]);
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("keeps the public SKILL.md compatibility corpus stable", async () => {
    const corpusRoot = join(process.cwd(), "../../fixtures/skills-corpus");
    const entries = await readdir(corpusRoot, { withFileTypes: true });
    const skillDirs = entries.filter((entry) => entry.isDirectory()).map((entry) => entry.name);
    expect(skillDirs.sort()).toEqual([
      "destructive-shell",
      "github-pr-review",
      "local-file-audit",
      "network-health-check"
    ]);

    for (const skillDir of skillDirs) {
      const report = await compatibilityReport([join(corpusRoot, skillDir, "SKILL.md")]);
      const expected = JSON.parse(
        await readFile(join(corpusRoot, skillDir, "expected.baseline.json"), "utf8")
      ) as unknown;
      expect(report).toEqual(expected);
    }
  });
});

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" }
  });
}

function spanNames(body: Record<string, unknown> | undefined): readonly string[] {
  const resourceSpans = Array.isArray(body?.resourceSpans) ? body.resourceSpans : [];
  const firstResource = resourceSpans[0] as { readonly scopeSpans?: unknown } | undefined;
  const scopeSpans = Array.isArray(firstResource?.scopeSpans) ? firstResource.scopeSpans : [];
  const firstScope = scopeSpans[0] as { readonly spans?: unknown } | undefined;
  const spans = Array.isArray(firstScope?.spans) ? firstScope.spans : [];
  return spans
    .map((span) =>
      span && typeof span === "object" && "name" in span ? String(span.name) : undefined
    )
    .filter((name): name is string => Boolean(name));
}
