import type { JsonRecord } from "@kelpclaw/workflow-spec";
import { extractOutputText, usageRecordFromOpenAiResponse } from "./openai-generator.js";
import type {
  OpenAiResponsesCreateRequest,
  OpenAiResponsesResult,
  OpenAiResponsesRunner
} from "./openai-generator.js";
import type {
  GeneratedNodeBuildRole,
  GeneratedNodeRoleRunInput,
  GeneratedNodeRoleRunResult,
  GeneratedNodeRoleRunner,
  WorkflowCodegenArtifactRef
} from "./types.js";

export interface OpenAiGeneratedNodeRoleRunnerOptions {
  readonly role: GeneratedNodeBuildRole;
  readonly apiKey?: string | undefined;
  readonly model?: string | undefined;
  readonly responsesRunner?: OpenAiResponsesRunner | undefined;
}

interface RoleQueryResult {
  readonly summary: string;
  readonly status: "succeeded" | "failed";
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
  readonly response: OpenAiResponsesResult;
}

export class OpenAiGeneratedNodeRoleRunner implements GeneratedNodeRoleRunner {
  public readonly role: GeneratedNodeBuildRole;
  private readonly apiKey: string | undefined;
  private readonly model: string;
  private readonly responsesRunner: OpenAiResponsesRunner | undefined;

  public constructor(options: OpenAiGeneratedNodeRoleRunnerOptions) {
    this.role = options.role;
    this.apiKey = options.apiKey ?? process.env.OPENAI_API_KEY;
    this.model = options.model ?? modelForRole(options.role);
    this.responsesRunner = options.responsesRunner;
  }

  public async run(input: GeneratedNodeRoleRunInput): Promise<GeneratedNodeRoleRunResult> {
    try {
      const runner = await this.getResponsesRunner();
      const roleResult = await runRoleResponse(
        runner,
        this.createResponsesRequest(createRolePrompt(input)),
        input.request.signal,
        input.outputArtifactRefs
      );
      const usage = usageRecordFromOpenAiResponse(roleResult.response);
      const modelInvocation = {
        id: `model.${input.request.job.id}.${input.role}.${input.iteration}.openai`,
        role: input.role,
        inputSummary: input.inputSummary.slice(0, 240),
        outputArtifact:
          roleResult.outputArtifactRefs[0]?.path ?? `agent-run:${input.role}:${input.iteration}`,
        provider: "openai",
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
          modelProvider: "openai",
          model: roleResult.response.model ?? this.model,
          modelCostUsd,
          modelInvocations: [modelInvocation],
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
          modelProvider: "openai",
          model: roleResult.response.model ?? this.model,
          modelCostUsd,
          modelInvocations: [modelInvocation]
        };
      }

      return {
        status: roleResult.status,
        inputSummary: roleResult.summary,
        outputArtifactRefs: roleResult.outputArtifactRefs,
        modelProvider: "openai",
        model: roleResult.response.model ?? this.model,
        modelCostUsd,
        modelInvocations: [modelInvocation]
      };
    } catch (error) {
      return {
        status: "failed",
        inputSummary: input.inputSummary,
        outputArtifactRefs: [],
        modelProvider: "openai",
        model: this.model,
        error: error instanceof Error ? error.message : `${input.role} OpenAI role failed.`
      };
    }
  }

  private async getResponsesRunner(): Promise<OpenAiResponsesRunner> {
    if (this.responsesRunner) {
      return this.responsesRunner;
    }
    if (!this.apiKey) {
      throw new Error("OPENAI_API_KEY is required for OpenAI generated-node role runners.");
    }

    const { default: OpenAI } = await import("openai");
    const client = new OpenAI({ apiKey: this.apiKey });
    return async (request, options) => {
      const requestOptions = options?.signal ? ({ signal: options.signal } as never) : undefined;
      const response = await client.responses.create(request as never, requestOptions);
      return response as unknown as OpenAiResponsesResult;
    };
  }

  private createResponsesRequest(prompt: string): OpenAiResponsesCreateRequest {
    return {
      model: this.model,
      instructions: [
        `You are the ${this.role} agent for a KelpClaw generated-node build.`,
        "Return structured JSON only.",
        "Do not mutate workflow state, resolve secrets, or call external providers."
      ].join("\n"),
      input: prompt,
      text: {
        format: {
          type: "json_schema",
          name: `kelpclaw_${this.role.replace(/-/gu, "_")}_role`,
          strict: true,
          schema: roleOutputSchema
        }
      },
      store: false,
      tools: []
    };
  }
}

export function createOpenAiGeneratedNodeRoleRunners(
  options: {
    readonly apiKey?: string | undefined;
    readonly responsesRunner?: OpenAiResponsesRunner | undefined;
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
      new OpenAiGeneratedNodeRoleRunner({
        role,
        apiKey: options.apiKey,
        model: modelForRole(role),
        responsesRunner: options.responsesRunner
      })
    ])
  ) as Partial<Record<GeneratedNodeBuildRole, GeneratedNodeRoleRunner>>;
}

function modelForRole(role: GeneratedNodeBuildRole): string {
  switch (role) {
    case "workflow-architect":
      return (
        process.env.KELPCLAW_OPENAI_WORKFLOW_ARCHITECT_MODEL ??
        process.env.KELPCLAW_WORKFLOW_ARCHITECT_MODEL ??
        sharedModelFallback()
      );
    case "coder":
      return (
        process.env.KELPCLAW_OPENAI_CODER_MODEL ??
        process.env.KELPCLAW_CODER_MODEL ??
        sharedModelFallback()
      );
    case "tester":
      return (
        process.env.KELPCLAW_OPENAI_TESTER_MODEL ??
        process.env.KELPCLAW_TESTER_MODEL ??
        sharedModelFallback()
      );
    case "runner":
      return (
        process.env.KELPCLAW_OPENAI_RUNNER_MODEL ??
        process.env.KELPCLAW_RUNNER_MODEL ??
        sharedModelFallback()
      );
    case "fixer":
      return (
        process.env.KELPCLAW_OPENAI_FIXER_MODEL ??
        process.env.KELPCLAW_FIXER_MODEL ??
        sharedModelFallback()
      );
    case "evaluator":
      return (
        process.env.KELPCLAW_OPENAI_EVALUATOR_MODEL ??
        process.env.KELPCLAW_EVALUATOR_MODEL ??
        sharedModelFallback()
      );
  }
}

function sharedModelFallback(): string {
  return (
    process.env.KELPCLAW_OPENAI_CODEGEN_MODEL ??
    process.env.KELPCLAW_CODEGEN_MODEL ??
    process.env.KELPCLAW_OPENAI_PLANNER_MODEL ??
    process.env.KELPCLAW_PLANNER_MODEL ??
    "gpt-5.4"
  );
}

async function runRoleResponse(
  runner: OpenAiResponsesRunner,
  request: OpenAiResponsesCreateRequest,
  signal: AbortSignal | undefined,
  fallbackArtifacts: readonly WorkflowCodegenArtifactRef[]
): Promise<RoleQueryResult> {
  const response = await runner(request, { signal });
  const structured = parseRoleStructuredOutput(
    parsedOutputValue(response.output) ?? extractOutputText(response)
  );
  return {
    summary: structured.summary,
    status: structured.status,
    outputArtifactRefs:
      structured.outputArtifactRefs.length > 0 ? structured.outputArtifactRefs : fallbackArtifacts,
    response
  };
}

function parseRoleStructuredOutput(output: unknown): {
  readonly summary: string;
  readonly status: RoleQueryResult["status"];
  readonly outputArtifactRefs: readonly WorkflowCodegenArtifactRef[];
} {
  const parsed = typeof output === "string" ? safeParseJson(output) : output;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("OpenAI generated-node role output must be a JSON object.");
  }
  const record = parsed as {
    readonly summary?: unknown;
    readonly status?: unknown;
    readonly outputArtifactRefs?: unknown;
  };
  if (typeof record.summary !== "string" || record.summary.length === 0) {
    throw new Error("OpenAI generated-node role output requires a summary.");
  }
  const status = record.status === "failed" ? "failed" : "succeeded";
  const outputArtifactRefs = Array.isArray(record.outputArtifactRefs)
    ? record.outputArtifactRefs.filter(isArtifactRef)
    : [];

  return {
    summary: record.summary,
    status,
    outputArtifactRefs
  };
}

function parsedOutputValue(output: unknown): unknown {
  const contentItems = outputContentItems(output);
  for (const item of contentItems) {
    if ("parsed" in item && item.parsed !== undefined) {
      return item.parsed;
    }
  }

  return undefined;
}

function outputContentItems(output: unknown): readonly Record<string, unknown>[] {
  if (!Array.isArray(output)) {
    return [];
  }

  return output.flatMap((item) => {
    if (!item || typeof item !== "object" || Array.isArray(item)) {
      return [];
    }
    const content = (item as { readonly content?: unknown }).content;
    if (!Array.isArray(content)) {
      return [];
    }
    return content.filter(
      (contentItem): contentItem is Record<string, unknown> =>
        !!contentItem && typeof contentItem === "object" && !Array.isArray(contentItem)
    );
  });
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
    throw new Error("OpenAI generated-node role output was not valid JSON.");
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
    }
  }
} as const satisfies JsonRecord;
