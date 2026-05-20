import type { Options, SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import type { JsonRecord } from "@kelpclaw/workflow-spec";
import type {
  GeneratedNodeBuildRole,
  GeneratedNodeFixTriageDecision,
  GeneratedNodeRoleRunInput,
  GeneratedNodeRoleRunResult,
  GeneratedNodeRoleRunner,
  WorkflowCodegenArtifactRef
} from "./types.js";

export interface AgentSdkGeneratedNodeRoleRunnerOptions {
  readonly role: GeneratedNodeBuildRole;
  readonly apiKey?: string | undefined;
  readonly model?: string | undefined;
  readonly queryRunner?: AgentRoleQueryRunner | undefined;
  readonly maxTurns?: number | undefined;
}

export type AgentRoleQueryRunner = (
  prompt: string,
  options: Options
) => AsyncIterable<Pick<SDKMessage, "type"> & Record<string, unknown>>;

interface RoleQueryResult {
  readonly summary: string;
  readonly status: "succeeded" | "failed";
  readonly totalCostUsd: number;
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly fixTriage?: GeneratedNodeFixTriageDecision | undefined;
  readonly usage: ModelUsageSnapshot;
}

interface ModelUsageSnapshot {
  readonly durationMs?: number | undefined;
  readonly durationApiMs?: number | undefined;
  readonly inputTokens?: number | undefined;
  readonly outputTokens?: number | undefined;
  readonly cacheReadInputTokens?: number | undefined;
  readonly cacheCreationInputTokens?: number | undefined;
  readonly totalTokens?: number | undefined;
  readonly costUsd?: number | undefined;
  readonly modelUsage?: JsonRecord | undefined;
}

export class AgentSdkGeneratedNodeRoleRunner implements GeneratedNodeRoleRunner {
  public readonly role: GeneratedNodeBuildRole;
  private readonly apiKey: string | undefined;
  private readonly model: string | undefined;
  private readonly queryRunner: AgentRoleQueryRunner | undefined;
  private readonly maxTurns: number;

  public constructor(options: AgentSdkGeneratedNodeRoleRunnerOptions) {
    this.role = options.role;
    this.apiKey = options.apiKey ?? process.env.ANTHROPIC_API_KEY;
    this.model = options.model ?? modelForRole(options.role);
    this.queryRunner = options.queryRunner;
    this.maxTurns = options.maxTurns ?? 1;
  }

  public async run(input: GeneratedNodeRoleRunInput): Promise<GeneratedNodeRoleRunResult> {
    try {
      const runner = await this.getQueryRunner();
      const roleResult = await runRoleQuery(
        runner,
        createRolePrompt(input),
        this.createQueryOptions(),
        input.outputArtifactRefs
      );
      const modelInvocation = {
        id: `model.${input.request.job.id}.${input.role}.${input.iteration}.agent-sdk`,
        role: input.role,
        inputSummary: input.inputSummary.slice(0, 240),
        outputArtifact:
          roleResult.outputArtifactRefs[0]?.path ?? `agent-run:${input.role}:${input.iteration}`,
        provider: "anthropic",
        model: this.model ?? "default",
        determinismExpectation: "bounded" as const,
        retryBudget: {
          maxAttempts: input.request.job.retry.maxAttempts,
          maxCostUsd: input.request.maxModelCostUsd
        },
        correlationId: input.request.job.correlationId,
        createdAt: input.request.generatedAt ?? new Date().toISOString(),
        ...roleResult.usage
      };

      if (roleResult.status === "failed") {
        return {
          status: "failed",
          inputSummary: roleResult.summary,
          outputArtifactRefs: roleResult.outputArtifactRefs,
          modelProvider: "anthropic",
          model: this.model ?? "default",
          modelCostUsd: roleResult.totalCostUsd,
          modelInvocations: [modelInvocation],
          fixTriage: roleResult.fixTriage,
          error: roleResult.summary
        };
      }

      if (input.role === "coder") {
        const generation = await input.generateCode(input.request);
        return {
          status: "succeeded",
          inputSummary: roleResult.summary,
          outputArtifactRefs: [
            {
              path: generation.sourceArtifact.path,
              checksum: generation.sourceArtifact.checksum,
              contentType: generation.sourceArtifact.contentType
            },
            {
              path: generation.dependencyManifestArtifact.path,
              checksum: generation.dependencyManifestArtifact.checksum,
              contentType: generation.dependencyManifestArtifact.contentType
            }
          ],
          generation,
          modelProvider: "anthropic",
          model: this.model ?? "default",
          modelCostUsd: roleResult.totalCostUsd,
          modelInvocations: [modelInvocation]
        };
      }

      return {
        status: roleResult.status,
        inputSummary: roleResult.summary,
        outputArtifactRefs: roleResult.outputArtifactRefs,
        modelProvider: "anthropic",
        model: this.model ?? "default",
        modelCostUsd: roleResult.totalCostUsd,
        modelInvocations: [modelInvocation],
        fixTriage: roleResult.fixTriage
      };
    } catch (error) {
      return {
        status: "failed",
        inputSummary: input.inputSummary,
        outputArtifactRefs: [],
        modelProvider: "anthropic",
        model: this.model ?? "default",
        error: error instanceof Error ? error.message : `${input.role} role failed.`
      };
    }
  }

  private async getQueryRunner(): Promise<AgentRoleQueryRunner> {
    if (this.queryRunner) {
      return this.queryRunner;
    }
    if (!this.apiKey) {
      throw new Error("ANTHROPIC_API_KEY is required for live generated-node role runners.");
    }

    const sdk = await import("@anthropic-ai/claude-agent-sdk");
    return (prompt, options) => sdk.query({ prompt, options });
  }

  private createQueryOptions(): Options {
    const options: Options = {
      maxTurns: this.maxTurns,
      tools: [],
      env: {
        ...process.env,
        ANTHROPIC_API_KEY: this.apiKey,
        CLAUDE_AGENT_SDK_CLIENT_APP: "kelpclaw-codegen-role/0.1.0"
      },
      outputFormat: {
        type: "json_schema",
        schema: roleOutputSchema
      }
    };
    if (this.model) {
      options.model = this.model;
    }

    return options;
  }
}

export function createAgentSdkGeneratedNodeRoleRunners(
  options: {
    readonly apiKey?: string | undefined;
    readonly queryRunner?: AgentRoleQueryRunner | undefined;
  } = {}
): Partial<Record<GeneratedNodeBuildRole, GeneratedNodeRoleRunner>> {
  const roles: readonly GeneratedNodeBuildRole[] = [
    "workflow-architect",
    "coder",
    "tester",
    "runner",
    "fixer",
    "evaluator"
  ];

  return Object.fromEntries(
    roles.map((role) => [
      role,
      new AgentSdkGeneratedNodeRoleRunner({
        role,
        apiKey: options.apiKey,
        model: modelForRole(role),
        queryRunner: options.queryRunner
      })
    ])
  ) as Partial<Record<GeneratedNodeBuildRole, GeneratedNodeRoleRunner>>;
}

function modelForRole(role: GeneratedNodeBuildRole): string | undefined {
  switch (role) {
    case "workflow-architect":
      return (
        process.env.KELPCLAW_WORKFLOW_ARCHITECT_MODEL ??
        process.env.KELPCLAW_ARCHITECT_MODEL ??
        process.env.KELPCLAW_CODEGEN_MODEL ??
        process.env.KELPCLAW_PLANNER_MODEL
      );
    case "coder":
      return (
        process.env.KELPCLAW_CODER_MODEL ??
        process.env.KELPCLAW_CODEGEN_MODEL ??
        process.env.KELPCLAW_PLANNER_MODEL
      );
    case "tester":
      return (
        process.env.KELPCLAW_TESTER_MODEL ??
        process.env.KELPCLAW_CODEGEN_MODEL ??
        process.env.KELPCLAW_PLANNER_MODEL
      );
    case "runner":
      return (
        process.env.KELPCLAW_RUNNER_MODEL ??
        process.env.KELPCLAW_CODEGEN_MODEL ??
        process.env.KELPCLAW_PLANNER_MODEL
      );
    case "fixer":
      return (
        process.env.KELPCLAW_FIXER_MODEL ??
        process.env.KELPCLAW_CODEGEN_MODEL ??
        process.env.KELPCLAW_PLANNER_MODEL
      );
    case "evaluator":
      return (
        process.env.KELPCLAW_EVALUATOR_MODEL ??
        process.env.KELPCLAW_CODEGEN_MODEL ??
        process.env.KELPCLAW_PLANNER_MODEL
      );
  }
}

async function runRoleQuery(
  runner: AgentRoleQueryRunner,
  prompt: string,
  options: Options,
  fallbackArtifacts: readonly WorkflowCodegenArtifactRef[]
): Promise<RoleQueryResult> {
  let summary = "";
  let status: RoleQueryResult["status"] = "succeeded";
  let totalCostUsd = 0;
  let outputArtifactRefs: readonly WorkflowCodegenArtifactRef[] = fallbackArtifacts;
  let usage: ModelUsageSnapshot = {};
  let fixTriage: GeneratedNodeFixTriageDecision | undefined;

  for await (const message of runner(prompt, options)) {
    if (message.type !== "result") {
      continue;
    }
    const record = message as Record<string, unknown>;
    totalCostUsd += numberValue(record.total_cost_usd);
    usage = mergeUsage(usage, usageFromResult(record));
    const structured = parseRoleStructuredOutput(record.structured_output ?? record.result);
    summary = structured.summary;
    status = structured.status;
    fixTriage = structured.fixTriage;
    outputArtifactRefs =
      structured.outputArtifactRefs.length > 0 ? structured.outputArtifactRefs : fallbackArtifacts;
  }

  if (summary.length === 0) {
    throw new Error("Anthropic Agent SDK did not return a role summary.");
  }

  return {
    summary,
    status,
    totalCostUsd,
    outputArtifactRefs,
    fixTriage,
    usage
  };
}

function usageFromResult(record: Record<string, unknown>): ModelUsageSnapshot {
  const usage = recordValue(record.usage);
  const inputTokens =
    numberValue(usage.input_tokens) ||
    numberValue(usage.inputTokens) ||
    numberValue(usage.cache_creation_input_tokens) + numberValue(usage.cache_read_input_tokens);
  const outputTokens = numberValue(usage.output_tokens) || numberValue(usage.outputTokens);
  const cacheReadInputTokens =
    numberValue(usage.cache_read_input_tokens) || numberValue(usage.cacheReadInputTokens);
  const cacheCreationInputTokens =
    numberValue(usage.cache_creation_input_tokens) || numberValue(usage.cacheCreationInputTokens);
  const costUsd =
    numberValue(record.total_cost_usd) ||
    numberValue(record.totalCostUsd) ||
    numberValue(record.costUSD);
  const durationMs = numberValue(record.duration_ms) || numberValue(record.durationMs);
  const durationApiMs = numberValue(record.duration_api_ms) || numberValue(record.durationApiMs);
  const modelUsage = jsonRecordValue(record.modelUsage);
  const totalTokens = inputTokens + outputTokens + cacheReadInputTokens + cacheCreationInputTokens;

  return {
    ...(durationMs > 0 ? { durationMs } : {}),
    ...(durationApiMs > 0 ? { durationApiMs } : {}),
    ...(inputTokens > 0 ? { inputTokens } : {}),
    ...(outputTokens > 0 ? { outputTokens } : {}),
    ...(cacheReadInputTokens > 0 ? { cacheReadInputTokens } : {}),
    ...(cacheCreationInputTokens > 0 ? { cacheCreationInputTokens } : {}),
    ...(totalTokens > 0 ? { totalTokens } : {}),
    ...(costUsd > 0 ? { costUsd } : {}),
    ...(Object.keys(modelUsage).length > 0 ? { modelUsage } : {})
  };
}

function mergeUsage(left: ModelUsageSnapshot, right: ModelUsageSnapshot): ModelUsageSnapshot {
  return {
    durationMs: (left.durationMs ?? 0) + (right.durationMs ?? 0) || undefined,
    durationApiMs: (left.durationApiMs ?? 0) + (right.durationApiMs ?? 0) || undefined,
    inputTokens: (left.inputTokens ?? 0) + (right.inputTokens ?? 0) || undefined,
    outputTokens: (left.outputTokens ?? 0) + (right.outputTokens ?? 0) || undefined,
    cacheReadInputTokens:
      (left.cacheReadInputTokens ?? 0) + (right.cacheReadInputTokens ?? 0) || undefined,
    cacheCreationInputTokens:
      (left.cacheCreationInputTokens ?? 0) + (right.cacheCreationInputTokens ?? 0) || undefined,
    totalTokens: (left.totalTokens ?? 0) + (right.totalTokens ?? 0) || undefined,
    costUsd: (left.costUsd ?? 0) + (right.costUsd ?? 0) || undefined,
    modelUsage:
      left.modelUsage || right.modelUsage
        ? {
            ...(left.modelUsage ?? {}),
            ...(right.modelUsage ?? {})
          }
        : undefined
  };
}

function parseRoleStructuredOutput(output: unknown): {
  readonly summary: string;
  readonly status: RoleQueryResult["status"];
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly fixTriage?: GeneratedNodeFixTriageDecision | undefined;
} {
  const parsed = typeof output === "string" ? safeParseJson(output) : output;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("Generated-node role output must be a JSON object.");
  }
  const record = parsed as {
    readonly summary?: unknown;
    readonly status?: unknown;
    readonly outputArtifactRefs?: unknown;
    readonly fixTriage?: unknown;
  };
  if (typeof record.summary !== "string" || record.summary.length === 0) {
    throw new Error("Generated-node role output requires a summary.");
  }
  const status = record.status === "failed" ? "failed" : "succeeded";
  const outputArtifactRefs = Array.isArray(record.outputArtifactRefs)
    ? record.outputArtifactRefs.filter(isArtifactRef)
    : [];
  const fixTriage = parseFixTriageDecision(record.fixTriage);

  return {
    summary: record.summary,
    status,
    outputArtifactRefs,
    ...(fixTriage ? { fixTriage } : {})
  };
}

function parseFixTriageDecision(value: unknown): GeneratedNodeFixTriageDecision | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return undefined;
  }
  const record = value as Partial<GeneratedNodeFixTriageDecision>;
  const actions = ["targeted-patch", "retry-codegen", "rearchitect", "give-up"] as const;
  const scopes = ["local-code", "node-contract", "workflow-design", "external-blocker"] as const;
  if (
    !actions.includes(record.action as (typeof actions)[number]) ||
    !scopes.includes(record.scope as (typeof scopes)[number]) ||
    typeof record.rationale !== "string"
  ) {
    return undefined;
  }

  const confidence =
    typeof record.confidence === "number" && Number.isFinite(record.confidence)
      ? Math.min(1, Math.max(0, record.confidence))
      : 0.5;

  return {
    action: record.action as GeneratedNodeFixTriageDecision["action"],
    scope: record.scope as GeneratedNodeFixTriageDecision["scope"],
    rationale: record.rationale,
    confidence
  };
}

function isArtifactRef(value: unknown): value is WorkflowCodegenArtifactRef {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return false;
  }
  const record = value as Partial<WorkflowCodegenArtifactRef>;
  return (
    typeof record.path === "string" &&
    typeof record.checksum === "string" &&
    ["text/typescript", "application/json", "text/plain"].includes(String(record.contentType))
  );
}

function safeParseJson(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    throw new Error("Generated-node role output was not valid JSON.");
  }
}

function numberValue(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function jsonRecordValue(value: unknown): JsonRecord {
  return JSON.parse(JSON.stringify(recordValue(value))) as JsonRecord;
}

function createRolePrompt(input: GeneratedNodeRoleRunInput): string {
  return [
    `You are the ${input.role} agent for a KelpClaw generated-node build.`,
    "Return concise structured output only.",
    "Do not mutate workflow state, resolve secrets, or call external providers.",
    `Workflow id: ${input.request.workflowId}`,
    `Node id: ${input.request.nodeId}`,
    `Iteration: ${input.iteration}`,
    `Input summary: ${input.inputSummary}`,
    `Prompt: ${input.request.prompt}`,
    `Planner rationale: ${input.request.plannerRationale}`,
    `Inputs JSON Schema: ${JSON.stringify(input.request.inputSchema)}`,
    `Outputs JSON Schema: ${JSON.stringify(input.request.outputSchema)}`,
    `Sandbox: ${JSON.stringify(input.request.sandbox)}`,
    input.previousFailure ? `Previous failure: ${input.previousFailure}` : "",
    input.role === "fixer"
      ? "Fixer instruction: triage before repair. Set fixTriage.action to targeted-patch for small local code/payload/runtime issues, retry-codegen for normal regeneration, rearchitect when workflow or node design is wrong, and give-up for external blockers."
      : "",
    `Known output artifacts: ${JSON.stringify(input.outputArtifactRefs)}`
  ]
    .filter((line) => line.length > 0)
    .join("\n");
}

const roleOutputSchema = {
  type: "object",
  required: ["summary", "status", "outputArtifactRefs"],
  additionalProperties: false,
  properties: {
    summary: { type: "string", minLength: 1 },
    status: { enum: ["succeeded", "failed"] },
    outputArtifactRefs: {
      type: "array",
      items: {
        type: "object",
        required: ["path", "checksum", "contentType"],
        additionalProperties: false,
        properties: {
          path: { type: "string", minLength: 1 },
          checksum: { type: "string", minLength: 1 },
          contentType: { enum: ["text/typescript", "application/json", "text/plain"] }
        }
      }
    },
    fixTriage: {
      type: "object",
      required: ["action", "scope", "rationale", "confidence"],
      additionalProperties: false,
      properties: {
        action: {
          enum: ["targeted-patch", "retry-codegen", "rearchitect", "give-up"]
        },
        scope: {
          enum: ["local-code", "node-contract", "workflow-design", "external-blocker"]
        },
        rationale: { type: "string", minLength: 1 },
        confidence: { type: "number", minimum: 0, maximum: 1 }
      }
    }
  }
} as const;
