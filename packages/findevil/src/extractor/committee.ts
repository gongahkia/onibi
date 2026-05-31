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
import { extractFromAnthropic } from "./providers/anthropic.js";
import { extractFromAzureOpenAI } from "./providers/azure.js";
import { extractFromGemini } from "./providers/gemini.js";
import { extractFromOpenAI } from "./providers/openai.js";
import type { RawClaimsJson } from "./providers/shared.js";
import {
  extractClaimsSingle,
  type ClaimExtractionAttempt,
  type CommitteeClaimExtractionCompletion,
  type ExtractClaimsOptions
} from "./index.js";

export type ProviderName = "anthropic" | "openai" | "openai-azure" | "gemini";
export type CommitteeProvider = ProviderName;
export type Provider = (prompt: string, model: string) => Promise<RawClaimsJson>;

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

const defaultVotePath = ".kelpclaw/findevil/committee-vote.jsonl";
const defaultCacheDir = ".kelpclaw/findevil/extractor-cache";
const defaultQuorumThreshold = 0.75;
const defaultAnthropicModel = "claude-opus-4-7";
const defaultOpenAiModel = "gpt-5";
const defaultGeminiModel = "gemini-2.5-pro";
const defaultCommittee = [
  { provider: "anthropic", model: defaultAnthropicModel, weight: 1 },
  { provider: "openai-azure", model: defaultOpenAiModel, weight: 1 },
  { provider: "gemini", model: defaultGeminiModel, weight: 1 }
] as const satisfies ReadonlyArray<CommitteeModelSpec>;
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
  if (activeModels.length === 0) {
    throw noConfiguredProvidersError();
  }
  const settled = await Promise.all(
    activeModels.map(async (spec): Promise<ModelSuccess | ModelFailure> => {
      try {
        return {
          spec,
          ledger: await extractClaimsSingle(report, {
            apiKey: options.apiKey,
            cacheDir: modelCacheDir(options.cacheDir, spec),
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

export function defaultCommitteeModels(): CommitteeModelSpec[] {
  return defaultCommittee.map((model) => ({ ...model }));
}

export function providerDispatch(provider: ProviderName): Provider {
  switch (provider) {
    case "anthropic":
      return extractFromAnthropic;
    case "openai":
      return extractFromOpenAI;
    case "openai-azure":
      return extractFromAzureOpenAI;
    case "gemini":
      return extractFromGemini;
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
  const providerMatch = /^(anthropic|openai|openai-azure|gemini)[:/](.+)$/iu.exec(modelPart);
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
  if (
    provider === "anthropic" ||
    provider === "openai" ||
    provider === "openai-azure" ||
    provider === "gemini"
  ) {
    return provider;
  }
  throw new Error(`Unsupported committee model provider: ${input}`);
}

function inferProvider(model: string): CommitteeProvider {
  const normalized = model.toLowerCase();
  if (normalized.startsWith("claude")) {
    return "anthropic";
  }
  if (normalized.startsWith("gemini")) {
    return "gemini";
  }
  if (
    normalized.startsWith("gpt") ||
    normalized.startsWith("o1") ||
    normalized.startsWith("o3") ||
    normalized.startsWith("o4") ||
    normalized.startsWith("chatgpt")
  ) {
    return hasAzureOpenAiCredential() && !hasOpenAiCredential() ? "openai-azure" : "openai";
  }
  const configured = configuredProviderNames();
  if (configured.length === 1 && configured[0]) {
    return configured[0];
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
  options: Pick<ExtractClaimsCommitteeOptions, "committeeComplete" | "complete">
): CommitteeModelSpec[] {
  const normalized = models.length > 0 ? models.map(normalizeModel) : defaultCommitteeModels();
  if (options.committeeComplete || options.complete) {
    return normalized;
  }
  const available = normalized.filter((model) => hasProviderCredential(model.provider));
  if (available.length > 0) {
    return available;
  }
  const credentialFallback = fallbackModelFromCredentials();
  return credentialFallback ? [credentialFallback] : [];
}

function normalizeModel(model: CommitteeModelSpec): CommitteeModelSpec {
  return {
    provider: model.provider,
    model: model.model,
    weight: Number.isFinite(model.weight) && model.weight > 0 ? model.weight : 1
  };
}

function fallbackModelFromCredentials(): CommitteeModelSpec | undefined {
  const provider = configuredProviderNames()[0];
  return provider ? defaultModelForProvider(provider) : undefined;
}

function defaultModelForProvider(provider: ProviderName): CommitteeModelSpec {
  return {
    provider,
    model: provider === "gemini" ? defaultGeminiModel : defaultOpenAiLikeModel(provider),
    weight: 1
  };
}

function defaultOpenAiLikeModel(provider: ProviderName): string {
  return provider === "anthropic" ? defaultAnthropicModel : defaultOpenAiModel;
}

function hasProviderCredential(provider: CommitteeProvider): boolean {
  switch (provider) {
    case "anthropic":
      return hasAnthropicCredential();
    case "openai":
      return hasOpenAiCredential();
    case "openai-azure":
      return hasAzureOpenAiCredential();
    case "gemini":
      return hasGeminiCredential();
  }
}

function hasAnthropicCredential(): boolean {
  return stringEnv("ANTHROPIC_API_KEY") !== undefined;
}

function hasOpenAiCredential(): boolean {
  return stringEnv("OPENAI_API_KEY") !== undefined;
}

function hasAzureOpenAiCredential(): boolean {
  return (
    stringEnv("AZURE_OPENAI_ENDPOINT") !== undefined &&
    stringEnv("AZURE_OPENAI_API_KEY") !== undefined &&
    stringEnv("AZURE_OPENAI_DEPLOYMENT") !== undefined
  );
}

function hasGeminiCredential(): boolean {
  return stringEnv("GOOGLE_API_KEY") !== undefined;
}

function configuredProviderNames(): ProviderName[] {
  return (["anthropic", "openai-azure", "gemini", "openai"] as const).filter((provider) =>
    hasProviderCredential(provider)
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
  return (attempt) => providerDispatch(spec.provider)(attempt.userPrompt, spec.model);
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
  spec: CommitteeModelSpec
): string {
  return join(cacheDir ?? defaultCacheDir, safePathPart(spec.provider), safePathPart(spec.model));
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

function noConfiguredProvidersError(): Error {
  return new Error(
    [
      "No Find Evil extractor providers configured.",
      "Set ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY, or all of",
      "AZURE_OPENAI_ENDPOINT + AZURE_OPENAI_API_KEY + AZURE_OPENAI_DEPLOYMENT."
    ].join(" ")
  );
}

function stringEnv(name: string): string | undefined {
  const value = process.env[name]?.trim();
  return value ? value : undefined;
}
