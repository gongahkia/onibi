import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import {
  claimLedgerSchema,
  type Claim,
  type ClaimLedger,
  type ClaimStatus,
  type ClaimType,
  type EvidenceRef
} from "../types/claim.js";
import { claimExtractorToolName } from "./prompts.js";
import {
  extractClaimsSingle,
  type ClaimExtractionAttempt,
  type CommitteeClaimExtractionCompletion,
  type ExtractClaimsOptions
} from "./index.js";

export type CommitteeProvider = "anthropic" | "openai";

export interface CommitteeModelSpec {
  readonly provider: CommitteeProvider;
  readonly model: string;
  readonly weight: number;
}

interface ExtractClaimsCommitteeOptions extends Pick<
  ExtractClaimsOptions,
  | "apiKey"
  | "cacheDir"
  | "committeeComplete"
  | "committeeQuorumThreshold"
  | "committeeVotePath"
  | "complete"
  | "maxRetries"
> {
  readonly now?: (() => string) | undefined;
}

interface ModelSuccess {
  readonly spec: CommitteeModelSpec;
  readonly ledger: ClaimLedger;
}

interface ModelFailure {
  readonly spec: CommitteeModelSpec;
  readonly error: unknown;
}

interface ClaimVote {
  readonly claim: Claim;
  readonly spec: CommitteeModelSpec;
  readonly modelIndex: number;
}

interface CommitteeVoteRow {
  readonly claimId: string;
  readonly provider: CommitteeProvider;
  readonly model: string;
  readonly weight: number;
  readonly type: ClaimType;
  readonly severity: Claim["severity"];
  readonly status: ClaimStatus;
  readonly confidence: number;
  readonly text: string;
}

type AnthropicConstructor = new (options?: { readonly apiKey?: string }) => {
  readonly messages: {
    create(input: Record<string, unknown>): Promise<unknown>;
  };
};

type OpenAiConstructor = new (options?: { readonly apiKey?: string }) => {
  readonly responses: {
    create(input: Record<string, unknown>): Promise<unknown>;
  };
};

const defaultVotePath = ".kelpclaw/findevil/committee-vote.jsonl";
const defaultQuorumThreshold = 0.75;
const defaultAnthropicModel = "claude-3-5-sonnet-latest";
const defaultOpenAiModel = "gpt-4.1-mini";
const anthropicSdkPackage = "@anthropic-ai/sdk";
const openAiSdkPackage = "openai";
const severityRank: Record<Claim["severity"], number> = {
  informational: 0,
  low: 1,
  medium: 2,
  high: 3,
  critical: 4
};

export async function extractClaimsCommittee(
  report: string,
  models: ReadonlyArray<CommitteeModelSpec>,
  options: ExtractClaimsCommitteeOptions = {}
): Promise<ClaimLedger> {
  const activeModels = resolveActiveModels(models, options);
  const settled = await Promise.all(
    activeModels.map(async (spec, index): Promise<ModelSuccess | ModelFailure> => {
      try {
        return {
          spec,
          ledger: await extractClaimsSingle(report, {
            apiKey: options.apiKey,
            cacheDir: modelCacheDir(options.cacheDir, spec, index),
            maxRetries: options.maxRetries,
            model: spec.model,
            complete: completionForModel(spec, options)
          })
        };
      } catch (error) {
        return { spec, error };
      }
    })
  );
  const successes = settled.filter((result): result is ModelSuccess => "ledger" in result);
  if (successes.length === 0) {
    const fallback = await trySingleModelFallback(report, activeModels, options);
    if (fallback) {
      await writeJsonl(options.committeeVotePath ?? defaultVotePath, voteRows([fallback]));
      return fallback.ledger;
    }
    const firstFailure = settled.find((result): result is ModelFailure => "error" in result);
    throw new Error("committee claim extraction failed for every model.", {
      cause: firstFailure?.error
    });
  }
  const rows = voteRows(successes);
  await writeJsonl(options.committeeVotePath ?? defaultVotePath, rows);
  return reconcileLedgers(successes, {
    now: options.now ?? (() => new Date().toISOString()),
    quorumThreshold: options.committeeQuorumThreshold ?? defaultQuorumThreshold
  });
}

async function trySingleModelFallback(
  report: string,
  activeModels: readonly CommitteeModelSpec[],
  options: ExtractClaimsCommitteeOptions
): Promise<ModelSuccess | undefined> {
  if (options.committeeComplete || options.complete || activeModels.length < 2) {
    return undefined;
  }
  const spec = fallbackModelFromCredentials() ?? activeModels[0] ?? fallbackModel();
  try {
    return {
      spec,
      ledger: await extractClaimsSingle(report, {
        apiKey: options.apiKey,
        cacheDir: modelCacheDir(options.cacheDir, spec, 0),
        maxRetries: options.maxRetries,
        model: spec.model
      })
    };
  } catch {
    return undefined;
  }
}

export function parseCommitteeModels(input: string): CommitteeModelSpec[] {
  return input
    .split(",")
    .map((token) => parseCommitteeModel(token.trim()))
    .filter((model): model is CommitteeModelSpec => model !== undefined);
}

function parseCommitteeModel(input: string): CommitteeModelSpec | undefined {
  if (input.length === 0) {
    return undefined;
  }
  const [modelPart, weightPart] = splitWeight(input);
  const providerMatch = /^(anthropic|openai)[:/](.+)$/iu.exec(modelPart);
  const provider = providerMatch?.[1]
    ? providerFromString(providerMatch[1])
    : inferProvider(modelPart);
  const model = providerMatch?.[2]?.trim() ?? modelPart.trim();
  if (model.length === 0) {
    return undefined;
  }
  return {
    provider,
    model,
    weight: parseWeight(weightPart)
  };
}

function splitWeight(input: string): readonly [string, string | undefined] {
  const match = /^(.*?)(?:@(\d+(?:\.\d+)?))?$/u.exec(input);
  return [match?.[1]?.trim() ?? input, match?.[2]];
}

function providerFromString(input: string): CommitteeProvider {
  const provider = input.toLowerCase();
  if (provider === "anthropic" || provider === "openai") {
    return provider;
  }
  throw new Error(`Unsupported committee model provider: ${input}`);
}

function inferProvider(model: string): CommitteeProvider {
  const normalized = model.toLowerCase();
  if (normalized.startsWith("claude")) {
    return "anthropic";
  }
  if (
    normalized.startsWith("gpt") ||
    normalized.startsWith("o1") ||
    normalized.startsWith("o3") ||
    normalized.startsWith("o4") ||
    normalized.startsWith("chatgpt")
  ) {
    return "openai";
  }
  if (hasOpenAiCredential() && !hasAnthropicCredential()) {
    return "openai";
  }
  return "anthropic";
}

function parseWeight(input: string | undefined): number {
  if (!input) {
    return 1;
  }
  const value = Number(input);
  return Number.isFinite(value) && value > 0 ? value : 1;
}

function resolveActiveModels(
  models: ReadonlyArray<CommitteeModelSpec>,
  options: Pick<ExtractClaimsCommitteeOptions, "apiKey" | "committeeComplete" | "complete">
): CommitteeModelSpec[] {
  const normalized = models.length > 0 ? models.map(normalizeModel) : [fallbackModel()];
  if (options.committeeComplete || options.complete) {
    return normalized;
  }
  const available = normalized.filter((model) => hasProviderCredential(model.provider, options));
  if (available.length > 0) {
    return available;
  }
  const credentialFallback = fallbackModelFromCredentials();
  return credentialFallback ? [credentialFallback] : [normalized[0] ?? fallbackModel()];
}

function normalizeModel(model: CommitteeModelSpec): CommitteeModelSpec {
  return {
    provider: model.provider,
    model: model.model,
    weight: Number.isFinite(model.weight) && model.weight > 0 ? model.weight : 1
  };
}

function fallbackModelFromCredentials(): CommitteeModelSpec | undefined {
  if (hasAnthropicCredential()) {
    return {
      provider: "anthropic",
      model: process.env.KELP_FINDEVIL_ANTHROPIC_MODEL ?? defaultAnthropicModel,
      weight: 1
    };
  }
  if (hasOpenAiCredential()) {
    return {
      provider: "openai",
      model: process.env.KELP_FINDEVIL_OPENAI_MODEL ?? defaultOpenAiModel,
      weight: 1
    };
  }
  return undefined;
}

function fallbackModel(): CommitteeModelSpec {
  return {
    provider: "anthropic",
    model: process.env.KELP_FINDEVIL_ANTHROPIC_MODEL ?? defaultAnthropicModel,
    weight: 1
  };
}

function hasProviderCredential(
  provider: CommitteeProvider,
  options: Pick<ExtractClaimsCommitteeOptions, "apiKey">
): boolean {
  return options.apiKey !== undefined
    ? true
    : provider === "openai"
      ? hasOpenAiCredential()
      : hasAnthropicCredential();
}

function hasAnthropicCredential(): boolean {
  return stringEnv("ANTHROPIC_API_KEY") !== undefined;
}

function hasOpenAiCredential(): boolean {
  return (
    stringEnv("OPENAI_API_KEY") !== undefined ||
    stringEnv("GPT5_MINI_API_KEY") !== undefined ||
    stringEnv("GPT5_PRO_API_KEY") !== undefined
  );
}

function completionForModel(
  spec: CommitteeModelSpec,
  options: ExtractClaimsCommitteeOptions
): ((attempt: ClaimExtractionAttempt) => Promise<unknown>) | undefined {
  const injected: CommitteeClaimExtractionCompletion | undefined = options.committeeComplete
    ? options.committeeComplete
    : options.complete
      ? async ({ attempt }) => options.complete?.(attempt)
      : undefined;
  if (injected) {
    return (attempt) => injected({ model: spec, attempt });
  }
  return (attempt) => defaultModelCompletion(spec, attempt, options);
}

async function defaultModelCompletion(
  spec: CommitteeModelSpec,
  request: ClaimExtractionAttempt,
  options: Pick<ExtractClaimsCommitteeOptions, "apiKey">
): Promise<unknown> {
  return spec.provider === "openai"
    ? openAiCompletion(spec, request, options)
    : anthropicCompletion(spec, request, options);
}

async function anthropicCompletion(
  spec: CommitteeModelSpec,
  request: ClaimExtractionAttempt,
  options: Pick<ExtractClaimsCommitteeOptions, "apiKey">
): Promise<unknown> {
  const Anthropic = await loadAnthropicConstructor();
  const apiKey = apiKeyForProvider(spec.provider, options);
  const client = new Anthropic(apiKey ? { apiKey } : undefined);
  return client.messages.create({
    model: spec.model,
    max_tokens: 4096,
    temperature: 0,
    system: request.systemPrompt,
    messages: [
      {
        role: "user",
        content: request.userPrompt
      }
    ],
    tools: [
      {
        name: request.toolName,
        description: "Emit the extracted KelpClaw Find Evil claim ledger.",
        input_schema: request.jsonSchema
      }
    ],
    tool_choice: {
      type: "tool",
      name: request.toolName
    }
  });
}

async function openAiCompletion(
  spec: CommitteeModelSpec,
  request: ClaimExtractionAttempt,
  options: Pick<ExtractClaimsCommitteeOptions, "apiKey">
): Promise<unknown> {
  const OpenAI = await loadOpenAiConstructor();
  const apiKey = apiKeyForProvider(spec.provider, options);
  const client = new OpenAI(apiKey ? { apiKey } : undefined);
  const response = await client.responses.create({
    model: spec.model,
    instructions: request.systemPrompt,
    input: request.userPrompt,
    text: {
      format: {
        type: "json_schema",
        name: claimExtractorToolName,
        strict: true,
        schema: request.jsonSchema
      }
    },
    store: false,
    tools: []
  });
  return parsedOpenAiOutput(response) ?? outputText(response) ?? response;
}

async function loadAnthropicConstructor(): Promise<AnthropicConstructor> {
  const module = (await import(anthropicSdkPackage)) as {
    readonly default?: unknown;
    readonly Anthropic?: unknown;
  };
  const constructor = module.default ?? module.Anthropic;
  if (typeof constructor !== "function") {
    throw new Error("@anthropic-ai/sdk did not expose an Anthropic constructor.");
  }
  return constructor as AnthropicConstructor;
}

async function loadOpenAiConstructor(): Promise<OpenAiConstructor> {
  const module = (await import(openAiSdkPackage)) as {
    readonly default?: unknown;
    readonly OpenAI?: unknown;
  };
  const constructor = module.default ?? module.OpenAI;
  if (typeof constructor !== "function") {
    throw new Error("openai did not expose an OpenAI constructor.");
  }
  return constructor as OpenAiConstructor;
}

function apiKeyForProvider(
  provider: CommitteeProvider,
  options: Pick<ExtractClaimsCommitteeOptions, "apiKey">
): string | undefined {
  return options.apiKey ?? providerEnvKey(provider);
}

function providerEnvKey(provider: CommitteeProvider): string | undefined {
  return provider === "openai"
    ? (stringEnv("OPENAI_API_KEY") ??
        stringEnv("GPT5_MINI_API_KEY") ??
        stringEnv("GPT5_PRO_API_KEY"))
    : stringEnv("ANTHROPIC_API_KEY");
}

function parsedOpenAiOutput(response: unknown): unknown {
  const items = outputContentItems(recordValue(response).output);
  for (const item of items) {
    if ("parsed" in item && item.parsed !== undefined) {
      return item.parsed;
    }
  }
  return undefined;
}

function outputText(response: unknown): string | undefined {
  const direct = recordValue(response).output_text;
  if (typeof direct === "string" && direct.trim().length > 0) {
    return direct.trim();
  }
  const text = outputContentItems(recordValue(response).output)
    .map((item) => (typeof item.text === "string" ? item.text : ""))
    .filter((value) => value.length > 0)
    .join("\n")
    .trim();
  return text.length > 0 ? text : undefined;
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
    return Array.isArray(content) ? content.filter(isRecord) : [];
  });
}

function voteRows(successes: readonly ModelSuccess[]): CommitteeVoteRow[] {
  return successes.flatMap((success) =>
    success.ledger.claims.map((claim) => ({
      claimId: claim.id,
      provider: success.spec.provider,
      model: success.spec.model,
      weight: success.spec.weight,
      type: claim.type,
      severity: claim.severity,
      status: claim.status,
      confidence: claim.confidence,
      text: claim.text
    }))
  );
}

function reconcileLedgers(
  successes: readonly ModelSuccess[],
  options: {
    readonly now: () => string;
    readonly quorumThreshold: number;
  }
): ClaimLedger {
  const votesByClaimId = new Map<string, ClaimVote[]>();
  successes.forEach((success, modelIndex) => {
    for (const claim of success.ledger.claims) {
      const votes = votesByClaimId.get(claim.id) ?? [];
      votes.push({ claim, spec: success.spec, modelIndex });
      votesByClaimId.set(claim.id, votes);
    }
  });

  const claims = [...votesByClaimId.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .flatMap(([, votes]) => {
      const claim = reconcileClaim(votes, successes, options.quorumThreshold);
      return claim ? [claim] : [];
    });

  return claimLedgerSchema.parse({
    id: `claim-ledger-committee-${successes[0]?.ledger.id ?? "extractor"}`,
    ...(successes[0]?.ledger.runId ? { runId: successes[0].ledger.runId } : {}),
    generatedAt: options.now(),
    claims
  });
}

function reconcileClaim(
  votes: readonly ClaimVote[],
  successes: readonly ModelSuccess[],
  quorumThreshold: number
): Claim | undefined {
  const totalWeight = successes.reduce((sum, success) => sum + success.spec.weight, 0);
  const selectedType = mostCited(votes.map((vote) => [vote.claim.type, vote.spec.weight] as const));
  const selectedSeverity = mostCitedSeverity(votes);
  const matchingVotes = votes.filter(
    (vote) => vote.claim.type === selectedType && vote.claim.severity === selectedSeverity
  );
  const confidence = boundedConfidence(
    matchingVotes.reduce((sum, vote) => sum + vote.spec.weight * vote.claim.confidence, 0) /
      totalWeight
  );
  if (successes.length > 1 && votes.length === 1 && confidence < quorumThreshold) {
    return undefined;
  }
  const base = strongestVote(matchingVotes.length > 0 ? matchingVotes : votes).claim;
  const hasTypeOrSeverityDisagreement = votes.some(
    (vote) => vote.claim.type !== selectedType || vote.claim.severity !== selectedSeverity
  );
  const hasTripleAgreement = matchingVotes.length >= Math.min(2, successes.length);
  const status =
    successes.length > 1 && hasTypeOrSeverityDisagreement && !hasTripleAgreement
      ? "unverifiable"
      : successes.length > 1 && confidence < quorumThreshold
        ? "inferred"
        : base.status;
  return {
    ...base,
    type: selectedType,
    severity: selectedSeverity,
    status,
    confidence,
    evidenceRefs: uniqueEvidenceRefs(matchingVotes.flatMap((vote) => vote.claim.evidenceRefs)),
    missingEvidence: uniqueStrings(matchingVotes.flatMap((vote) => vote.claim.missingEvidence))
  };
}

function mostCited<T extends string>(entries: ReadonlyArray<readonly [T, number]>): T {
  const first = entries[0];
  if (!first) {
    throw new Error("cannot reconcile an empty committee vote set.");
  }
  const scores = new Map<T, number>();
  for (const [value, weight] of entries) {
    scores.set(value, (scores.get(value) ?? 0) + weight);
  }
  return [...scores.entries()].sort((left, right) => right[1] - left[1])[0]?.[0] ?? first[0];
}

function mostCitedSeverity(votes: readonly ClaimVote[]): Claim["severity"] {
  const first = votes[0];
  if (!first) {
    throw new Error("cannot reconcile an empty committee vote set.");
  }
  const scores = new Map<Claim["severity"], number>();
  for (const vote of votes) {
    scores.set(vote.claim.severity, (scores.get(vote.claim.severity) ?? 0) + vote.spec.weight);
  }
  return (
    [...scores.entries()].sort(
      (left, right) => right[1] - left[1] || severityRank[right[0]] - severityRank[left[0]]
    )[0]?.[0] ?? first.claim.severity
  );
}

function strongestVote(votes: readonly ClaimVote[]): ClaimVote {
  return votes
    .slice()
    .sort(
      (left, right) =>
        right.spec.weight * right.claim.confidence - left.spec.weight * left.claim.confidence ||
        left.modelIndex - right.modelIndex
    )[0] as ClaimVote;
}

function boundedConfidence(value: number): number {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.max(0, Math.min(1, Number(value.toFixed(4))));
}

function uniqueEvidenceRefs(refs: readonly EvidenceRef[]): EvidenceRef[] {
  const seen = new Set<string>();
  return refs.filter((ref) => {
    const key = JSON.stringify(ref);
    if (seen.has(key)) {
      return false;
    }
    seen.add(key);
    return true;
  });
}

function uniqueStrings(values: readonly string[]): string[] {
  return [...new Set(values)];
}

function modelCacheDir(
  cacheDir: string | undefined,
  spec: CommitteeModelSpec,
  index: number
): string | undefined {
  return cacheDir
    ? join(cacheDir, "committee", `${index + 1}-${safePathPart(spec.model)}`)
    : undefined;
}

function safePathPart(value: string): string {
  return value.replace(/[^A-Za-z0-9._-]+/gu, "_");
}

async function writeJsonl(path: string, rows: readonly unknown[]): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(
    path,
    rows.length > 0 ? `${rows.map((row) => JSON.stringify(row)).join("\n")}\n` : "",
    "utf8"
  );
}

function recordValue(input: unknown): Record<string, unknown> {
  return isRecord(input) ? input : {};
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}

function stringEnv(name: string): string | undefined {
  const value = process.env[name]?.trim();
  return value ? value : undefined;
}
