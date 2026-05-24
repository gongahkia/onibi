import { createCodegenMetadata, createGeneratedArtifact } from "./artifacts.js";
import {
  assertDependencyManifestPolicy,
  createDependencyManifestArtifact,
  dependencyManifestFromArtifact
} from "./dependency-policy.js";
import type { DependencyManifestInput } from "./dependency-policy.js";
import type { CodegenGenerationRequest, CodegenGenerationResult, CodeGenerator } from "./types.js";

export interface OpenAiCodeGeneratorOptions {
  readonly apiKey?: string | undefined;
  readonly model?: string | undefined;
  readonly maxRepairAttempts?: number | undefined;
  readonly responsesRunner?: OpenAiResponsesRunner | undefined;
}

export interface OpenAiResponsesCreateRequest {
  readonly model: string;
  readonly instructions: string;
  readonly input: string;
  readonly text: {
    readonly format: {
      readonly type: "json_schema";
      readonly name: string;
      readonly strict: boolean;
      readonly schema: Readonly<Record<string, unknown>>;
    };
  };
  readonly store: boolean;
  readonly tools: readonly [];
}

export interface OpenAiResponsesRunOptions {
  readonly signal?: AbortSignal | undefined;
}

export interface OpenAiResponsesResult {
  readonly id?: string | undefined;
  readonly model?: string | undefined;
  readonly output_text?: string | undefined;
  readonly output?: unknown;
  readonly usage?: unknown;
  readonly total_cost_usd?: number | undefined;
}

export type OpenAiResponsesRunner = (
  request: OpenAiResponsesCreateRequest,
  options?: OpenAiResponsesRunOptions | undefined
) => Promise<OpenAiResponsesResult>;

export interface AzureOpenAiResponsesConfig {
  readonly apiKey: string;
  readonly endpoint: string;
  readonly deployment: string;
  readonly apiVersion: string;
}

interface OpenAiStructuredOutput {
  readonly sourceCode: string;
  readonly packageManager: DependencyManifestInput["packageManager"];
  readonly dependencies: readonly string[];
  readonly devDependencies: readonly string[];
  readonly installCommand: readonly string[];
}

export class OpenAiCodeGenerator implements CodeGenerator {
  private readonly apiKey: string | undefined;
  private readonly model: string;
  private readonly maxRepairAttempts: number;
  private readonly responsesRunner: OpenAiResponsesRunner | undefined;

  public constructor(options: OpenAiCodeGeneratorOptions = {}) {
    this.apiKey = options.apiKey ?? process.env.OPENAI_API_KEY;
    this.model = options.model ?? openAiModelFromEnv("gpt-5.4");
    this.maxRepairAttempts = options.maxRepairAttempts ?? 1;
    this.responsesRunner = options.responsesRunner;
  }

  public async generate(request: CodegenGenerationRequest): Promise<CodegenGenerationResult> {
    const runner = await this.getResponsesRunner();
    let prompt = createGenerationPrompt(request);
    let lastError: Error | null = null;

    for (let attempt = 0; attempt <= this.maxRepairAttempts; attempt += 1) {
      try {
        const structured = parseStructuredOutput(
          await runStructuredResponse(runner, this.createResponsesRequest(prompt))
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
            generator: "openai.responses",
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
        lastError = error instanceof Error ? error : new Error("OpenAI code generation failed.");
        prompt = createRepairPrompt(request, lastError.message);
      }
    }

    throw lastError ?? new Error("OpenAI code generation failed.");
  }

  private async getResponsesRunner(): Promise<OpenAiResponsesRunner> {
    if (this.responsesRunner) {
      return this.responsesRunner;
    }
    const azure = resolveAzureOpenAiResponsesConfig(this.apiKey);
    if (azure) {
      return createAzureOpenAiResponsesRunner(azure);
    }

    if (!this.apiKey) {
      throw new Error(
        "OPENAI_API_KEY or GPT5_MINI_API_KEY/GPT5_PRO_API_KEY is required for OpenAI live code generation."
      );
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
        "You are KelpClaw's generated-node code author.",
        "Return structured JSON only.",
        "Do not call external providers, read secrets, mutate workflows, or use undeclared network access."
      ].join("\n"),
      input: prompt,
      text: {
        format: {
          type: "json_schema",
          name: "kelpclaw_generated_node",
          strict: true,
          schema: generatedCodeSchema
        }
      },
      store: false,
      tools: []
    };
  }
}

export function openAiModelFromEnv(fallback: string): string {
  return (
    process.env.KELPCLAW_OPENAI_CODEGEN_MODEL ??
    process.env.KELPCLAW_CODEGEN_MODEL ??
    process.env.KELPCLAW_OPENAI_PLANNER_MODEL ??
    process.env.KELPCLAW_PLANNER_MODEL ??
    process.env.GPT5_MINI_DEPLOYMENT ??
    process.env.GPT5_PRO_DEPLOYMENT ??
    fallback
  );
}

export function resolveAzureOpenAiResponsesConfig(
  apiKeyOverride?: string | undefined
): AzureOpenAiResponsesConfig | undefined {
  const endpoint =
    readEnv("KELPCLAW_AZURE_OPENAI_ENDPOINT") ??
    readEnv("GPT5_MINI_ENDPOINT") ??
    readEnv("GPT5_PRO_ENDPOINT") ??
    readEnv("AZURE_ENDPOINT");
  const deployment =
    readEnv("KELPCLAW_AZURE_OPENAI_DEPLOYMENT") ??
    readEnv("GPT5_MINI_DEPLOYMENT") ??
    readEnv("GPT5_PRO_DEPLOYMENT");
  const apiVersion =
    readEnv("KELPCLAW_AZURE_OPENAI_API_VERSION") ??
    readEnv("GPT5_MINI_API_VERSION") ??
    readEnv("GPT5_PRO_API_VERSION") ??
    readEnv("API_VERSION");
  const apiKey =
    readEnv("KELPCLAW_AZURE_OPENAI_API_KEY") ||
    readEnv("GPT5_MINI_API_KEY") ||
    readEnv("GPT5_PRO_API_KEY") ||
    apiKeyOverride ||
    readEnv("OPENAI_API_KEY");

  if (!endpoint || !deployment || !apiVersion || !apiKey) {
    return undefined;
  }

  return {
    apiKey,
    endpoint: endpoint.replace(/\/+$/u, ""),
    deployment,
    apiVersion
  };
}

export function createAzureOpenAiResponsesRunner(
  config: AzureOpenAiResponsesConfig
): OpenAiResponsesRunner {
  return async (request, options) => {
    const url = new URL(
      `${config.endpoint}/openai/deployments/${encodeURIComponent(config.deployment)}/responses`
    );
    url.searchParams.set("api-version", config.apiVersion);
    const response = await fetch(url, {
      body: JSON.stringify({ ...request, model: config.deployment }),
      headers: {
        "Content-Type": "application/json",
        "api-key": config.apiKey
      },
      method: "POST",
      ...(options?.signal ? { signal: options.signal } : {})
    });
    if (!response.ok) {
      if (response.status === 404) {
        return await runAzureOpenAiChatCompletionsFallback(config, request, options);
      }
      throw await createAzureOpenAiRequestError("Responses", response);
    }
    return (await response.json()) as OpenAiResponsesResult;
  };
}

async function runAzureOpenAiChatCompletionsFallback(
  config: AzureOpenAiResponsesConfig,
  request: OpenAiResponsesCreateRequest,
  options?: OpenAiResponsesRunOptions | undefined
): Promise<OpenAiResponsesResult> {
  const url = new URL(
    `${config.endpoint}/openai/deployments/${encodeURIComponent(config.deployment)}/chat/completions`
  );
  url.searchParams.set("api-version", config.apiVersion);
  const responseFormat = request.text.format;
  const response = await fetch(url, {
    body: JSON.stringify({
      messages: [
        { role: "system", content: request.instructions },
        { role: "user", content: request.input }
      ],
      response_format: {
        type: "json_schema",
        json_schema: {
          name: responseFormat.name,
          strict: responseFormat.strict,
          schema: responseFormat.schema
        }
      }
    }),
    headers: {
      "Content-Type": "application/json",
      "api-key": config.apiKey
    },
    method: "POST",
    ...(options?.signal ? { signal: options.signal } : {})
  });
  if (!response.ok) {
    throw await createAzureOpenAiRequestError("Chat Completions", response);
  }
  const chatResponse = (await response.json()) as Record<string, unknown>;
  return {
    id: stringValue(chatResponse.id),
    model: stringValue(chatResponse.model),
    output_text: azureChatMessageContent(chatResponse),
    usage: chatResponse.usage
  };
}

async function createAzureOpenAiRequestError(api: string, response: Response): Promise<Error> {
  const body = await safeReadJson(response);
  const error = recordValue(recordValue(body).error);
  const code = stringValue(error.code);
  const suffix = code ? ` (${code})` : "";
  return new Error(`Azure OpenAI ${api} request failed: ${response.status}${suffix}`);
}

async function safeReadJson(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch {
    return undefined;
  }
}

function azureChatMessageContent(response: Record<string, unknown>): string {
  const choices = Array.isArray(response.choices) ? response.choices : [];
  for (const choice of choices) {
    const content = recordValue(recordValue(choice).message).content;
    if (typeof content === "string") {
      return content.trim();
    }
  }
  return "";
}

function readEnv(name: string): string | undefined {
  const value = process.env[name]?.trim();
  return value ? value : undefined;
}

async function runStructuredResponse(
  runner: OpenAiResponsesRunner,
  request: OpenAiResponsesCreateRequest
): Promise<unknown> {
  const response = await runner(request);
  const parsed = parsedOutputValue(response.output);
  if (parsed !== undefined) {
    return parsed;
  }

  const outputText = extractOutputText(response);
  if (outputText.length === 0) {
    throw new Error("OpenAI Responses API did not return structured output.");
  }

  return outputText;
}

function parseStructuredOutput(output: unknown): OpenAiStructuredOutput {
  const parsed = typeof output === "string" ? safeParseJson(output) : output;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("Generated code output must be a JSON object.");
  }

  const record = parsed as Partial<OpenAiStructuredOutput>;
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

  return record as OpenAiStructuredOutput;
}

export function extractOutputText(response: OpenAiResponsesResult): string {
  if (typeof response.output_text === "string") {
    return response.output_text.trim();
  }

  return outputTextValues(response.output).join("\n").trim();
}

export function usageRecordFromOpenAiResponse(response: OpenAiResponsesResult) {
  const usage = recordValue(response.usage);
  const inputTokens = numberValue(usage.input_tokens) || numberValue(usage.inputTokens);
  const outputTokens = numberValue(usage.output_tokens) || numberValue(usage.outputTokens);
  const cachedTokens =
    numberValue(recordValue(usage.input_tokens_details).cached_tokens) ||
    numberValue(recordValue(usage.inputTokensDetails).cachedTokens);
  const reasoningTokens =
    numberValue(recordValue(usage.output_tokens_details).reasoning_tokens) ||
    numberValue(recordValue(usage.outputTokensDetails).reasoningTokens);
  const totalTokens =
    numberValue(usage.total_tokens) || numberValue(usage.totalTokens) || inputTokens + outputTokens;
  const costUsd =
    numberValue(response.total_cost_usd) ||
    numberValue(recordValue(response as unknown as Record<string, unknown>).totalCostUsd);

  return {
    ...(inputTokens > 0 ? { inputTokens } : {}),
    ...(outputTokens > 0 ? { outputTokens } : {}),
    ...(cachedTokens > 0 ? { cacheReadInputTokens: cachedTokens } : {}),
    ...(reasoningTokens > 0 ? { modelUsage: { reasoningTokens } } : {}),
    ...(totalTokens > 0 ? { totalTokens } : {}),
    ...(costUsd > 0 ? { costUsd } : {})
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

function outputTextValues(output: unknown): readonly string[] {
  return outputContentItems(output)
    .map((item) => (typeof item.text === "string" ? item.text : ""))
    .filter((text) => text.length > 0);
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

function stringValue(value: unknown): string | undefined {
  return typeof value === "string" && value.length > 0 ? value : undefined;
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
