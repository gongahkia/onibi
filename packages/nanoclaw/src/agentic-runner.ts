import { createHash, randomUUID } from "node:crypto";
import type { Options, SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import { redactJsonRecord, stableJsonStringify } from "@kelpclaw/workflow-spec";
import type {
  JsonRecord,
  JsonValue,
  WorkflowAgentMemoryRecord,
  WorkflowAgentMemoryScope,
  WorkflowAgenticNodePolicy,
  WorkflowAgenticToolGrant
} from "@kelpclaw/workflow-spec";
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

interface AzureOpenAiResponsesConfig {
  readonly apiKey: string;
  readonly endpoint: string;
  readonly deployment: string;
  readonly apiVersion: string;
}

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
  readonly memoryWrites?: readonly JsonRecord[] | undefined;
}

interface AgenticPolicyDecision {
  readonly subject: string;
  readonly allowed: boolean;
  readonly reason: string;
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
      const policyDecisions = evaluateAgenticPolicy(node);
      const denied = policyDecisions.filter((decision) => !decision.allowed);
      if (denied.length > 0) {
        return {
          status: "failed",
          output: {},
          error: `Agentic policy denied: ${denied.map((decision) => decision.subject).join(", ")}.`,
          metadata: {
            agentic: true,
            provider: this.provider,
            model: this.model,
            policyDenied: true,
            policyDecisions: policyDecisions.map((decision) => ({ ...decision }))
          }
        };
      }

      const memory = await loadAgentMemory(node, context);
      const research =
        this.provider === "openai"
          ? await this.runOpenAi(node, context, memory.records)
          : await this.runAnthropic(node, context, memory.records);
      const memoryWrites = await persistAgentMemoryWrites(node, context, research);
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
          tools: [...(node.agentic?.tools ?? [])],
          policyDecisions: policyDecisions.map((decision) => ({ ...decision })),
          memoryReadCount: memory.records.length,
          memoryWriteCount: memoryWrites.length,
          memoryRecordIds: [
            ...memory.records.map((record) => record.id),
            ...memoryWrites.map((record) => record.id)
          ],
          runtimeDecisionTraceEvents: [
            {
              kind: "runtime.agent-policy",
              summary: "Agent policy checked.",
              selectedAction: denied.length === 0 ? "allow" : "deny",
              rationale: policyDecisions.map((decision) => decision.reason).join(" ")
            },
            {
              kind: "runtime.memory-read",
              summary: "Agent memory read.",
              selectedAction: `read ${memory.records.length} scoped record(s)`,
              rationale: memory.rationale
            },
            ...(memoryWrites.length > 0
              ? [
                  {
                    kind: "runtime.memory-write",
                    summary: "Agent memory write.",
                    selectedAction: `wrote ${memoryWrites.length} scoped record(s)`,
                    rationale: "Structured memory writes were accepted by policy."
                  }
                ]
              : [])
          ]
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
    context: NodeRunContext,
    memories: readonly WorkflowAgentMemoryRecord[]
  ): Promise<ResearchOutput> {
    const runner = await this.getOpenAiRunner();
    const response = await runner(
      {
        model: this.model,
        instructions: agenticInstructions(node.agentic),
        input: researchPrompt(node, context, memories),
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
    context: NodeRunContext,
    memories: readonly WorkflowAgentMemoryRecord[]
  ): Promise<ResearchOutput> {
    const runner = await this.getAnthropicRunner();
    const abortController = abortControllerForSignal(context.signal);
    let result: unknown;
    const options: Options = {
      maxTurns: node.agentic?.budget.maxIterations ?? 3,
      tools: anthropicToolsForPolicy(node.agentic),
      allowedTools: anthropicToolsForPolicy(node.agentic),
      env: {
        ...process.env,
        ...(this.apiKey ? { ANTHROPIC_API_KEY: this.apiKey } : {}),
        CLAUDE_AGENT_SDK_CLIENT_APP: "kelpclaw-agentic-research/0.1.0"
      },
      outputFormat: {
        type: "json_schema",
        schema: researchOutputSchema
      },
      ...(abortController ? { abortController } : {}),
      ...(this.model ? { model: this.model } : {})
    };
    for await (const message of runner(researchPrompt(node, context, memories), options)) {
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
    const azure = resolveAzureOpenAiResponsesConfig(this.apiKey);
    if (azure) {
      return createAzureOpenAiResponsesRunner(azure);
    }
    if (!this.apiKey) {
      throw new Error(
        "OPENAI_API_KEY or GPT5_MINI_API_KEY/GPT5_PRO_API_KEY is required for OpenAI agentic research."
      );
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
  return provider === "openai"
    ? (process.env.OPENAI_API_KEY ?? process.env.GPT5_MINI_API_KEY ?? process.env.GPT5_PRO_API_KEY)
    : process.env.ANTHROPIC_API_KEY;
}

function modelForProvider(provider: AgenticProvider): string {
  if (provider === "openai") {
    return (
      process.env.KELPCLAW_OPENAI_AGENTIC_MODEL ??
      process.env.KELPCLAW_AGENTIC_MODEL ??
      process.env.KELPCLAW_OPENAI_PLANNER_MODEL ??
      process.env.KELPCLAW_PLANNER_MODEL ??
      process.env.GPT5_MINI_DEPLOYMENT ??
      process.env.GPT5_PRO_DEPLOYMENT ??
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

function resolveAzureOpenAiResponsesConfig(
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
    apiKeyOverride ||
    readEnv("KELPCLAW_AZURE_OPENAI_API_KEY") ||
    readEnv("GPT5_MINI_API_KEY") ||
    readEnv("GPT5_PRO_API_KEY") ||
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

function createAzureOpenAiResponsesRunner(
  config: AzureOpenAiResponsesConfig
): OpenAiAgenticResponsesRunner {
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
      throw new Error(`Azure OpenAI Responses request failed: ${response.status}`);
    }
    return (await response.json()) as OpenAiAgenticResponsesResult;
  };
}

function readEnv(name: string): string | undefined {
  const value = process.env[name]?.trim();
  return value ? value : undefined;
}

function evaluateAgenticPolicy(node: CompiledDagNode): readonly AgenticPolicyDecision[] {
  const policy = node.agentic;
  if (!policy) {
    return [
      {
        subject: "agentic.policy",
        allowed: false,
        reason: "Agentic execution requires an explicit node policy."
      }
    ];
  }

  const grants = normalizedToolGrants(policy);
  const decisions: AgenticPolicyDecision[] = [];
  for (const grant of grants) {
    const secretDenied = grant.secretRefs.filter(
      (secretRef) => !policy.secretRefs.includes(secretRef)
    );
    const hostDenied =
      policy.networkPolicy === "none"
        ? grant.allowedHosts
        : grant.allowedHosts.filter((host) => !hostAllowed(policy.allowedHosts, host));
    const missingOperation =
      (grant.kind === "mcp" || grant.kind === "adapter") &&
      (!grant.operation || !grant.operationVersion);
    const sideEffectDenied = grant.sideEffect === "write";
    const allowed =
      secretDenied.length === 0 &&
      hostDenied.length === 0 &&
      !missingOperation &&
      !sideEffectDenied;
    decisions.push({
      subject: `${grant.kind}:${grant.name}`,
      allowed,
      reason: allowed
        ? `Tool grant '${grant.name}' is within declared policy.`
        : [
            secretDenied.length ? `undeclared secrets ${secretDenied.join(", ")}` : "",
            hostDenied.length ? `undeclared hosts ${hostDenied.join(", ")}` : "",
            missingOperation ? "missing operation metadata" : "",
            sideEffectDenied ? "write side effects require explicit non-agentic approval flow" : ""
          ]
            .filter(Boolean)
            .join("; ")
    });
  }

  if (decisions.length === 0) {
    decisions.push({
      subject: "agentic.no-tools",
      allowed: true,
      reason: "No external tool grants were requested."
    });
  }

  return decisions;
}

function normalizedToolGrants(
  policy: WorkflowAgenticNodePolicy | undefined
): readonly WorkflowAgenticToolGrant[] {
  if (!policy) {
    return [];
  }
  const shorthand = policy.tools.map((tool) => ({
    kind: "builtin" as const,
    name: tool,
    allowedHosts: [],
    secretRefs: [],
    sideEffect: tool === "web-search" ? ("read" as const) : ("none" as const)
  }));
  return [...shorthand, ...(policy.toolGrants ?? [])];
}

function isBuiltinToolAllowed(
  policy: WorkflowAgenticNodePolicy | undefined,
  toolName: string
): boolean {
  return normalizedToolGrants(policy).some(
    (grant) => grant.kind === "builtin" && grant.name === toolName
  );
}

function hostAllowed(allowedHosts: readonly string[], host: string): boolean {
  return allowedHosts.includes("*") || allowedHosts.includes(host);
}

async function loadAgentMemory(
  node: CompiledDagNode,
  context: NodeRunContext
): Promise<{
  readonly records: readonly WorkflowAgentMemoryRecord[];
  readonly rationale: string;
}> {
  const scope = node.agentic?.memoryScope ?? "none";
  if (scope === "none") {
    return {
      records: [],
      rationale: "Node memory scope is none."
    };
  }
  if (!context.agentMemory) {
    return {
      records: [],
      rationale: "No agent memory store was configured."
    };
  }

  const namespace = memoryNamespace(node);
  const records = await context.agentMemory.list({
    workflowId: context.dag.workflowId,
    namespace,
    memoryScope: scope,
    branchId: stringConfig(node, "branchId"),
    runId: context.workspace.runId,
    nodeId: node.id
  });

  return {
    records: records.slice(0, 8),
    rationale: `Loaded ${Math.min(records.length, 8)} ${scope} scoped memory record(s).`
  };
}

async function persistAgentMemoryWrites(
  node: CompiledDagNode,
  context: NodeRunContext,
  research: ResearchOutput
): Promise<readonly WorkflowAgentMemoryRecord[]> {
  const scope = node.agentic?.memoryScope ?? "none";
  if (scope === "none" || !context.agentMemory || !research.memoryWrites?.length) {
    return [];
  }

  const now = new Date().toISOString();
  const namespace = memoryNamespace(node);
  const writes = research.memoryWrites.slice(0, 4).map((write, index) => {
    const content = redactJsonRecord(write, { secretRefs: node.agentic?.secretRefs ?? [] });
    return {
      id: `memory.${context.dag.workflowId}.${node.id}.${Date.now()}.${index}.${randomUUID()}`,
      scope: scope as Exclude<WorkflowAgentMemoryScope, "none">,
      namespace,
      workflowId: context.dag.workflowId,
      ...(stringConfig(node, "branchId") ? { branchId: stringConfig(node, "branchId") } : {}),
      runId: context.workspace.runId,
      nodeId: node.id,
      tags: stringArrayConfig(node, "memoryTags"),
      contentHash: sha256Json(content),
      content,
      shareable: scope === "workspace" || booleanConfig(node, "memoryShareable"),
      createdAt: now,
      updatedAt: now,
      ...(numberConfig(node, "memoryTtlSeconds") !== undefined
        ? {
            expiresAt: new Date(
              Date.now() + (numberConfig(node, "memoryTtlSeconds") ?? 0) * 1000
            ).toISOString()
          }
        : {})
    };
  });

  const saved: WorkflowAgentMemoryRecord[] = [];
  for (const record of writes) {
    saved.push(await context.agentMemory.save(record));
  }
  return saved;
}

function memoryPromptRecord(record: WorkflowAgentMemoryRecord): JsonRecord {
  return {
    id: record.id,
    scope: record.scope,
    tags: [...record.tags],
    content: record.content,
    updatedAt: record.updatedAt
  };
}

function memoryNamespace(node: CompiledDagNode): string {
  return (
    stringConfig(node, "agentMemoryNamespace") ??
    process.env.KELPCLAW_AGENT_MEMORY_NAMESPACE ??
    "default"
  );
}

function stringConfig(node: CompiledDagNode, key: string): string | undefined {
  const value = node.config[key];
  return typeof value === "string" && value.length > 0 ? value : undefined;
}

function booleanConfig(node: CompiledDagNode, key: string): boolean {
  return node.config[key] === true;
}

function numberConfig(node: CompiledDagNode, key: string): number | undefined {
  const value = node.config[key];
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function stringArrayConfig(node: CompiledDagNode, key: string): readonly string[] {
  const value = node.config[key];
  return Array.isArray(value)
    ? value.filter((entry): entry is string => typeof entry === "string")
    : [];
}

function sha256Json(value: JsonRecord): string {
  return `sha256:${createHash("sha256").update(stableJsonStringify(value), "utf8").digest("hex")}`;
}

function openAiToolsForPolicy(
  policy: WorkflowAgenticNodePolicy | undefined
): readonly JsonRecord[] {
  if (!isBuiltinToolAllowed(policy, "web-search")) {
    return [];
  }

  return [{ type: "web_search_preview" }];
}

function anthropicToolsForPolicy(policy: WorkflowAgenticNodePolicy | undefined): string[] {
  return isBuiltinToolAllowed(policy, "web-search") ? ["WebSearch", "WebFetch"] : [];
}

function agenticInstructions(policy: WorkflowAgenticNodePolicy | undefined): string {
  return [
    "You are KelpClaw's bounded research agent.",
    "Use web search when available and cite concrete sources.",
    "Return structured JSON only.",
    "You may return optional memoryWrites as an array of concise JSON objects worth remembering.",
    "Do not resolve secrets, mutate workflow state, send messages, or deploy anything.",
    `Human approval boundaries: ${(policy?.humanApprovalBoundaries ?? []).join("; ") || "none"}.`,
    `Stop conditions: ${(policy?.stopConditions ?? []).join("; ") || "research complete"}.`
  ].join("\n");
}

function researchPrompt(
  node: CompiledDagNode,
  context: NodeRunContext,
  memories: readonly WorkflowAgentMemoryRecord[]
): string {
  return [
    agenticInstructions(node.agentic),
    "",
    `Workflow: ${context.dag.workflowId} r${context.dag.revision}`,
    `Node: ${node.id} (${node.label})`,
    `Node description: ${node.description}`,
    `Node config: ${JSON.stringify(node.config)}`,
    `Input payload: ${JSON.stringify(context.input)}`,
    `Scoped memory: ${JSON.stringify(memories.map(memoryPromptRecord))}`,
    "",
    "Return JSON with summary, sources, limitations, and optional memoryWrites."
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
      ? record.limitations.filter(
          (limitation): limitation is string => typeof limitation === "string"
        )
      : [],
    memoryWrites: Array.isArray(record.memoryWrites)
      ? record.memoryWrites.filter(isJsonRecord).slice(0, 4)
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

function isJsonRecord(value: unknown): value is JsonRecord {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
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
    },
    memoryWrites: {
      type: "array",
      items: {
        type: "object",
        additionalProperties: true
      }
    }
  }
} as const satisfies JsonRecord;
