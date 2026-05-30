import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { stableJsonStringify, type JsonValue } from "@kelpclaw/workflow-spec";
import {
  claimLedgerSchema,
  claimTypes,
  type Claim,
  type ClaimLedger
} from "../types/claim.js";
import {
  catalogTechniquesFromIds,
  resolveAttackTechniquesForClaim,
  suggestTechniquesForClaim
} from "../attack/index.js";
import {
  buildClaimExtractorUserPrompt,
  claimExtractorSystemPrompt,
  claimExtractorToolName,
  claimLedgerJsonSchema
} from "./prompts.js";

export interface ClaimExtractionAttempt {
  readonly systemPrompt: string;
  readonly userPrompt: string;
  readonly jsonSchema: typeof claimLedgerJsonSchema;
  readonly toolName: typeof claimExtractorToolName;
  readonly attempt: number;
  readonly validationError?: string | undefined;
}

export type ClaimExtractionCompletion = (attempt: ClaimExtractionAttempt) => Promise<unknown>;

export interface ExtractClaimsOptions {
  readonly cacheDir?: string | undefined;
  readonly model?: string | undefined;
  readonly apiKey?: string | undefined;
  readonly maxRetries?: number | undefined;
  readonly complete?: ClaimExtractionCompletion | undefined;
}

interface AnthropicClient {
  readonly messages: {
    create(input: Record<string, unknown>): Promise<unknown>;
  };
}

type AnthropicConstructor = new (options?: { readonly apiKey?: string }) => AnthropicClient;

const anthropicSdkPackage = "@anthropic-ai/sdk";
const defaultCacheDir = ".kelpclaw/findevil/extractor-cache";
const defaultModel = "claude-3-5-sonnet-latest";

export async function extractClaims(
  report: string | JsonValue,
  options: ExtractClaimsOptions = {}
): Promise<ClaimLedger> {
  const reportText = reportToPromptText(report);
  const cachePath = resolve(options.cacheDir ?? defaultCacheDir, `${sha256Hex(reportText)}.json`);
  const cached = await readCachedLedger(cachePath);
  if (cached) {
    return cached;
  }

  const maxRetries = options.maxRetries ?? 3;
  let validationError: string | undefined;
  let lastError: unknown;
  for (let attempt = 0; attempt <= maxRetries; attempt += 1) {
    const request: ClaimExtractionAttempt = {
      systemPrompt: claimExtractorSystemPrompt,
      userPrompt: buildClaimExtractorUserPrompt(reportText, validationError),
      jsonSchema: claimLedgerJsonSchema,
      toolName: claimExtractorToolName,
      attempt,
      ...(validationError ? { validationError } : {})
    };
    const raw = options.complete
      ? await options.complete(request)
      : await defaultAnthropicCompletion(request, options);
    try {
      const ledger = parseClaimLedgerPayload(decodeCompletionPayload(raw));
      await writeCachedLedger(cachePath, ledger);
      return ledger;
    } catch (error) {
      lastError = error;
      validationError = error instanceof Error ? error.message : String(error);
    }
  }

  throw new Error(`claim extraction failed schema validation: ${String(validationError)}`, {
    cause: lastError
  });
}

async function defaultAnthropicCompletion(
  request: ClaimExtractionAttempt,
  options: Pick<ExtractClaimsOptions, "apiKey" | "model">
): Promise<unknown> {
  const Anthropic = await loadAnthropicConstructor();
  const client = new Anthropic(options.apiKey ? { apiKey: options.apiKey } : undefined);
  return client.messages.create({
    model: options.model ?? defaultModel,
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

function decodeCompletionPayload(raw: unknown): unknown {
  if (isRecord(raw) && Array.isArray(raw.content)) {
    const toolUse = raw.content.find(
      (block) =>
        isRecord(block) &&
        block.type === "tool_use" &&
        block.name === claimExtractorToolName &&
        "input" in block
    );
    if (isRecord(toolUse)) {
      return toolUse.input;
    }
    const text = raw.content
      .filter(
        (block): block is { readonly text: string } =>
          isRecord(block) && typeof block.text === "string"
      )
      .map((block) => block.text)
      .join("\n")
      .trim();
    if (text.length > 0) {
      return JSON.parse(text);
    }
  }
  if (typeof raw === "string") {
    return JSON.parse(raw);
  }
  return raw;
}

async function readCachedLedger(path: string): Promise<ClaimLedger | undefined> {
  try {
    return parseClaimLedgerPayload(JSON.parse(await readFile(path, "utf8")));
  } catch (error) {
    if (isNodeError(error) && error.code === "ENOENT") {
      return undefined;
    }
    throw error;
  }
}

async function writeCachedLedger(path: string, ledger: ClaimLedger): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, `${JSON.stringify(ledger, null, 2)}\n`, "utf8");
}

function parseClaimLedgerPayload(payload: unknown): ClaimLedger {
  return applyAttackTechniques(claimLedgerSchema.parse(mergeAttackTechniquePayload(payload)));
}

function applyAttackTechniques(ledger: ClaimLedger): ClaimLedger {
  return claimLedgerSchema.parse({
    ...ledger,
    claims: ledger.claims.map((claim) => ({
      ...claim,
      attackTechniques: resolveAttackTechniquesForClaim(claim)
    }))
  });
}

function mergeAttackTechniquePayload(payload: unknown): unknown {
  if (!isRecord(payload) || !Array.isArray(payload.claims)) {
    return payload;
  }
  return {
    ...payload,
    claims: payload.claims.map((claim) => mergeClaimAttackTechniquePayload(claim))
  };
}

function mergeClaimAttackTechniquePayload(rawClaim: unknown): unknown {
  if (!isRecord(rawClaim)) {
    return rawClaim;
  }
  const type = typeof rawClaim.type === "string" && isClaimType(rawClaim.type)
    ? rawClaim.type
    : undefined;
  const ids = attackTechniqueIdsFromPayload(rawClaim.attackTechniques);
  const catalogTechniques = ids ? catalogTechniquesFromIds(ids) : undefined;
  if (catalogTechniques) {
    return {
      ...rawClaim,
      attackTechniques: catalogTechniques
    };
  }
  return type
    ? {
        ...rawClaim,
        attackTechniques: suggestTechniquesForClaim({ type })
      }
    : rawClaim;
}

function attackTechniqueIdsFromPayload(input: unknown): string[] | undefined {
  if (!Array.isArray(input) || input.length === 0) {
    return undefined;
  }
  const ids = input.map((entry) =>
    isRecord(entry) && typeof entry.id === "string" ? entry.id : undefined
  );
  return ids.every((id): id is string => typeof id === "string") ? ids : [];
}

function reportToPromptText(report: string | JsonValue): string {
  return typeof report === "string" ? report : stableJsonStringify(report);
}

function sha256Hex(value: string): string {
  return createHash("sha256").update(value, "utf8").digest("hex");
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}

function isClaimType(input: string): input is Claim["type"] {
  return claimTypes.includes(input as Claim["type"]);
}

function isNodeError(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && "code" in error;
}
