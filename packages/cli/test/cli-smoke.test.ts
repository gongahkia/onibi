import { appendFile, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  compatibilityReport,
  exportAuditBundle,
  exportSarif,
  governanceControls,
  governanceReport,
  initAuditKey,
  inventoryCoverage,
  inventoryGraph,
  inventoryScan,
  policyExplain,
  replayDiff,
  runDemoCommand,
  runEvidenceCommand,
  runDoctorCommand,
  runHelpCommand,
  runWebCommand,
  runCrossAgentReplaySmoke,
  runOtlpSmoke,
  runSkill,
  verifyAuditBundle,
  verifyClaudeCode
} from "../src/index.js";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  process.exitCode = undefined;
});

describe("kelp-claw smoke commands", () => {
  it("reports CLI help and local readiness checks for product demos", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-doctor-"));

    try {
      const help = runHelpCommand();
      expect(help).toMatchObject({
        ok: true,
        name: "kelp-claw",
        usage: "kelp-claw <command> [options]"
      });
      expect(help.commands).toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            group: "adoption",
            entries: expect.arrayContaining(["doctor", "demo governance"])
          })
        ])
      );

      const doctor = await runDoctorCommand([
        "--root",
        tempDir,
        "--codex-bin",
        join(tempDir, "missing-codex")
      ]);
      expect(doctor).toMatchObject({
        ok: true,
        root: tempDir,
        checks: expect.arrayContaining([
          expect.objectContaining({ id: "node-version", status: "pass" }),
          expect.objectContaining({ id: "workspace-writable", status: "pass" }),
          expect.objectContaining({ id: "policy-pack:sg-agentic-ai-baseline", status: "pass" }),
          expect.objectContaining({ id: "codex-cli", status: "warn", required: false })
        ])
      });
      expect(doctor.recommendations).toEqual(
        expect.arrayContaining(["Install or pass --codex-bin for live Codex CLI wrapper demos."])
      );
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

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
    const keyDir = join(tempDir, "keys");
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
        bundleDir,
        "--key-dir",
        keyDir
      ]);
      expect(bundle.files).toEqual([
        "skill.json",
        "workflow.json",
        "bom.json",
        "audit.jsonl",
        "policy-decisions.json",
        "compatibility.json",
        "result.json",
        "index.html",
        "manifest.json",
        "manifest.sig",
        "manifest.pub.json",
        "attestation.json",
        "attestation.sig"
      ]);
      expect(bundle).toMatchObject({ signed: true, manifest: "manifest.json" });
      await expect(readFile(join(bundleDir, "index.html"), "utf8")).resolves.toContain(
        "KelpClaw Audit Bundle"
      );
      await expect(verifyAuditBundle([bundleDir])).resolves.toMatchObject({
        ok: true,
        runId: "skill-run.test",
        strict: false,
        signature: { valid: true, algorithm: "ed25519" },
        files: { checked: 8, failed: [] },
        failures: []
      });
      await expect(verifyAuditBundle([bundleDir, "--strict"])).resolves.toMatchObject({
        ok: true,
        strict: true,
        attestation: {
          valid: true,
          signed: true,
          manifestHash: expect.stringMatching(/^sha256:/u),
          referencedFiles: expect.arrayContaining(["audit.jsonl", "policy-decisions.json"])
        }
      });
      await appendFile(join(bundleDir, "audit.jsonl"), '{"tampered":true}\n', "utf8");
      await expect(verifyAuditBundle([bundleDir])).resolves.toMatchObject({
        ok: false,
        strict: false,
        signature: { valid: true },
        files: { failed: ["audit.jsonl"] }
      });
    } finally {
      process.exitCode = undefined;
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("includes governance reports in signed audit bundles when requested", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-governance-bundle-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const runsDir = join(tempDir, "runs");
    const bundleDir = join(tempDir, "bundle");
    await writeFile(
      skillPath,
      `---
name: governance-bundle
tools: [Read]
---

# Governance Bundle

Read a local file and summarize it.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");

    try {
      await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--run-id",
        "skill-run.governance-bundle",
        "--runs-dir",
        runsDir
      ]);
      const bundle = await exportAuditBundle([
        "skill-run.governance-bundle",
        "--runs-dir",
        runsDir,
        "--out",
        bundleDir,
        "--include-governance",
        "--region",
        "sg",
        "--framework",
        "agentic-ai"
      ]);
      expect(bundle.files).toContain("governance-report.json");
      expect(bundle.files).toContain("governance-report.html");
      await expect(readFile(join(bundleDir, "governance-report.html"), "utf8")).resolves.toContain(
        "KelpClaw Governance Report"
      );
      await expect(verifyAuditBundle([bundleDir])).resolves.toMatchObject({
        ok: true,
        signature: { valid: true }
      });
      const manifest = JSON.parse(await readFile(join(bundleDir, "manifest.json"), "utf8")) as {
        readonly files?: readonly { readonly path?: string }[];
      };
      expect(manifest.files?.map((file) => file.path)).toContain("governance-report.json");
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("exports SARIF, controls, and strict bundle attestations for security review", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-security-review-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const runsDir = join(tempDir, "runs");
    const bundleDir = join(tempDir, "bundle");
    const controlsPath = join(tempDir, "controls.md");
    const sarifPath = join(tempDir, "findings.sarif");
    await writeFile(
      skillPath,
      `---
name: security-review
tools: [Bash]
---

# Security Review

\`\`\`bash
rm -rf /tmp/kelpclaw-security-review
\`\`\`
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");

    try {
      const sarif = await exportSarif([skillPath, "--policy", "baseline", "--out", sarifPath]);
      expect(sarif).toMatchObject({
        ok: true,
        out: sarifPath,
        resultCount: expect.any(Number)
      });
      expect(sarif.resultCount).toBeGreaterThan(0);
      expect(sarif.sarif).toMatchObject({
        version: "2.1.0",
        runs: [
          {
            results: expect.arrayContaining([
              expect.objectContaining({
                ruleId: "kelp.policy.baseline-deny-destructive-shell",
                level: "error"
              })
            ])
          }
        ]
      });
      await expect(readFile(sarifPath, "utf8")).resolves.toContain(
        "baseline-deny-destructive-shell"
      );

      const controls = await governanceControls([
        skillPath,
        "--policy",
        "baseline",
        "--out",
        controlsPath
      ]);
      expect(controls).toMatchObject({
        ok: false,
        out: controlsPath,
        controlCount: expect.any(Number)
      });
      await expect(readFile(controlsPath, "utf8")).resolves.toContain(
        "| Control Area | Status | Evidence Files | Residual Risk | Reviewer Action |"
      );

      await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--run-id",
        "skill-run.security-review",
        "--runs-dir",
        runsDir,
        "--policy",
        "no-destructive-shell"
      ]);
      const bundle = await exportAuditBundle([
        "skill-run.security-review",
        "--runs-dir",
        runsDir,
        "--out",
        bundleDir,
        "--include-governance",
        "--include-sarif",
        "--include-controls"
      ]);
      expect(bundle.files).toEqual(
        expect.arrayContaining([
          "governance-report.json",
          "findings.sarif",
          "controls.md",
          "attestation.json",
          "attestation.sig"
        ])
      );
      await expect(verifyAuditBundle([bundleDir, "--strict"])).resolves.toMatchObject({
        ok: true,
        attestation: {
          valid: true,
          referencedFiles: expect.arrayContaining(["findings.sarif", "controls.md"])
        }
      });

      const attestation = JSON.parse(
        await readFile(join(bundleDir, "attestation.json"), "utf8")
      ) as Record<string, unknown>;
      await writeFile(
        join(bundleDir, "attestation.json"),
        `${JSON.stringify({ ...attestation, runId: "tampered" }, null, 2)}\n`,
        "utf8"
      );
      await expect(verifyAuditBundle([bundleDir, "--strict"])).resolves.toMatchObject({
        ok: false,
        attestation: { valid: false },
        failures: expect.arrayContaining([
          "attestation signature is invalid.",
          "attestation runId does not match manifest runId."
        ])
      });
    } finally {
      process.exitCode = undefined;
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("runs governed web search and writes portable evidence", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-web-command-"));
    const outDir = join(tempDir, "web-evidence");
    const requests: string[] = [];
    vi.stubEnv("EXA_API_KEY", "exa-test-key");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string | URL | Request) => {
        requests.push(String(url));
        return jsonResponse(
          {
            results: [
              {
                title: "Singapore AI governance",
                url: "https://example.test/sg-ai",
                text: "Evidence for governed search."
              }
            ]
          },
          200
        );
      })
    );

    try {
      const result = await runWebCommand([
        "search",
        "Singapore AI governance",
        "--provider",
        "exa",
        "--domain",
        "example.test",
        "--out",
        outDir
      ]);

      expect(result).toMatchObject({
        ok: true,
        status: "succeeded",
        policyPack: "web-search-safe",
        toolName: "exa.search",
        files: ["web-evidence.json", "web-events.jsonl", "web-bom.json", "web-evidence.html"]
      });
      expect(requests).toEqual(["https://api.exa.ai/search"]);
      await expect(readFile(join(outDir, "web-evidence.json"), "utf8")).resolves.toContain(
        "Singapore AI governance"
      );
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("blocks governed web work when policy requires approval", async () => {
    const fetchImpl = vi.fn();
    vi.stubGlobal("fetch", fetchImpl);

    const result = await runWebCommand([
      "search",
      "agentic ai governance",
      "--provider",
      "exa",
      "--store-full-content"
    ]);

    expect(result).toMatchObject({
      ok: false,
      status: "blocked",
      policyPack: "web-search-safe",
      toolName: "exa.search",
      decision: {
        action: "require-approval",
        matchedRuleIds: ["web-search-safe-review-full-content-storage"]
      }
    });
    expect(fetchImpl).not.toHaveBeenCalled();
    expect(process.exitCode).toBe(1);
  });

  it("includes web evidence in governance reports and audit bundles", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-web-evidence-bundle-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const runsDir = join(tempDir, "runs");
    const webDir = join(tempDir, "web");
    const bundleDir = join(tempDir, "bundle");
    vi.stubEnv("EXA_API_KEY", "exa-test-key");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse(
          {
            results: [
              {
                title: "MAS source",
                url: "https://mas.gov.sg/example",
                text: "Regulatory evidence."
              }
            ]
          },
          200
        )
      )
    );
    await writeFile(
      skillPath,
      `---
name: web-governance
tools: [Read]
---

# Web Governance

Read local inputs and cite web evidence.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");

    try {
      await runWebCommand([
        "search",
        "Singapore AI governance",
        "--provider",
        "exa",
        "--out",
        webDir
      ]);
      await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--run-id",
        "skill-run.web-evidence",
        "--runs-dir",
        runsDir
      ]);
      const report = await governanceReport([
        skillPath,
        "--include-web-evidence",
        webDir,
        "--region",
        "sg"
      ]);
      expect(report).toMatchObject({
        controls: { webEvidence: true },
        webEvidence: {
          sourceCount: 1,
          providers: ["exa"],
          domains: ["mas.gov.sg"]
        }
      });

      const bundle = await exportAuditBundle([
        "skill-run.web-evidence",
        "--runs-dir",
        runsDir,
        "--out",
        bundleDir,
        "--include-web-evidence",
        webDir,
        "--include-governance"
      ]);
      expect(bundle.files).toEqual(
        expect.arrayContaining([
          "web-evidence.json",
          "web-events.jsonl",
          "web-bom.json",
          "web-evidence.html",
          "governance-report.json",
          "governance-report.html"
        ])
      );
      await expect(verifyAuditBundle([bundleDir])).resolves.toMatchObject({
        ok: true,
        signature: { valid: true }
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("includes Piranesi-derived evidence workspaces in governance, bundles, and inventory", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-evidence-cli-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const runsDir = join(tempDir, ".kelpclaw", "runs");
    const evidenceDir = join(tempDir, ".kelpclaw", "evidence");
    const bundleDir = join(tempDir, ".kelpclaw", "audit-bundles", "skill-run.evidence");
    const sarifPath = join(tempDir, "findings.sarif");
    await writeFile(
      skillPath,
      `---
name: evidence-governance
tools: [Read]
---

# Evidence Governance

Read local evidence and summarize findings.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(sarifPath, `${JSON.stringify(cliSarifFixture(), null, 2)}\n`, "utf8");

    try {
      await expect(
        runEvidenceCommand([
          "init",
          "--workspace",
          evidenceDir,
          "--client",
          "Example Client",
          "--project",
          "Agent Governance"
        ])
      ).resolves.toMatchObject({ ok: true, workspace: evidenceDir });
      await expect(
        runEvidenceCommand(["import-sarif", sarifPath, "--workspace", evidenceDir])
      ).resolves.toMatchObject({
        ok: true,
        importedFindings: 1
      });
      await expect(runEvidenceCommand(["sign", "--workspace", evidenceDir])).resolves.toMatchObject(
        {
          ok: true,
          manifest: { manifestId: expect.stringMatching(/^sha256:/u) }
        }
      );
      await expect(
        runEvidenceCommand(["verify", "--workspace", evidenceDir])
      ).resolves.toMatchObject({
        ok: true,
        failures: []
      });

      await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--run-id",
        "skill-run.evidence",
        "--runs-dir",
        runsDir
      ]);
      const report = await governanceReport([
        "skill-run.evidence",
        "--runs-dir",
        runsDir,
        "--include-evidence",
        evidenceDir
      ]);
      expect(report).toMatchObject({
        controls: { evidenceWorkspace: true },
        evidenceWorkspace: {
          findingCount: 1,
          signed: true,
          verified: true,
          sourceReferenceGaps: 0
        }
      });

      const bundle = await exportAuditBundle([
        "skill-run.evidence",
        "--runs-dir",
        runsDir,
        "--out",
        bundleDir,
        "--include-evidence",
        evidenceDir,
        "--include-governance"
      ]);
      expect(bundle.files).toEqual(
        expect.arrayContaining([
          "evidence-summary.json",
          "evidence-workspace/workspace.json",
          "evidence-workspace/evidence/index.json",
          "evidence-workspace/normalized/findings.json",
          "evidence-workspace/index.html"
        ])
      );
      await expect(verifyAuditBundle([bundleDir, "--strict"])).resolves.toMatchObject({
        ok: true,
        attestation: {
          referencedFiles: expect.arrayContaining(["evidence-summary.json"])
        }
      });

      const inventory = await inventoryScan(["--root", tempDir]);
      expect(inventory.evidenceWorkspaces).toEqual([
        expect.objectContaining({
          path: ".kelpclaw/evidence",
          findingCount: 1,
          signed: true,
          verified: true
        })
      ]);
      expect(inventory.permissionEdges).toEqual(
        expect.arrayContaining([
          {
            source: "skill:SKILL.md",
            target: "evidence-workspace:.kelpclaw/evidence",
            kind: "has-evidence-workspace"
          }
        ])
      );
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("runs the one-command governance demo into a portable audit handoff", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-demo-governance-"));
    const outDir = join(tempDir, "demo");

    try {
      const demo = await runDemoCommand([
        "governance",
        "--out",
        outDir,
        "--run-id",
        "skill-run.demo-test"
      ]);
      expect(demo).toMatchObject({
        ok: true,
        outDir,
        runId: "skill-run.demo-test",
        policy: "sg-agentic-ai-baseline",
        evidence: {
          importedFindings: 1,
          verified: true
        },
        verification: {
          ok: true,
          strict: true
        }
      });
      await expect(readFile(join(outDir, "audit-bundle", "index.html"), "utf8")).resolves.toContain(
        "KelpClaw Audit Bundle"
      );
      await expect(
        readFile(join(outDir, "audit-bundle", "evidence-workspace", "index.html"), "utf8")
      ).resolves.toContain("KelpClaw Evidence Workspace");
      await expect(
        readFile(join(outDir, "audit-bundle", "governance-report.json"), "utf8")
      ).resolves.toContain("demo-governance-skill");
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("scans agent inventory, renders permission graphs, and checks evidence coverage", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-inventory-"));
    const skillDir = join(tempDir, "skills", "network");
    const githubDir = join(tempDir, ".github", "workflows");
    const docsDir = join(tempDir, "docs");
    const skillPath = join(skillDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const runsDir = join(tempDir, ".kelpclaw", "runs");
    const bundlesRoot = join(tempDir, ".kelpclaw", "audit-bundles");
    const bundleDir = join(bundlesRoot, "skill-run.inventory");
    const webDir = join(tempDir, ".kelpclaw", "web-evidence", "sg");
    const graphPath = join(tempDir, "permissions.md");
    const coveragePath = join(tempDir, "coverage.md");
    vi.stubEnv("EXA_API_KEY", "exa-test-key");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonResponse(
          {
            results: [
              {
                title: "SG AI evidence",
                url: "https://example.test/sg-ai",
                text: "Governed web evidence."
              }
            ]
          },
          200
        )
      )
    );
    await mkdir(skillDir, { recursive: true });
    await mkdir(githubDir, { recursive: true });
    await mkdir(docsDir, { recursive: true });
    await writeFile(
      skillPath,
      `---
name: inventory-network-skill
tools: [Read, WebFetch]
---

# Inventory Network Skill

Read local inputs and fetch https://example.test/sg-ai as governed evidence.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(
      join(githubDir, "kelpclaw.yml"),
      `name: KelpClaw
on: [pull_request]
jobs:
  audit:
    runs-on: ubuntu-latest
    steps:
      - uses: gongahkia/kelp-claw/.github/actions/audit-skill@main
        with:
          mode: inventory
          upload-sarif: true
`,
      "utf8"
    );
    await writeFile(
      join(docsDir, "mcp.md"),
      "Run `kelp-claw mcp web-gateway --policy sg-web-research --allow-browser-tools` for governed web tools.\n",
      "utf8"
    );

    try {
      await runWebCommand([
        "search",
        "Singapore agentic AI governance",
        "--provider",
        "exa",
        "--out",
        webDir
      ]);
      await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--run-id",
        "skill-run.inventory",
        "--runs-dir",
        runsDir,
        "--policy",
        "sg-agentic-ai-baseline"
      ]);
      await exportAuditBundle([
        "skill-run.inventory",
        "--runs-dir",
        runsDir,
        "--out",
        bundleDir,
        "--key-dir",
        join(tempDir, "keys"),
        "--include-controls",
        "--include-sarif"
      ]);

      const inventory = await inventoryScan([
        "--root",
        tempDir,
        "--policy",
        "sg-agentic-ai-baseline"
      ]);
      expect(inventory).toMatchObject({
        ok: true,
        policyPack: "sg-agentic-ai-baseline",
        skills: [
          expect.objectContaining({
            path: "skills/network/SKILL.md",
            toolsDetected: ["Read", "WebFetch"],
            network: "declared",
            runnable: true
          })
        ],
        runs: [expect.objectContaining({ runId: "skill-run.inventory" })],
        bundles: [
          expect.objectContaining({
            runId: "skill-run.inventory",
            hasSignature: true,
            hasAttestation: true,
            hasSarif: true,
            hasControls: true
          })
        ],
        webEvidence: [expect.objectContaining({ provider: "exa", sourceCount: 1 })],
        githubActions: [
          expect.objectContaining({
            usesAuditSkill: true,
            uploadsSarif: true,
            hasInventoryMode: true
          })
        ],
        mcpGateways: [
          expect.objectContaining({
            policy: "sg-web-research",
            allowsBrowserTools: true
          })
        ]
      });
      expect(inventory.permissionEdges).toEqual(
        expect.arrayContaining([
          {
            source: "skill:skills/network/SKILL.md",
            target: "tool:WebFetch",
            kind: "uses-tool"
          },
          {
            source: "run:skill-run.inventory",
            target: "bundle:.kelpclaw/audit-bundles/skill-run.inventory",
            kind: "exported-as"
          },
          {
            source: "bundle:.kelpclaw/audit-bundles/skill-run.inventory",
            target: "attestation:ed25519",
            kind: "signed-by"
          }
        ])
      );
      expect(inventory.coverageFindings).toEqual([]);

      const graph = await inventoryGraph([
        "--root",
        tempDir,
        "--format",
        "markdown",
        "--out",
        graphPath
      ]);
      expect(graph).toMatchObject({
        ok: true,
        format: "markdown",
        out: graphPath
      });
      expect(graph.content).toContain("# KelpClaw Permission Graph");
      await expect(readFile(graphPath, "utf8")).resolves.toContain("skill:skills/network/SKILL.md");

      const coverage = await inventoryCoverage([
        "--root",
        tempDir,
        "--format",
        "markdown",
        "--out",
        coveragePath,
        "--fail-on",
        "none"
      ]);
      expect(coverage).toMatchObject({
        ok: true,
        format: "markdown",
        findingCount: 0,
        summary: { high: 0, moderate: 0, info: 0 },
        out: coveragePath
      });
      await expect(readFile(coveragePath, "utf8")).resolves.toContain(
        "# KelpClaw Inventory Coverage"
      );
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("fails inventory coverage on high-severity policy gaps when requested", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-inventory-fail-"));
    const skillDir = join(tempDir, "skills", "destructive");
    const skillPath = join(skillDir, "SKILL.md");
    await mkdir(skillDir, { recursive: true });
    await writeFile(
      skillPath,
      `---
name: destructive-inventory-skill
tools: [Bash]
---

# Destructive Inventory Skill

\`\`\`bash
rm -rf /tmp/kelpclaw-inventory-fail
\`\`\`
`,
      "utf8"
    );

    try {
      const coverage = await inventoryCoverage([
        "--root",
        tempDir,
        "--policy",
        "baseline",
        "--fail-on",
        "high"
      ]);
      expect(coverage.ok).toBe(false);
      expect(coverage.summary.high).toBeGreaterThan(0);
      expect(coverage.findings).toEqual(
        expect.arrayContaining([
          expect.objectContaining({
            severity: "high",
            category: "policy"
          })
        ])
      );
      expect(process.exitCode).toBe(1);
    } finally {
      process.exitCode = undefined;
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("exposes repository inventory mode in the GitHub Action", async () => {
    const action = await readFile(
      join(process.cwd(), "../../.github/actions/audit-skill/action.yml"),
      "utf8"
    );

    expect(action).toContain("mode:");
    expect(action).toContain("inventory scan");
    expect(action).toContain("inventory graph");
    expect(action).toContain("inventory coverage");
    expect(action).toContain("fail-on-coverage");
    expect(action).toContain("always() && inputs.upload-artifact");
  });

  it("initializes an audit signing key and explains policy decisions", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-policy-explain-"));
    const skillPath = join(tempDir, "SKILL.md");
    await writeFile(
      skillPath,
      `---
name: explain-skill
tools: [Bash]
---

# Explain Skill

\`\`\`bash
rm -rf /tmp/kelpclaw-explain
\`\`\`
`,
      "utf8"
    );

    try {
      await expect(initAuditKey(["--key-dir", join(tempDir, "keys")])).resolves.toMatchObject({
        ok: true,
        algorithm: "ed25519",
        keyId: expect.stringMatching(/^sha256:/u)
      });
      const explanation = await policyExplain([skillPath, "--policy", "baseline"]);
      expect(explanation).toMatchObject({
        ok: false,
        policyPack: "baseline",
        compatibility: { runnable: false },
        summary: { totalSteps: 1, denied: 1 }
      });
      expect(explanation.plannedSteps[0]).toMatchObject({
        index: 0,
        tool: "Bash",
        decision: {
          action: "deny",
          matchedRuleIds: ["baseline-deny-destructive-shell", "baseline-log-shell"]
        }
      });
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("generates SG agentic AI governance reports for static skills", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-governance-static-"));
    const lowSkillPath = join(tempDir, "low.SKILL.md");
    const destructiveSkillPath = join(tempDir, "destructive.SKILL.md");
    const piiSkillPath = join(tempDir, "pii.SKILL.md");
    await writeFile(
      lowSkillPath,
      `---
name: low-risk
tools: [Read]
---

# Low Risk

Read a local file and summarize it.
`,
      "utf8"
    );
    await writeFile(
      destructiveSkillPath,
      `---
name: destructive-risk
tools: [Bash]
---

# Destructive Risk

\`\`\`bash
rm -rf /tmp/kelpclaw-risk
\`\`\`
`,
      "utf8"
    );
    await writeFile(
      piiSkillPath,
      `---
name: pii-write
tools: [Write]
---

# PII Write

Write a customer email report.
`,
      "utf8"
    );

    try {
      await expect(
        governanceReport([
          lowSkillPath,
          "--region",
          "sg",
          "--framework",
          "agentic-ai",
          "--policy",
          "sg-agentic-ai-baseline"
        ])
      ).resolves.toMatchObject({
        ok: true,
        region: "sg",
        framework: "agentic-ai",
        subject: { kind: "skill", name: "low-risk" },
        autonomyTier: "low",
        controls: { auditTrail: false, signedBundle: false }
      });
      await expect(
        governanceReport([destructiveSkillPath, "--policy", "sg-agentic-ai-baseline"])
      ).resolves.toMatchObject({
        ok: false,
        autonomyTier: "high",
        riskSummary: { toolRisk: "high", reversibilityRisk: "high" }
      });
      const piiReport = await governanceReport([piiSkillPath, "--policy", "sg-pdpa-strict"]);
      expect(piiReport).toMatchObject({
        ok: true,
        autonomyTier: "moderate",
        controls: { approvalRequired: true }
      });
      expect(piiReport.findings.some((finding) => finding.category === "data-risk")).toBe(true);
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

  it("normalizes Codex-style JSONL events in wrapper mode", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-wrapper-skill-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const fakeAgentPath = join(tempDir, "fake-agent.mjs");
    const runsDir = join(tempDir, "runs");
    await writeFile(
      skillPath,
      `---
name: wrapper-skill
tools: [Bash]
---

# Wrapper Skill

Run a safe shell-shaped operation.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(
      fakeAgentPath,
      `process.stdin.resume();
process.stdin.on("end", () => {
  console.log(JSON.stringify({ type: "local_shell_call", command: "printf wrapper", result: { stdout: "wrapper" } }));
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
        "--wrapper",
        "--enforce-policy",
        "--agent-command",
        process.execPath,
        "--agent-arg",
        fakeAgentPath,
        "--run-id",
        "skill-run.wrapper",
        "--runs-dir",
        runsDir
      ]);
      expect(run).toMatchObject({
        ok: true,
        status: "succeeded",
        wrapper: true
      });
      const agentRun = JSON.parse(
        await readFile(join(runsDir, "skill-run.wrapper", "agent-run.json"), "utf8")
      ) as {
        readonly hookEvents?: readonly {
          readonly hookEvent?: string;
          readonly toolName?: string;
        }[];
        readonly wrapperEvents?: readonly {
          readonly toolName?: string;
          readonly status?: string;
        }[];
        readonly enforcement?: { readonly source?: string; readonly wrapperBlocked?: boolean };
      };
      expect(agentRun.enforcement).toMatchObject({
        source: "none",
        wrapperBlocked: false
      });
      expect(agentRun.wrapperEvents).toEqual([
        expect.objectContaining({ toolName: "Bash", status: "allowed" })
      ]);
      expect(agentRun.hookEvents).toEqual([
        expect.objectContaining({ hookEvent: "ObservedToolUse", toolName: "Bash" })
      ]);
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("uses run-backed wrapper evidence in governance reports", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-governance-run-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const fakeAgentPath = join(tempDir, "fake-agent.mjs");
    const runsDir = join(tempDir, "runs");
    await writeFile(
      skillPath,
      `---
name: governance-run
tools: [Bash]
---

# Governance Run

Run a safe shell-shaped operation.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(
      fakeAgentPath,
      `process.stdin.resume();
process.stdin.on("end", () => {
  console.log(JSON.stringify({ type: "local_shell_call", command: "printf governed", result: { stdout: "governed" } }));
});
`,
      "utf8"
    );

    try {
      await runSkill([
        skillPath,
        "--input",
        inputPath,
        "--agent",
        "codex-cli",
        "--wrapper",
        "--enforce-policy",
        "--agent-command",
        process.execPath,
        "--agent-arg",
        fakeAgentPath,
        "--run-id",
        "skill-run.governance",
        "--runs-dir",
        runsDir
      ]);
      const report = await governanceReport([
        "skill-run.governance",
        "--runs-dir",
        runsDir,
        "--region",
        "sg",
        "--framework",
        "agentic-ai"
      ]);
      expect(report).toMatchObject({
        ok: true,
        subject: { kind: "run", runId: "skill-run.governance" },
        controls: {
          auditTrail: true,
          replayEvidence: true,
          hookEvents: true
        }
      });
      expect(report.frameworkMapping.map((mapping) => mapping.controlArea)).toContain(
        "Traceability and audit evidence"
      );
    } finally {
      await rm(tempDir, { recursive: true, force: true });
    }
  });

  it("fails closed on unclassified Codex JSONL events under enforced wrapper mode", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "kelpclaw-wrapper-block-"));
    const skillPath = join(tempDir, "SKILL.md");
    const inputPath = join(tempDir, "input.json");
    const fakeAgentPath = join(tempDir, "fake-agent.mjs");
    const runsDir = join(tempDir, "runs");
    await writeFile(
      skillPath,
      `---
name: wrapper-block
tools: [Bash]
---

# Wrapper Block

Run a runtime-selected operation.
`,
      "utf8"
    );
    await writeFile(inputPath, "{}\n", "utf8");
    await writeFile(
      fakeAgentPath,
      `process.stdin.resume();
process.stdin.on("end", () => {
  console.log(JSON.stringify({ type: "tool_call", payload: { opaque: true } }));
  setTimeout(() => console.log("should not print"), 5000);
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
        "--wrapper",
        "--enforce-policy",
        "--agent-command",
        process.execPath,
        "--agent-arg",
        fakeAgentPath,
        "--run-id",
        "skill-run.wrapper-block",
        "--runs-dir",
        runsDir
      ]);
      expect(run).toMatchObject({
        ok: false,
        status: "blocked",
        wrapper: true
      });
      const agentRun = JSON.parse(
        await readFile(join(runsDir, "skill-run.wrapper-block", "agent-run.json"), "utf8")
      ) as {
        readonly wrapperEvents?: readonly {
          readonly toolName?: string;
          readonly status?: string;
        }[];
        readonly enforcement?: {
          readonly source?: string;
          readonly unclassifiedBlocked?: boolean;
          readonly terminatedByPolicy?: boolean;
        };
      };
      expect(agentRun.enforcement).toMatchObject({
        source: "unclassified-event",
        unclassifiedBlocked: true,
        terminatedByPolicy: true
      });
      expect(agentRun.wrapperEvents).toEqual([
        expect.objectContaining({ toolName: "Unknown", status: "denied" })
      ]);
    } finally {
      process.exitCode = undefined;
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
        readonly hookEvents?: readonly {
          readonly status?: string;
          readonly hookEvent?: string;
          readonly contentHash?: string;
          readonly prevEventHash?: string;
          readonly chainIndex?: number;
        }[];
        readonly enforcement?: { readonly hookBlocked?: boolean; readonly source?: string };
        readonly generatedArtifacts?: readonly string[];
      };
      expect(agentRun.enforcement).toMatchObject({
        hookBlocked: true,
        source: "hook-pretool"
      });
      expect(agentRun.hookEvents).toEqual([
        expect.objectContaining({
          hookEvent: "PreToolUse",
          status: "denied",
          chainIndex: 0,
          contentHash: expect.stringMatching(/^sha256:/u),
          prevEventHash: expect.stringMatching(/^sha256:/u)
        })
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
        "--wrapper",
        "--enforce-policy",
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
      "codex-jsonl-shell",
      "destructive-shell",
      "github-pr-mutate",
      "github-pr-review",
      "local-file-audit",
      "network-health-check",
      "pii-file-write"
    ]);

    for (const skillDir of skillDirs) {
      const skillPath = join(corpusRoot, skillDir, "SKILL.md");
      const expectedFiles = (await readdir(join(corpusRoot, skillDir)))
        .filter((file) => /^expected\..+\.json$/u.test(file))
        .sort((left, right) => left.localeCompare(right));
      expect(expectedFiles.length).toBeGreaterThan(0);
      for (const expectedFile of expectedFiles) {
        const policy = expectedFile.replace(/^expected\./u, "").replace(/\.json$/u, "");
        const report = await compatibilityReport([skillPath, "--policy", policy]);
        const expected = JSON.parse(
          await readFile(join(corpusRoot, skillDir, expectedFile), "utf8")
        ) as unknown;
        expect(report).toEqual(expected);
      }
    }
  });
});

function cliSarifFixture() {
  return {
    version: "2.1.0",
    runs: [
      {
        tool: {
          driver: {
            name: "KelpClaw Evidence Fixture",
            rules: [
              {
                id: "KC001",
                name: "Evidence-backed governance finding",
                fullDescription: { text: "Finding imported from SARIF evidence." },
                help: { text: "Review the evidence workspace." },
                properties: { tags: ["CWE-693"] }
              }
            ]
          }
        },
        results: [
          {
            ruleId: "KC001",
            level: "warning",
            message: { text: "Evidence finding observed" },
            locations: [
              {
                physicalLocation: {
                  artifactLocation: { uri: "SKILL.md" },
                  region: { startLine: 6 }
                }
              }
            ]
          }
        ]
      }
    ]
  };
}

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
