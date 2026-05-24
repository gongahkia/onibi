#!/usr/bin/env node
import { spawn } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import {
  createPromotedSkillOtlpTracePayload,
  exportOtlpTraces,
  type OtlpTraceEvent
} from "@kelpclaw/adapters";
import { installClaudeCodeHooks, smokeClaudeCodeHookEvents } from "@kelpclaw/agent-hooks";
import {
  createCrossAgentReplayRuns,
  crossAgentReplaySkillMdFixture,
  synthesizeWorkflowFromTrajectory,
  trajectoryReplayShape
} from "@kelpclaw/codegen";
import {
  addEvidenceFile,
  compareEvidenceWorkspaces,
  createEvidenceWorkspace,
  importBurpEvidence,
  importNessusEvidence,
  importNmapEvidence,
  importNucleiEvidence,
  importSarifEvidence,
  importZapEvidence,
  loadEvidenceWorkspace,
  qaEvidenceWorkspace,
  renderEvidenceQaMarkdown,
  renderEvidenceRetestMarkdown,
  signEvidenceWorkspace,
  verifyEvidenceWorkspace,
  type EvidenceKind,
  type EvidenceSensitivity
} from "@kelpclaw/evidence";
import { evaluatePolicy, requirePolicyPack } from "@kelpclaw/policy";
import {
  createWebIntelClient,
  defaultProviderForOperation,
  policyArgsForWebRequest,
  toolNameForWebRequest,
  writeWebEvidenceFiles,
  type WebIntelProvider,
  type WebIntelRequest
} from "@kelpclaw/web-intel";
import {
  initAuditKey,
  compatibilityReport,
  exportSarif,
  exportAuditBundle,
  governanceControls,
  governanceReport,
  inventoryCoverage,
  inventoryGraph,
  inventoryScan,
  policyExplain,
  policyPackCliOutput,
  replayDiff,
  runSkill,
  verifyAuditBundle
} from "./skill-runner.js";

type JsonRecord = Record<string, unknown>;

const apiBaseUrl = process.env.KELPCLAW_API_URL ?? "http://127.0.0.1:8787";
const apiToken = process.env.KELPCLAW_API_TOKEN ?? process.env.KELPCLAW_ADMIN_TOKEN;

async function main(argv: readonly string[]): Promise<void> {
  const [command, ...args] = argv;
  switch (command) {
    case "start-recording":
      return printJson(
        await postJson("/api/agent-runs", {
          sourceAgent: requiredOption(args, "--agent"),
          sessionId: requiredOption(args, "--session-id"),
          ...(option(args, "--title") ? { title: option(args, "--title") } : {})
        })
      );
    case "record-step":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/events`,
          {
            hookEvent: requiredOption(args, "--hook-event"),
            toolName: requiredOption(args, "--tool-name"),
            args: jsonOption(args, "--args-json") ?? {},
            ...(jsonOption(args, "--result-json") !== undefined
              ? { result: jsonOption(args, "--result-json") }
              : {}),
            ...(option(args, "--status") ? { status: option(args, "--status") } : {})
          }
        )
      );
    case "stop-recording":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/stop`,
          {
            status: option(args, "--status") ?? "stopped"
          }
        )
      );
    case "approve-step":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/events/${encodeURIComponent(requiredOption(args, "--event-id"))}/approve`,
          {
            ...(option(args, "--reviewed-by") ? { reviewedBy: option(args, "--reviewed-by") } : {}),
            ...(option(args, "--reason") ? { reason: option(args, "--reason") } : {})
          }
        )
      );
    case "deny-step":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/events/${encodeURIComponent(requiredOption(args, "--event-id"))}/deny`,
          {
            ...(option(args, "--reviewed-by") ? { reviewedBy: option(args, "--reviewed-by") } : {}),
            ...(option(args, "--reason") ? { reason: option(args, "--reason") } : {})
          }
        )
      );
    case "promote":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredOption(args, "--run-id"))}/promote`,
          {
            skillName: requiredOption(args, "--skill-name"),
            ...(option(args, "--capability")
              ? { capabilities: [option(args, "--capability") as string] }
              : {})
          }
        )
      );
    case "policy":
      if (args[0] === "use") {
        return printJson(await usePolicyPack(args.slice(1)));
      }
      if (args[0] === "explain") {
        return printJson(await policyExplain(args.slice(1)));
      }
      return printJson(
        await putJson("/api/policies", {
          yaml: await readFile(requiredOption(args, "--file"), "utf8")
        })
      );
    case "compat":
    case "compat-report":
      return printJson(await compatibilityReport(args));
    case "run-skill":
      return printJson(await runSkill(args));
    case "export-audit-bundle":
      return printJson(await exportAuditBundle(args));
    case "export-sarif":
      return printJson(await exportSarif(args));
    case "governance":
      if (args[0] === "report") {
        return printJson(await governanceReport(args.slice(1)));
      }
      if (args[0] === "controls") {
        return printJson(await governanceControls(args.slice(1)));
      }
      throw new Error(
        "Usage: kelp-claw governance <report|controls> <SKILL.md|runId> [--region sg]"
      );
    case "web":
      return printJson(await runWebCommand(args));
    case "evidence":
      return printJson(await runEvidenceCommand(args));
    case "inventory":
      if (args[0] === "scan") {
        return printJson(await inventoryScan(args.slice(1)));
      }
      if (args[0] === "graph") {
        return printJson(await inventoryGraph(args.slice(1)));
      }
      if (args[0] === "coverage") {
        return printJson(await inventoryCoverage(args.slice(1)));
      }
      throw new Error("Usage: kelp-claw inventory <scan|graph|coverage> [--root DIR]");
    case "verify-audit-bundle":
      return printJson(await verifyAuditBundle(args));
    case "audit-key":
      if (args[0] === "init") {
        return printJson(await initAuditKey(args.slice(1)));
      }
      throw new Error("Usage: kelp-claw audit-key init [--key-dir .kelpclaw/keys]");
    case "replay-diff":
      return printJson(await replayDiff(args));
    case "audit-verify":
      return printJson(
        await getJson(
          `/api/agent-runs/${encodeURIComponent(requiredPositional(args, 0))}/audit/verify`
        )
      );
    case "audit-anchor":
      return printJson(
        await postJson(
          `/api/agent-runs/${encodeURIComponent(requiredPositional(args, 0))}/audit/anchor`,
          {}
        )
      );
    case "tbom-export":
      return printJson(
        await getJson(`/api/agent-runs/${encodeURIComponent(requiredPositional(args, 0))}/tbom`)
      );
    case "mint-role-token":
      return printJson(
        await postJson("/api/auth/role-tokens", {
          roles: requiredOption(args, "--roles")
            .split(",")
            .map((role) => role.trim())
            .filter((role) => role.length > 0),
          ...(option(args, "--subject") ? { subject: option(args, "--subject") } : {}),
          ...(option(args, "--expires-at") ? { expiresAt: option(args, "--expires-at") } : {}),
          ...(numberOption(args, "--ttl-seconds") !== undefined
            ? { ttlSeconds: numberOption(args, "--ttl-seconds") }
            : {})
        })
      );
    case "inspect-role-token":
      return printJson(
        await postJson("/api/auth/role-tokens/inspect", {
          token: requiredOption(args, "--token")
        })
      );
    case "verify-claude-code":
      return printJson(await verifyClaudeCode(args));
    case "otlp-smoke":
    case "datadog-otlp-smoke":
      return printJson(await runOtlpSmoke(args));
    case "cross-agent-replay-smoke":
      return printJson(runCrossAgentReplaySmoke());
    case "mcp":
      return runMcp(args);
    default:
      throw new Error(
        "Usage: kelp-claw <run-skill|compat|compat-report|policy|governance|web|evidence|inventory|audit-key|export-audit-bundle|export-sarif|verify-audit-bundle|replay-diff|start-recording|record-step|stop-recording|approve-step|deny-step|promote|mcp|audit-verify|audit-anchor|tbom-export|mint-role-token|inspect-role-token|verify-claude-code|otlp-smoke|cross-agent-replay-smoke>"
      );
  }
}

export {
  initAuditKey,
  compatibilityReport,
  exportSarif,
  exportAuditBundle,
  governanceControls,
  governanceReport,
  inventoryCoverage,
  inventoryGraph,
  inventoryScan,
  policyExplain,
  policyPackCliOutput,
  replayDiff,
  runSkill,
  verifyAuditBundle
} from "./skill-runner.js";

export async function runWebCommand(args: readonly string[]): Promise<JsonRecord> {
  const [command, ...commandArgs] = args;
  const request = webRequestFromArgs(command, commandArgs);
  const provider = request.provider ?? defaultProviderForOperation(request.operation);
  const policyName = option(commandArgs, "--policy") ?? "web-search-safe";
  const pack = requirePolicyPack(policyName);
  const toolName = toolNameForWebRequest(request, provider);
  const decision = evaluatePolicy(
    {
      tool: toolName,
      args: policyArgsForWebRequest(request, provider),
      skill: {
        id: "kelpclaw.web",
        tags: ["web", provider, request.operation]
      }
    },
    pack.ruleset
  );
  if (decision.action === "deny" || decision.action === "require-approval") {
    process.exitCode = 1;
    return {
      ok: false,
      status: "blocked",
      policyPack: pack.name,
      toolName,
      decision
    };
  }

  const client = createWebIntelClient();
  const evidence = await client.run({ ...request, provider });
  const outDir = option(commandArgs, "--out");
  const files = outDir ? await writeWebEvidenceFiles(outDir, evidence) : undefined;
  return {
    ok: true,
    status: "succeeded",
    policyPack: pack.name,
    toolName,
    decision,
    ...(outDir ? { outDir, files } : {}),
    evidence
  };
}

export async function runEvidenceCommand(args: readonly string[]): Promise<JsonRecord> {
  const [command, ...commandArgs] = args;
  const workspace = resolve(option(commandArgs, "--workspace") ?? ".kelpclaw/evidence");
  switch (command) {
    case "init": {
      const created = await createEvidenceWorkspace(workspace, {
        ...(option(commandArgs, "--client") ? { client: option(commandArgs, "--client") } : {}),
        ...(option(commandArgs, "--project") ? { project: option(commandArgs, "--project") } : {}),
        ...(options(commandArgs, "--scope").length
          ? { scope: options(commandArgs, "--scope") }
          : {})
      });
      return {
        ok: true,
        workspace: created.root,
        evidenceCount: created.index.evidence.length,
        findingCount: created.findings.findings.length
      };
    }
    case "add": {
      const result = await addEvidenceFile(workspace, {
        filePath: requiredOption(commandArgs, "--file"),
        kind: evidenceKind(option(commandArgs, "--kind") ?? "other"),
        ...(option(commandArgs, "--title") ? { title: option(commandArgs, "--title") } : {}),
        ...(option(commandArgs, "--observed-at")
          ? { observedAt: option(commandArgs, "--observed-at") }
          : {}),
        ...(option(commandArgs, "--source") ? { source: option(commandArgs, "--source") } : {}),
        ...(option(commandArgs, "--sensitivity")
          ? { sensitivity: evidenceSensitivity(option(commandArgs, "--sensitivity")) }
          : {}),
        ...(options(commandArgs, "--tag").length ? { tags: options(commandArgs, "--tag") } : {}),
        ...(option(commandArgs, "--notes") ? { notes: option(commandArgs, "--notes") } : {})
      });
      return { ok: true, ...result };
    }
    case "list": {
      const state = await loadEvidenceWorkspace(workspace);
      return {
        ok: true,
        workspace: state.root,
        evidence: state.index.evidence,
        findings: state.findings.findings
      };
    }
    case "import-sarif": {
      const input = option(commandArgs, "--input") ?? requiredPositional(commandArgs, 0);
      const result = await importSarifEvidence(workspace, input);
      return { ok: true, ...result };
    }
    case "import-nmap": {
      const input = option(commandArgs, "--input") ?? requiredPositional(commandArgs, 0);
      const result = await importNmapEvidence(workspace, input);
      return { ok: true, ...result };
    }
    case "import-nuclei": {
      const input = option(commandArgs, "--input") ?? requiredPositional(commandArgs, 0);
      const result = await importNucleiEvidence(workspace, input);
      return { ok: true, ...result };
    }
    case "import-burp": {
      const input = option(commandArgs, "--input") ?? requiredPositional(commandArgs, 0);
      const result = await importBurpEvidence(workspace, input);
      return { ok: true, ...result };
    }
    case "import-zap": {
      const input = option(commandArgs, "--input") ?? requiredPositional(commandArgs, 0);
      const result = await importZapEvidence(workspace, input);
      return { ok: true, ...result };
    }
    case "import-nessus": {
      const input = option(commandArgs, "--input") ?? requiredPositional(commandArgs, 0);
      const result = await importNessusEvidence(workspace, input);
      return { ok: true, ...result };
    }
    case "sign":
      return signEvidenceWorkspace(workspace, {
        ...(option(commandArgs, "--key-dir") ? { keyDir: option(commandArgs, "--key-dir") } : {})
      });
    case "verify": {
      const result = await verifyEvidenceWorkspace(workspace, option(commandArgs, "--manifest"));
      if (!result.ok) {
        process.exitCode = 1;
      }
      return result as unknown as JsonRecord;
    }
    case "qa": {
      const result = await qaEvidenceWorkspace(workspace);
      if (!result.valid) {
        process.exitCode = 1;
      }
      const format = option(commandArgs, "--format") ?? "json";
      const markdown = format === "markdown" ? renderEvidenceQaMarkdown(result) : undefined;
      const out = option(commandArgs, "--out");
      if (out) {
        await writeTextWithParents(resolve(out), markdown ?? JSON.stringify(result, null, 2));
      }
      return {
        ok: result.valid,
        ...(out ? { out: resolve(out) } : {}),
        ...(markdown ? { markdown } : {}),
        ...result
      };
    }
    case "retest": {
      const result = await compareEvidenceWorkspaces(
        requiredOption(commandArgs, "--baseline"),
        requiredOption(commandArgs, "--current")
      );
      const format = option(commandArgs, "--format") ?? "json";
      const markdown = format === "markdown" ? renderEvidenceRetestMarkdown(result) : undefined;
      const out = option(commandArgs, "--out");
      if (out) {
        await writeTextWithParents(resolve(out), markdown ?? JSON.stringify(result, null, 2));
      }
      return {
        ok: true,
        ...(out ? { out: resolve(out) } : {}),
        ...(markdown ? { markdown } : {}),
        ...result
      };
    }
    default:
      throw new Error(
        "Usage: kelp-claw evidence <init|add|list|import-sarif|import-nmap|import-nuclei|import-burp|import-zap|import-nessus|sign|verify|qa|retest> [--workspace .kelpclaw/evidence]"
      );
  }
}

export async function verifyClaudeCode(args: readonly string[]): Promise<JsonRecord> {
  const command =
    option(args, "--command") ??
    'node "$CLAUDE_PROJECT_DIR/packages/agent-hooks/dist/index.js" send-event';
  const install = await installClaudeCodeHooks({
    settingsPath: option(args, "--settings"),
    command
  });
  const coverage = await readClaudeSettingsCoverage(install.settingsPath, install.events, command);
  const existingRunId = option(args, "--run-id");
  const run =
    existingRunId === undefined
      ? await createClaudeVerificationRun(args)
      : { id: existingRunId, created: false };
  const smoke = await smokeClaudeCodeHookEvents({
    runId: run.id,
    apiBaseUrl,
    apiToken,
    sourceAgent: "claude-code"
  });
  const audit = await getJson(`/api/agent-runs/${encodeURIComponent(run.id)}/audit/verify`);
  const auditVerification = jsonRecordField(jsonRecord(audit), "verification");
  const ok =
    coverage.every((event) => event.installed) &&
    smoke.events.every((event) => event.ok) &&
    auditVerification?.valid === true;
  if (!ok) {
    process.exitCode = 1;
  }
  return {
    ok,
    runId: run.id,
    createdRun: run.created,
    apiBaseUrl,
    settingsPath: install.settingsPath,
    command,
    env: {
      KELPCLAW_AGENT_RUN_ID: Boolean(process.env.KELPCLAW_AGENT_RUN_ID),
      KELPCLAW_API_URL: Boolean(process.env.KELPCLAW_API_URL),
      KELPCLAW_API_TOKEN: Boolean(process.env.KELPCLAW_API_TOKEN),
      KELPCLAW_ADMIN_TOKEN: Boolean(process.env.KELPCLAW_ADMIN_TOKEN)
    },
    settings: {
      eventCount: coverage.length,
      installed: coverage.every((event) => event.installed),
      events: coverage
    },
    smoke,
    audit
  };
}

export async function runOtlpSmoke(args: readonly string[]): Promise<JsonRecord> {
  const endpoint = requiredOption(args, "--endpoint", otlpEndpointFromEnv());
  const runId = option(args, "--run-id") ?? `agent-run.otlp-smoke.${Date.now()}`;
  const skillId = option(args, "--skill-id") ?? "skill.promoted.otlp-smoke";
  const promotedAt = option(args, "--promoted-at") ?? new Date().toISOString();
  const headers = {
    ...parseHeaderEnv(process.env.OTEL_EXPORTER_OTLP_HEADERS),
    ...parseHeaderEnv(process.env.KELPCLAW_OTLP_HEADERS),
    ...headersFromArgs(args),
    ...(process.env.DD_API_KEY ? { "DD-API-KEY": process.env.DD_API_KEY } : {})
  };
  const payload = createPromotedSkillOtlpTracePayload({
    endpoint,
    headers,
    serviceName: option(args, "--service-name") ?? "kelpclaw-cli",
    serviceVersion: option(args, "--service-version") ?? "0.1.0",
    runId,
    skillId,
    sourceAgent: "claude-code",
    promotedAt,
    events: smokeOtlpEvents(promotedAt)
  });
  const result = await exportOtlpTraces({ endpoint, headers, payload });
  const output = {
    ok: result.accepted,
    endpoint: result.endpoint,
    statusCode: result.statusCode,
    traceCount: 1,
    spanCount: result.spanCount,
    runId,
    skillId,
    headerNames: Object.keys(headers).sort(),
    ...(result.responseText ? { responseText: result.responseText.slice(0, 500) } : {})
  };
  if (!result.accepted) {
    process.exitCode = 1;
  }
  return output;
}

export function runCrossAgentReplaySmoke(): JsonRecord {
  const runs = createCrossAgentReplayRuns();
  const shapes = runs.map(trajectoryReplayShape);
  const workflows = runs.map((run) =>
    synthesizeWorkflowFromTrajectory(run, { createdAt: "2026-05-23T00:00:00.000Z" })
  );
  const expectedShape = JSON.stringify(shapes[0]);
  const shapeMatches = shapes.every((shape) => JSON.stringify(shape) === expectedShape);
  const workflowKinds = workflows.map((workflow) => workflow.nodes.map((node) => node.kind));
  const workflowKindsMatch = workflowKinds.every(
    (kinds) => JSON.stringify(kinds) === JSON.stringify(workflowKinds[0])
  );
  const agentTags = workflows.map((workflow) =>
    workflow.nodes
      .filter((node) => node.kind === "agent-step")
      .map((node) => node.agentStep?.sourceAgent)
  );
  const ok = shapeMatches && workflowKindsMatch;
  if (!ok) {
    process.exitCode = 1;
  }
  return {
    ok,
    skillName: "kelpclaw-replay-smoke",
    skillMdBytes: crossAgentReplaySkillMdFixture.length,
    agents: runs.map((run) => run.sourceAgent),
    eventCount: shapes[0]?.eventCount ?? 0,
    tools: shapes[0]?.tools ?? [],
    statuses: shapes[0]?.statuses ?? [],
    outputs: shapes[0]?.outputs ?? [],
    workflowKinds,
    agentTags
  };
}

function webRequestFromArgs(command: string | undefined, args: readonly string[]): WebIntelRequest {
  const provider = providerOption(args);
  const domains = options(args, "--domain");
  const numResults = numberOption(args, "--num-results");
  const common: {
    provider?: WebIntelProvider;
    domains?: readonly string[];
    numResults?: number;
    storeFullContent?: boolean;
  } = {};
  if (provider) {
    common.provider = provider;
  }
  if (domains.length) {
    common.domains = domains;
  }
  if (numResults !== undefined) {
    common.numResults = numResults;
  }
  if (hasFlag(args, "--store-full-content")) {
    common.storeFullContent = true;
  }
  switch (command) {
    case "search":
      return {
        operation: "web.search",
        query: requiredPositional(args, 0),
        ...common
      };
    case "fetch":
      return {
        operation: "web.fetch",
        url: requiredPositional(args, 0),
        ...common
      };
    case "answer":
      return {
        operation: "web.answer",
        question: requiredPositional(args, 0),
        ...common
      };
    case "research": {
      const providers = option(args, "--providers")
        ?.split(",")
        .map((entry) => entry.trim())
        .filter(Boolean);
      const selectedProvider = provider ?? providerFromString(providers?.[0]);
      return {
        operation: "web.search",
        query: requiredPositional(args, 0),
        goal: requiredPositional(args, 0),
        ...(selectedProvider ? { provider: selectedProvider } : {}),
        ...common,
        metadata: {
          command: "research",
          providers: providers ?? []
        }
      };
    }
    case "browser-session":
      return {
        operation: "web.browser.session",
        goal: requiredPositional(args, 0),
        ...common
      };
    case "browser-action":
      return {
        operation: "web.browser.action",
        browserSessionId: requiredOption(args, "--session-id"),
        action: requiredPositional(args, 0),
        ...common
      };
    case "agent-task":
      return {
        operation: "web.agent.task",
        goal: requiredPositional(args, 0),
        ...common
      };
    default:
      throw new Error(
        "Usage: kelp-claw web <search|fetch|answer|research> <query|url|question> [--provider exa|tinyfish] [--policy web-search-safe] [--out DIR]"
      );
  }
}

function providerOption(args: readonly string[]): WebIntelProvider | undefined {
  return providerFromString(option(args, "--provider"));
}

function providerFromString(value: string | undefined): WebIntelProvider | undefined {
  if (value === undefined) {
    return undefined;
  }
  if (value === "exa" || value === "tinyfish") {
    return value;
  }
  throw new Error(`Unsupported web provider '${value}'. Expected exa or tinyfish.`);
}

function evidenceKind(value: string): EvidenceKind {
  const kinds = new Set([
    "screenshot",
    "agent-run",
    "web-evidence",
    "scanner",
    "sarif",
    "transcript",
    "note",
    "other"
  ]);
  if (kinds.has(value)) {
    return value as EvidenceKind;
  }
  throw new Error(`Unsupported evidence kind '${value}'.`);
}

function evidenceSensitivity(value: string | undefined): EvidenceSensitivity | undefined {
  if (value === undefined) {
    return undefined;
  }
  const sensitivities = new Set(["public", "internal", "sensitive", "secret"]);
  if (sensitivities.has(value)) {
    return value as EvidenceSensitivity;
  }
  throw new Error(`Unsupported evidence sensitivity '${value}'.`);
}

async function usePolicyPack(args: readonly string[]): Promise<JsonRecord> {
  const name = requiredPositional(args, 0);
  const output = policyPackCliOutput(name);
  if (process.env.KELPCLAW_API_URL) {
    await putJson("/api/policies", { rules: output.ruleset.rules });
    return {
      ...output,
      installedToApi: true,
      apiBaseUrl
    };
  }
  return {
    ...output,
    installedToApi: false
  };
}

async function createClaudeVerificationRun(
  args: readonly string[]
): Promise<{ readonly id: string; readonly created: boolean }> {
  const sessionId = option(args, "--session-id") ?? `claude-code.verify.${Date.now()}`;
  const response = jsonRecord(
    await postJson("/api/agent-runs", {
      sourceAgent: "claude-code",
      sessionId,
      title: option(args, "--title") ?? "Claude Code Hook Verification"
    })
  );
  const run = jsonRecordField(response, "run");
  const id = stringField(run, "id");
  if (!id) {
    throw new Error("Claude Code verification could not read created run id.");
  }
  return { id, created: true };
}

async function readClaudeSettingsCoverage(
  settingsPath: string,
  events: readonly string[],
  command: string
): Promise<readonly JsonRecord[]> {
  const settings = jsonRecord(JSON.parse(await readFile(settingsPath, "utf8")) as unknown);
  const hooks = jsonRecordField(settings, "hooks");
  return events.map((event) => ({
    event,
    installed: claudeEventHasCommand(hooks?.[event], command)
  }));
}

function claudeEventHasCommand(value: unknown, command: string): boolean {
  return (
    Array.isArray(value) &&
    value.some((entry) => {
      const hooks = jsonArrayField(jsonRecord(entry), "hooks");
      return hooks.some((hook) => stringField(jsonRecord(hook), "command") === command);
    })
  );
}

function smokeOtlpEvents(timestamp: string): readonly OtlpTraceEvent[] {
  return [
    {
      sourceAgent: "claude-code",
      hookEvent: "PostToolUse",
      toolName: "Bash",
      toolUseId: "toolu.otlp-smoke.write",
      args: { command: "printf kelpclaw-otlp-smoke" },
      result: { stdout: "kelpclaw-otlp-smoke", exitCode: 0 },
      status: "succeeded",
      contentHash: `sha256:${"1".repeat(64)}`,
      prevEventHash: `sha256:${"0".repeat(64)}`,
      chainIndex: 0,
      startedAt: timestamp,
      finishedAt: timestamp
    },
    {
      sourceAgent: "claude-code",
      hookEvent: "PostToolUse",
      toolName: "Read",
      toolUseId: "toolu.otlp-smoke.read",
      args: { filePath: ".kelpclaw-otlp-smoke.txt" },
      result: { content: "kelpclaw-otlp-smoke\n" },
      status: "succeeded",
      contentHash: `sha256:${"2".repeat(64)}`,
      prevEventHash: `sha256:${"1".repeat(64)}`,
      chainIndex: 1,
      startedAt: timestamp,
      finishedAt: timestamp
    }
  ];
}

async function runMcp(args: readonly string[]): Promise<void> {
  const command = process.env.KELPCLAW_MCP_COMMAND ?? "kelp-mcp";
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, [...args], {
      stdio: "inherit",
      env: process.env
    });
    child.on("error", reject);
    child.on("exit", (code, signal) => {
      if (code === 0) {
        resolve();
      } else {
        reject(new Error(`MCP sidecar exited with ${signal ?? code ?? "unknown status"}.`));
      }
    });
  });
}

async function getJson(path: string): Promise<unknown> {
  return requestJson("GET", path);
}

async function postJson(path: string, body: JsonRecord): Promise<unknown> {
  return requestJson("POST", path, body);
}

async function putJson(path: string, body: JsonRecord): Promise<unknown> {
  return requestJson("PUT", path, body);
}

async function requestJson(method: string, path: string, body?: JsonRecord): Promise<unknown> {
  const response = await fetch(new URL(path, apiBaseUrl), {
    method,
    headers: {
      "content-type": "application/json",
      ...(apiToken ? { authorization: `Bearer ${apiToken}` } : {})
    },
    ...(body ? { body: JSON.stringify(body) } : {})
  });
  const payload = (await response.json()) as unknown;
  if (!response.ok) {
    throw new Error(JSON.stringify(payload));
  }
  return payload;
}

function option(args: readonly string[], name: string): string | undefined {
  const index = args.indexOf(name);
  const value = index >= 0 ? args[index + 1] : undefined;
  return value && !value.startsWith("--") ? value : undefined;
}

function options(args: readonly string[], name: string): readonly string[] {
  const values: string[] = [];
  for (let index = 0; index < args.length; index += 1) {
    if (args[index] === name) {
      const value = args[index + 1];
      if (value && !value.startsWith("--")) {
        values.push(value);
      }
    }
  }
  return values;
}

function requiredOption(args: readonly string[], name: string, fallback?: string): string {
  const value = option(args, name) ?? fallback;
  if (!value) {
    throw new Error(`Missing required option ${name}.`);
  }
  return value;
}

function requiredPositional(args: readonly string[], index: number): string {
  const value = args.filter((arg) => !arg.startsWith("--"))[index];
  if (!value) {
    throw new Error(`Missing positional argument ${index + 1}.`);
  }
  return value;
}

function jsonOption(args: readonly string[], name: string): unknown {
  const value = option(args, name);
  return value ? JSON.parse(value) : undefined;
}

function numberOption(args: readonly string[], name: string): number | undefined {
  const value = option(args, name);
  if (!value) {
    return undefined;
  }
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    throw new Error(`Option ${name} must be a number.`);
  }
  return parsed;
}

function hasFlag(args: readonly string[], name: string): boolean {
  return args.includes(name);
}

async function writeTextWithParents(path: string, content: string): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${content}${content.endsWith("\n") ? "" : "\n"}`, "utf8");
}

function otlpEndpointFromEnv(): string | undefined {
  return (
    process.env.KELPCLAW_OTLP_TRACES_ENDPOINT ??
    process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT ??
    tracesEndpointFromBase(
      process.env.KELPCLAW_OTLP_ENDPOINT ?? process.env.OTEL_EXPORTER_OTLP_ENDPOINT
    )
  );
}

function tracesEndpointFromBase(value: string | undefined): string | undefined {
  if (!value) {
    return undefined;
  }
  return `${value.replace(/\/+$/u, "")}/v1/traces`;
}

function headersFromArgs(args: readonly string[]): Readonly<Record<string, string>> {
  return Object.fromEntries(
    options(args, "--header")
      .map((entry) => {
        const [name, ...rest] = entry.split("=");
        return [name ?? "", rest.join("=")] as const;
      })
      .filter(([name, value]) => name.length > 0 && value.length > 0)
  );
}

function parseHeaderEnv(value: string | undefined): Readonly<Record<string, string>> {
  if (!value) {
    return {};
  }
  if (value.trim().startsWith("{")) {
    const parsed = JSON.parse(value) as unknown;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      throw new Error("OTLP headers JSON must be an object.");
    }
    return Object.fromEntries(
      Object.entries(parsed).filter(
        (entry): entry is [string, string] => typeof entry[1] === "string"
      )
    );
  }
  return Object.fromEntries(
    value
      .split(",")
      .map((part) => part.trim())
      .filter(Boolean)
      .map((part) => {
        const [name, ...rest] = part.split("=");
        return [decodeURIComponent(name ?? ""), decodeURIComponent(rest.join("="))] as const;
      })
      .filter(([name]) => name.length > 0)
  );
}

function jsonRecord(value: unknown): JsonRecord {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as JsonRecord) : {};
}

function jsonRecordField(value: JsonRecord | undefined, field: string): JsonRecord | undefined {
  const candidate = value?.[field];
  return candidate && typeof candidate === "object" && !Array.isArray(candidate)
    ? (candidate as JsonRecord)
    : undefined;
}

function jsonArrayField(value: JsonRecord | undefined, field: string): readonly unknown[] {
  const candidate = value?.[field];
  return Array.isArray(candidate) ? candidate : [];
}

function stringField(value: JsonRecord | undefined, field: string): string | undefined {
  const candidate = value?.[field];
  return typeof candidate === "string" ? candidate : undefined;
}

function printJson(value: unknown): void {
  process.stdout.write(`${JSON.stringify(value, null, 2)}\n`);
}

if (import.meta.url === `file://${process.argv[1]}`) {
  main(process.argv.slice(2)).catch((error: unknown) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
