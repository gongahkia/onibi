import { createCodegenMetadata, createGeneratedArtifact } from "./artifacts.js";
import {
  assertDependencyManifestPolicy,
  createDependencyManifestArtifact,
  dependencyManifestFromArtifact
} from "./dependency-policy.js";
import type { DependencyManifestInput } from "./dependency-policy.js";
import type { CodegenGenerationRequest, CodegenGenerationResult, CodeGenerator } from "./types.js";

export interface OpenWeightCodeGeneratorOptions {
  readonly apiKey?: string | undefined;
  readonly baseUrl?: string | undefined;
  readonly model?: string | undefined;
  readonly maxRepairAttempts?: number | undefined;
  readonly chatRunner?: OpenWeightChatRunner | undefined;
}

export interface OpenWeightChatMessage {
  readonly role: "system" | "user" | "assistant";
  readonly content: string;
}

export interface OpenWeightChatCompletionRequest {
  readonly model: string;
  readonly messages: readonly OpenWeightChatMessage[];
  readonly temperature: number;
  readonly stream: false;
  readonly response_format: {
    readonly type: "json_object";
  };
}

export interface OpenWeightChatRunOptions {
  readonly signal?: AbortSignal | undefined;
}

export interface OpenWeightChatCompletionResult {
  readonly id?: string | undefined;
  readonly model?: string | undefined;
  readonly choices?: readonly {
    readonly message?: {
      readonly content?: string | null | undefined;
    };
  }[];
  readonly usage?: unknown;
  readonly total_cost_usd?: number | undefined;
}

export type OpenWeightChatRunner = (
  request: OpenWeightChatCompletionRequest,
  options?: OpenWeightChatRunOptions | undefined
) => Promise<OpenWeightChatCompletionResult>;

export interface OpenWeightChatCompletionsConfig {
  readonly baseUrl: string;
  readonly apiKey?: string | undefined;
}

interface OpenWeightStructuredOutput {
  readonly sourceCode: string;
  readonly packageManager: DependencyManifestInput["packageManager"];
  readonly dependencies: readonly string[];
  readonly devDependencies: readonly string[];
  readonly installCommand: readonly string[];
}

export class OpenWeightCodeGenerator implements CodeGenerator {
  private readonly apiKey: string | undefined;
  private readonly baseUrl: string | undefined;
  private readonly model: string;
  private readonly maxRepairAttempts: number;
  private readonly chatRunner: OpenWeightChatRunner | undefined;

  public constructor(options: OpenWeightCodeGeneratorOptions = {}) {
    this.apiKey = options.apiKey ?? process.env.KELPCLAW_OPENWEIGHT_API_KEY;
    this.baseUrl = options.baseUrl ?? process.env.KELPCLAW_OPENWEIGHT_BASE_URL;
    this.model = options.model ?? openWeightModelFromEnv("qwen2.5-coder");
    this.maxRepairAttempts = options.maxRepairAttempts ?? 1;
    this.chatRunner = options.chatRunner;
  }

  public async generate(request: CodegenGenerationRequest): Promise<CodegenGenerationResult> {
    const runner = await this.getChatRunner();
    let prompt = createGenerationPrompt(request);
    let lastError: Error | null = null;

    for (let attempt = 0; attempt <= this.maxRepairAttempts; attempt += 1) {
      try {
        const structured = parseStructuredOutput(
          await runStructuredChat(runner, this.createChatRequest(prompt))
        );
        const dependencyManifestArtifact = createDependencyManifestArtifact({
          packageManager: structured.packageManager,
          dependencies: structured.dependencies,
          devDependencies: structured.devDependencies,
          installCommand: structured.installCommand
        });
        const dependencyManifest = dependencyManifestFromArtifact(dependencyManifestArtifact, {
          packageManager: structured.packageManager,
          dependencies: structured.dependencies,
          devDependencies: structured.devDependencies,
          installCommand: structured.installCommand
        });
        assertDependencyManifestPolicy(dependencyManifest);

        const sourceArtifact = createGeneratedArtifact({
          path: `generated/${request.nodeId}.ts`,
          content: structured.sourceCode.endsWith("\n")
            ? structured.sourceCode
            : `${structured.sourceCode}\n`,
          contentType: "text/typescript"
        });

        return {
          sourceArtifact,
          dependencyManifestArtifact,
          dependencyManifest,
          metadata: createCodegenMetadata({
            generator: "openweight.chat-completions",
            generatedAt: request.generatedAt ?? new Date().toISOString(),
            sourcePrompt: request.prompt,
            plannerRationale: request.plannerRationale,
            artifact: sourceArtifact,
            dependencyManifest,
            sandbox: request.sandbox,
            replay: {
              mode: "reuse-if-unchanged",
              seed: `${request.workflowId}.${request.nodeId}`
            },
            llmBacked: true
          })
        };
      } catch (error) {
        lastError =
          error instanceof Error ? error : new Error("Open-weight code generation failed.");
        prompt = createRepairPrompt(request, lastError.message);
      }
    }

    throw lastError ?? new Error("Open-weight code generation failed.");
  }

  private async getChatRunner(): Promise<OpenWeightChatRunner> {
    if (this.chatRunner) {
      return this.chatRunner;
    }
    if (!this.baseUrl) {
      throw new Error("KELPCLAW_OPENWEIGHT_BASE_URL is required for open-weight code generation.");
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
            "You are KelpClaw's generated-node code author.",
            "Return one JSON object only.",
            "Do not call external providers, read secrets, mutate workflows, or use undeclared network access."
          ].join("\n")
        },
        {
          role: "user",
          content: [
            prompt,
            "",
            `Required JSON schema: ${JSON.stringify(generatedCodeSchema)}`
          ].join("\n")
        }
      ],
      temperature: 0,
      stream: false,
      response_format: { type: "json_object" }
    };
  }
}

export function createOpenWeightChatCompletionsRunner(
  config: OpenWeightChatCompletionsConfig
): OpenWeightChatRunner {
  const baseUrl = config.baseUrl.replace(/\/+$/u, "");
  return async (request, options) => {
    const response = await fetch(`${baseUrl}/chat/completions`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        ...(config.apiKey ? { authorization: `Bearer ${config.apiKey}` } : {})
      },
      body: JSON.stringify(request),
      ...(options?.signal ? { signal: options.signal } : {})
    });
    if (!response.ok) {
      const body = await response.text();
      throw new Error(
        `Open-weight chat completions request failed: ${response.status}${body ? ` ${body}` : ""}`
      );
    }
    return (await response.json()) as OpenWeightChatCompletionResult;
  };
}

export function openWeightModelFromEnv(fallback: string): string {
  return (
    process.env.KELPCLAW_OPENWEIGHT_CODEGEN_MODEL ??
    process.env.KELPCLAW_CODEGEN_MODEL ??
    process.env.KELPCLAW_OPENWEIGHT_PLANNER_MODEL ??
    process.env.KELPCLAW_PLANNER_MODEL ??
    process.env.KELPCLAW_OPENWEIGHT_MODEL ??
    fallback
  );
}

export function extractOpenWeightOutputText(response: OpenWeightChatCompletionResult): string {
  return (response.choices ?? [])
    .map((choice) => choice.message?.content ?? "")
    .filter((content) => content.length > 0)
    .join("\n")
    .trim();
}

export function usageRecordFromOpenWeightChatResponse(response: OpenWeightChatCompletionResult) {
  const usage = recordValue(response.usage);
  const inputTokens = numberValue(usage.prompt_tokens) || numberValue(usage.input_tokens);
  const outputTokens = numberValue(usage.completion_tokens) || numberValue(usage.output_tokens);
  const totalTokens =
    numberValue(usage.total_tokens) || numberValue(usage.totalTokens) || inputTokens + outputTokens;
  const costUsd =
    numberValue(response.total_cost_usd) ||
    numberValue(recordValue(response as unknown as Record<string, unknown>).totalCostUsd);

  return {
    ...(inputTokens > 0 ? { inputTokens } : {}),
    ...(outputTokens > 0 ? { outputTokens } : {}),
    ...(totalTokens > 0 ? { totalTokens } : {}),
    ...(costUsd > 0 ? { costUsd } : {})
  };
}

async function runStructuredChat(
  runner: OpenWeightChatRunner,
  request: OpenWeightChatCompletionRequest
): Promise<unknown> {
  const outputText = extractOpenWeightOutputText(await runner(request));
  if (outputText.length === 0) {
    throw new Error("Open-weight chat completions did not return structured output.");
  }
  return outputText;
}

function parseStructuredOutput(output: unknown): OpenWeightStructuredOutput {
  const parsed = typeof output === "string" ? safeParseJson(output) : output;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("Generated code output must be a JSON object.");
  }

  const record = parsed as Partial<OpenWeightStructuredOutput>;
  if (
    typeof record.sourceCode !== "string" ||
    !["none", "npm", "pnpm"].includes(String(record.packageManager)) ||
    !Array.isArray(record.dependencies) ||
    !Array.isArray(record.devDependencies) ||
    !Array.isArray(record.installCommand) ||
    !record.dependencies.every((dependency) => typeof dependency === "string") ||
    !record.devDependencies.every((dependency) => typeof dependency === "string") ||
    !record.installCommand.every((part) => typeof part === "string")
  ) {
    throw new Error("Generated code output does not match the required schema.");
  }

  return record as OpenWeightStructuredOutput;
}

function safeParseJson(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    throw new Error("Generated code output was not valid JSON.");
  }
}

function createGenerationPrompt(request: CodegenGenerationRequest): string {
  return [
    "Generate a deterministic TypeScript NanoClaw node implementation.",
    "Return only structured output that matches the requested JSON schema.",
    "The code must read JSON from process.env.NANOCLAW_NODE_INPUT and write a JSON object to process.env.NANOCLAW_NODE_OUTPUT.",
    "Do not include undeclared network calls, filesystem reads outside the node workspace, secrets, replanning, or workflow mutation.",
    `Workflow id: ${request.workflowId}`,
    `Node id: ${request.nodeId}`,
    `Node prompt: ${request.prompt}`,
    `Planner rationale: ${request.plannerRationale}`,
    `Inputs JSON Schema: ${JSON.stringify(request.inputSchema)}`,
    `Outputs JSON Schema: ${JSON.stringify(request.outputSchema)}`,
    `Sandbox: ${JSON.stringify(request.sandbox)}`,
    `Runtime image: ${request.runtime.image}`
  ].join("\n");
}

function createRepairPrompt(request: CodegenGenerationRequest, error: string): string {
  return [
    createGenerationPrompt(request),
    "",
    "Repair the previous generated output.",
    `Validation error: ${error}`
  ].join("\n");
}

function numberValue(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

const generatedCodeSchema = {
  type: "object",
  required: ["sourceCode", "packageManager", "dependencies", "devDependencies", "installCommand"],
  additionalProperties: false,
  properties: {
    sourceCode: { type: "string", minLength: 1 },
    packageManager: { enum: ["none", "npm", "pnpm"] },
    dependencies: {
      type: "array",
      items: { type: "string", minLength: 1 }
    },
    devDependencies: {
      type: "array",
      items: { type: "string", minLength: 1 }
    },
    installCommand: {
      type: "array",
      items: { type: "string", minLength: 1 }
    }
  }
} as const;
