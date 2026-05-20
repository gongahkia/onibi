import type { Options, SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import type { JsonRecord, JsonValue, WorkflowAgenticNodePolicy } from "@kelpclaw/workflow-spec";
import type { CompiledDagNode, NodeRunContext, NodeRunner, NodeRunnerResult } from "./types.js";

export type AgenticProvider = "anthropic" | "openai";

export interface OpenAiAgenticResponsesRequest {
  readonly model: string;
  readonly instructions: string;
  readonly input: string;
  readonly tools: readonly JsonRecord[];
  readonly text: {
    readonly format: {
      readonly type: "json_schema";
      readonly name: string;
      readonly strict: boolean;
      readonly schema: JsonRecord;
    };
  };
  readonly store: boolean;
}

export interface OpenAiAgenticResponsesResult {
  readonly id?: string | undefined;
  readonly model?: string | undefined;
  readonly output_text?: string | undefined;
  readonly output?: unknown;
  readonly usage?: unknown;
}

export type OpenAiAgenticResponsesRunner = (
  request: OpenAiAgenticResponsesRequest,
  options?: { readonly signal?: AbortSignal | undefined } | undefined
) => Promise<OpenAiAgenticResponsesResult>;

export type AgenticQueryRunner = (
  prompt: string,
  options: Options
) => AsyncIterable<Pick<SDKMessage, "type"> & Record<string, unknown>>;

export interface AgenticResearchNodeRunnerOptions {
  readonly provider?: AgenticProvider | undefined;
  readonly apiKey?: string | undefined;
  readonly model?: string | undefined;
  readonly openAiRunner?: OpenAiAgenticResponsesRunner | undefined;
  readonly anthropicRunner?: AgenticQueryRunner | undefined;
}

interface ResearchSource {
  readonly title: string;
  readonly url: string;
  readonly snippet?: string | undefined;
}

interface ResearchOutput {
  readonly summary: string;
  readonly sources: readonly ResearchSource[];
  readonly limitations: readonly string[];
}

export class AgenticResearchNodeRunner implements NodeRunner {
  private readonly provider: AgenticProvider;
  private readonly apiKey: string | undefined;
  private readonly model: string;
  private readonly openAiRunner: OpenAiAgenticResponsesRunner | undefined;
  private readonly anthropicRunner: AgenticQueryRunner | undefined;

  public constructor(options: AgenticResearchNodeRunnerOptions = {}) {
    this.provider = options.provider ?? agenticProviderFromEnv();
    this.apiKey = options.apiKey ?? apiKeyForProvider(this.provider);
    this.model = options.model ?? modelForProvider(this.provider);
    this.openAiRunner = options.openAiRunner;
    this.anthropicRunner = options.anthropicRunner;
  }

  public async run(node: CompiledDagNode, context: NodeRunContext): Promise<NodeRunnerResult> {
    if (!isAgenticNode(node)) {
      return {
        status: "failed",
        output: {},
        error: `Node '${node.id}' is not configured for agentic execution.`
      };
    }

    try {
      const research =
        this.provider === "openai"
          ? await this.runOpenAi(node, context)
          : await this.runAnthropic(node, context);
      return {
        status: "succeeded",
        output: {
          result: research as unknown as JsonValue
        },
        metadata: {
          agentic: true,
          provider: this.provider,
          model: this.model,
          sourceCount: research.sources.length,
          tools: node.agentic?.tools ?? []
        }
      };
    } catch (error) {
      return {
        status: "failed",
        output: {},
        error: error instanceof Error ? error.message : "Agentic research execution failed.",
        metadata: {
          agentic: true,
          provider: this.provider,
          model: this.model
        }
      };
    }
  }

  private async runOpenAi(
    node: CompiledDagNode,
    context: NodeRunContext
  ): Promise<ResearchOutput> {
    const runner = await this.getOpenAiRunner();
    const response = await runner(
      {
        model: this.model,
        instructions: agenticInstructions(node.agentic),
        input: researchPrompt(node, context),
        tools: openAiToolsForPolicy(node.agentic),
        text: {
          format: {
            type: "json_schema",
            name: "kelpclaw_research_result",
            strict: true,
            schema: researchOutputSchema
          }
        },
        store: false
      },
      { signal: context.signal }
    );
    return parseResearchOutput(extractOpenAiOutputText(response));
  }

  private async runAnthropic(
    node: CompiledDagNode,
    context: NodeRunContext
  ): Promise<ResearchOutput> {
    const runner = await this.getAnthropicRunner();
    const abortController = abortControllerForSignal(context.signal);
    let result: unknown;
    for await (const message of runner(researchPrompt(node, context), {
      maxTurns: node.agentic?.budget.maxIterations ?? 3,
      tools: ["WebSearch", "WebFetch"],
      allowedTools: ["WebSearch", "WebFetch"],
      abortController,
      env: {
        ...process.env,
        ANTHROPIC_API_KEY: this.apiKey,
        CLAUDE_AGENT_SDK_CLIENT_APP: "kelpclaw-agentic-research/0.1.0"
      },
      outputFormat: {
        type: "json_schema",
        schema: researchOutputSchema
      },
      ...(this.model ? { model: this.model } : {})
    })) {
      if (message.type === "result") {
        result = message.structured_output ?? message.result;
      }
    }

    if (result === undefined) {
      throw new Error("Anthropic Agent SDK did not return a research result.");
    }

    return parseResearchOutput(result);
  }

  private async getOpenAiRunner(): Promise<OpenAiAgenticResponsesRunner> {
    if (this.openAiRunner) {
      return this.openAiRunner;
    }
    if (!this.apiKey) {
      throw new Error("OPENAI_API_KEY is required for OpenAI agentic research.");
    }

    const { default: OpenAI } = await import("openai");
    const client = new OpenAI({ apiKey: this.apiKey });
    return async (request, options) => {
      const requestOptions = options?.signal ? ({ signal: options.signal } as never) : undefined;
      const response = await client.responses.create(request as never, requestOptions);
      return response as unknown as OpenAiAgenticResponsesResult;
    };
  }

  private async getAnthropicRunner(): Promise<AgenticQueryRunner> {
    if (this.anthropicRunner) {
      return this.anthropicRunner;
    }
    if (!this.apiKey) {
      throw new Error("ANTHROPIC_API_KEY is required for Anthropic agentic research.");
    }

    const sdk = await import("@anthropic-ai/claude-agent-sdk");
    return (prompt, options) => sdk.query({ prompt, options });
  }
}

export function isAgenticNode(node: CompiledDagNode): boolean {
  return node.agentic !== undefined || node.config.skillMode === "agentic";
}

function agenticProviderFromEnv(): AgenticProvider {
  const provider =
    process.env.KELPCLAW_AGENTIC_PROVIDER ??
    process.env.KELPCLAW_CODEGEN_PROVIDER ??
    process.env.KELPCLAW_PLANNER_PROVIDER ??
    "anthropic";
  if (provider === "openai" || provider === "anthropic") {
    return provider;
  }

  throw new Error("KELPCLAW_AGENTIC_PROVIDER must be 'openai' or 'anthropic'.");
}

function apiKeyForProvider(provider: AgenticProvider): string | undefined {
  return provider === "openai" ? process.env.OPENAI_API_KEY : process.env.ANTHROPIC_API_KEY;
}

function modelForProvider(provider: AgenticProvider): string {
  if (provider === "openai") {
    return (
      process.env.KELPCLAW_OPENAI_AGENTIC_MODEL ??
      process.env.KELPCLAW_AGENTIC_MODEL ??
      process.env.KELPCLAW_OPENAI_PLANNER_MODEL ??
      process.env.KELPCLAW_PLANNER_MODEL ??
      "gpt-5.4"
    );
  }

  return (
    process.env.KELPCLAW_ANTHROPIC_AGENTIC_MODEL ??
    process.env.KELPCLAW_AGENTIC_MODEL ??
    process.env.KELPCLAW_PLANNER_MODEL ??
    "claude-sonnet-4-5-20250929"
  );
}

function openAiToolsForPolicy(
  policy: WorkflowAgenticNodePolicy | undefined
): readonly JsonRecord[] {
  if (!policy?.tools.includes("web-search")) {
    return [];
  }

  return [{ type: "web_search_preview" }];
}

function agenticInstructions(policy: WorkflowAgenticNodePolicy | undefined): string {
  return [
    "You are KelpClaw's bounded research agent.",
    "Use web search when available and cite concrete sources.",
    "Return structured JSON only.",
    "Do not resolve secrets, mutate workflow state, send messages, or deploy anything.",
    `Human approval boundaries: ${(policy?.humanApprovalBoundaries ?? []).join("; ") || "none"}.`,
    `Stop conditions: ${(policy?.stopConditions ?? []).join("; ") || "research complete"}.`
  ].join("\n");
}

function researchPrompt(node: CompiledDagNode, context: NodeRunContext): string {
  return [
    agenticInstructions(node.agentic),
    "",
    `Workflow: ${context.dag.workflowId} r${context.dag.revision}`,
    `Node: ${node.id} (${node.label})`,
    `Node description: ${node.description}`,
    `Node config: ${JSON.stringify(node.config)}`,
    `Input payload: ${JSON.stringify(context.input)}`,
    "",
    "Return JSON with summary, sources, and limitations."
  ].join("\n");
}

function parseResearchOutput(output: unknown): ResearchOutput {
  const parsed = typeof output === "string" ? safeParseJson(extractJsonObject(output)) : output;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("Agentic research output must be a JSON object.");
  }

  const record = parsed as Partial<ResearchOutput>;
  if (typeof record.summary !== "string" || record.summary.trim().length === 0) {
    throw new Error("Agentic research output is missing summary.");
  }

  return {
    summary: record.summary,
    sources: Array.isArray(record.sources)
      ? record.sources.map((source) => normalizeSource(source)).filter((source) => source.url)
      : [],
    limitations: Array.isArray(record.limitations)
      ? record.limitations.filter((limitation): limitation is string => typeof limitation === "string")
      : []
  };
}

function normalizeSource(source: unknown): ResearchSource {
  const record = source && typeof source === "object" ? (source as Record<string, unknown>) : {};
  return {
    title: typeof record.title === "string" ? record.title : "Source",
    url: typeof record.url === "string" ? record.url : "",
    ...(typeof record.snippet === "string" ? { snippet: record.snippet } : {})
  };
}

function extractOpenAiOutputText(response: OpenAiAgenticResponsesResult): string {
  if (typeof response.output_text === "string") {
    return response.output_text.trim();
  }

  return outputTextValues(response.output).join("\n").trim();
}

function outputTextValues(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.flatMap(outputTextValues);
  }
  if (!value || typeof value !== "object") {
    return [];
  }

  const record = value as Record<string, unknown>;
  const direct =
    typeof record.text === "string"
      ? [record.text]
      : typeof record.output_text === "string"
        ? [record.output_text]
        : [];
  return [...direct, ...outputTextValues(record.content), ...outputTextValues(record.output)];
}

function extractJsonObject(value: string): string {
  const trimmed = value.trim();
  if (trimmed.startsWith("{") && trimmed.endsWith("}")) {
    return trimmed;
  }

  const start = trimmed.indexOf("{");
  const end = trimmed.lastIndexOf("}");
  if (start >= 0 && end > start) {
    return trimmed.slice(start, end + 1);
  }

  return trimmed;
}

function safeParseJson(value: string): unknown {
  try {
    return JSON.parse(value);
  } catch {
    throw new Error("Agentic research output was not valid JSON.");
  }
}

function abortControllerForSignal(signal: AbortSignal | undefined): AbortController | undefined {
  if (!signal) {
    return undefined;
  }

  const controller = new AbortController();
  if (signal.aborted) {
    controller.abort(signal.reason);
    return controller;
  }

  signal.addEventListener("abort", () => controller.abort(signal.reason), { once: true });
  return controller;
}

const researchOutputSchema = {
  type: "object",
  required: ["summary", "sources", "limitations"],
  additionalProperties: false,
  properties: {
    summary: { type: "string", minLength: 1 },
    sources: {
      type: "array",
      items: {
        type: "object",
        required: ["title", "url"],
        additionalProperties: false,
        properties: {
          title: { type: "string" },
          url: { type: "string" },
          snippet: { type: "string" }
        }
      }
    },
    limitations: {
      type: "array",
      items: { type: "string" }
    }
  }
} as const satisfies JsonRecord;
