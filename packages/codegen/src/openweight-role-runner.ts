import type { JsonRecord } from "@kelpclaw/workflow-spec";
import {
  createOpenWeightChatCompletionsRunner,
  extractOpenWeightOutputText,
  usageRecordFromOpenWeightChatResponse
} from "./openweight-generator.js";
import type {
  OpenWeightChatCompletionRequest,
  OpenWeightChatCompletionResult,
  OpenWeightChatRunner
} from "./openweight-generator.js";
import type {
  GeneratedNodeBuildRole,
  GeneratedNodeFixTriageDecision,
  GeneratedNodeRoleRunInput,
  GeneratedNodeRoleRunResult,
  GeneratedNodeRoleRunner,
  WorkflowCodegenArtifactRef
} from "./types.js";

export interface OpenWeightGeneratedNodeRoleRunnerOptions {
  readonly role: GeneratedNodeBuildRole;
  readonly apiKey?: string | undefined;
  readonly baseUrl?: string | undefined;
  readonly model?: string | undefined;
  readonly chatRunner?: OpenWeightChatRunner | undefined;
}

interface RoleQueryResult {
  readonly summary: string;
  readonly status: "succeeded" | "failed";
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly fixTriage?: GeneratedNodeFixTriageDecision | undefined;
  readonly response: OpenWeightChatCompletionResult;
}

export class OpenWeightGeneratedNodeRoleRunner implements GeneratedNodeRoleRunner {
  public readonly role: GeneratedNodeBuildRole;
  private readonly apiKey: string | undefined;
  private readonly baseUrl: string | undefined;
  private readonly model: string;
  private readonly chatRunner: OpenWeightChatRunner | undefined;

  public constructor(options: OpenWeightGeneratedNodeRoleRunnerOptions) {
    this.role = options.role;
    this.apiKey = options.apiKey ?? process.env.KELPCLAW_OPENWEIGHT_API_KEY;
    this.baseUrl = options.baseUrl ?? process.env.KELPCLAW_OPENWEIGHT_BASE_URL;
    this.model = options.model ?? modelForRole(options.role);
    this.chatRunner = options.chatRunner;
  }

  public async run(input: GeneratedNodeRoleRunInput): Promise<GeneratedNodeRoleRunResult> {
    try {
      const runner = await this.getChatRunner();
      const roleResult = await runRoleChat(
        runner,
        this.createChatRequest(createRolePrompt(input)),
        input.request.signal,
        input.outputArtifactRefs
      );
      const usage = usageRecordFromOpenWeightChatResponse(roleResult.response);
      const modelInvocation = {
        id: `model.${input.request.job.id}.${input.role}.${input.iteration}.openweight`,
        role: input.role,
        inputSummary: input.inputSummary.slice(0, 240),
        outputArtifact:
          roleResult.outputArtifactRefs[0]?.path ?? `agent-run:${input.role}:${input.iteration}`,
        provider: "openweight",
        model: roleResult.response.model ?? this.model,
        determinismExpectation: "bounded" as const,
        retryBudget: {
          maxAttempts: input.request.job.retry.maxAttempts,
          maxCostUsd: input.request.maxModelCostUsd
        },
        correlationId: input.request.job.correlationId,
        createdAt: input.request.generatedAt ?? new Date().toISOString(),
        ...usage
      };
      const modelCostUsd =
        "costUsd" in usage && typeof usage.costUsd === "number" ? usage.costUsd : 0;

      if (roleResult.status === "failed") {
        return {
          status: "failed",
          inputSummary: roleResult.summary,
          outputArtifactRefs: roleResult.outputArtifactRefs,
          modelProvider: "openweight",
          model: roleResult.response.model ?? this.model,
          modelCostUsd,
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
          modelProvider: "openweight",
          model: roleResult.response.model ?? this.model,
          modelCostUsd,
          modelInvocations: [modelInvocation]
        };
      }

      return {
        status: roleResult.status,
        inputSummary: roleResult.summary,
        outputArtifactRefs: roleResult.outputArtifactRefs,
        modelProvider: "openweight",
        model: roleResult.response.model ?? this.model,
        modelCostUsd,
        modelInvocations: [modelInvocation],
        fixTriage: roleResult.fixTriage
      };
    } catch (error) {
      return {
        status: "failed",
        inputSummary: input.inputSummary,
        outputArtifactRefs: [],
        modelProvider: "openweight",
        model: this.model,
        error:
          error instanceof Error ? error.message : `${input.role} open-weight role failed.`
      };
    }
  }

  private async getChatRunner(): Promise<OpenWeightChatRunner> {
    if (this.chatRunner) {
      return this.chatRunner;
    }
    if (!this.baseUrl) {
      throw new Error(
        "KELPCLAW_OPENWEIGHT_BASE_URL is required for open-weight generated-node role runners."
      );
    }
    return createOpenWeightChatCompletionsRunner({
      baseUrl: this.baseUrl,
      apiKey: this.apiKey
    });
  }

  private createChatRequest(prompt: string): OpenWeightChatCompletionRequest {
    return {
      model: this.model,
      messages: [
        {
          role: "system",
          content: [
            `You are the ${this.role} agent for a KelpClaw generated-node build.`,
            "Return one JSON object only.",
            "Do not mutate workflow state, resolve secrets, or call external providers."
          ].join("\n")
        },
        {
          role: "user",
          content: [
            prompt,
            "",
            `Required JSON schema: ${JSON.stringify(roleOutputSchema)}`
          ].join("\n")
        }
      ],
      temperature: 0,
      stream: false,
      response_format: { type: "json_object" }
    };
  }
}

export function createOpenWeightGeneratedNodeRoleRunners(
  options: {
    readonly apiKey?: string | undefined;
    readonly baseUrl?: string | undefined;
    readonly chatRunner?: OpenWeightChatRunner | undefined;
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
      new OpenWeightGeneratedNodeRoleRunner({
        role,
        apiKey: options.apiKey,
        baseUrl: options.baseUrl,
        model: modelForRole(role),
        chatRunner: options.chatRunner
      })
    ])
  ) as Partial<Record<GeneratedNodeBuildRole, GeneratedNodeRoleRunner>>;
}

function modelForRole(role: GeneratedNodeBuildRole): string {
  switch (role) {
    case "workflow-architect":
      return (
        process.env.KELPCLAW_OPENWEIGHT_WORKFLOW_ARCHITECT_MODEL ??
        process.env.KELPCLAW_WORKFLOW_ARCHITECT_MODEL ??
        sharedModelFallback()
      );
    case "coder":
      return (
        process.env.KELPCLAW_OPENWEIGHT_CODER_MODEL ??
        process.env.KELPCLAW_CODER_MODEL ??
        sharedModelFallback()
      );
    case "tester":
      return (
        process.env.KELPCLAW_OPENWEIGHT_TESTER_MODEL ??
        process.env.KELPCLAW_TESTER_MODEL ??
        sharedModelFallback()
      );
    case "runner":
      return (
        process.env.KELPCLAW_OPENWEIGHT_RUNNER_MODEL ??
        process.env.KELPCLAW_RUNNER_MODEL ??
        sharedModelFallback()
      );
    case "fixer":
      return (
        process.env.KELPCLAW_OPENWEIGHT_FIXER_MODEL ??
        process.env.KELPCLAW_FIXER_MODEL ??
        sharedModelFallback()
      );
    case "evaluator":
      return (
        process.env.KELPCLAW_OPENWEIGHT_EVALUATOR_MODEL ??
        process.env.KELPCLAW_EVALUATOR_MODEL ??
        sharedModelFallback()
      );
  }
}

function sharedModelFallback(): string {
  return (
    process.env.KELPCLAW_OPENWEIGHT_CODEGEN_MODEL ??
    process.env.KELPCLAW_CODEGEN_MODEL ??
    process.env.KELPCLAW_OPENWEIGHT_PLANNER_MODEL ??
    process.env.KELPCLAW_PLANNER_MODEL ??
    process.env.KELPCLAW_OPENWEIGHT_MODEL ??
    "qwen2.5-coder"
  );
}

async function runRoleChat(
  runner: OpenWeightChatRunner,
  request: OpenWeightChatCompletionRequest,
  signal: AbortSignal | undefined,
  fallbackArtifacts: readonly WorkflowCodegenArtifactRef[]
): Promise<RoleQueryResult> {
  const response = await runner(request, { signal });
  const structured = parseRoleStructuredOutput(extractOpenWeightOutputText(response));
  return {
    summary: structured.summary,
    status: structured.status,
    outputArtifactRefs:
      structured.outputArtifactRefs.length > 0 ? structured.outputArtifactRefs : fallbackArtifacts,
    fixTriage: structured.fixTriage,
    response
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
    throw new Error("Open-weight generated-node role output must be a JSON object.");
  }
  const record = parsed as {
    readonly summary?: unknown;
    readonly status?: unknown;
    readonly outputArtifactRefs?: unknown;
    readonly fixTriage?: unknown;
  };
  if (typeof record.summary !== "string" || record.summary.length === 0) {
    throw new Error("Open-weight generated-node role output requires a summary.");
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
    throw new Error("Open-weight generated-node role output was not valid JSON.");
  }
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
} as const satisfies JsonRecord;
