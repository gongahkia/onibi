import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { extractClaims } from "../src/extractor/index.js";
import { defaultCommitteeModels, extractClaimsCommittee } from "../src/extractor/committee.js";
import type { Claim, ClaimLedger } from "../src/types/claim.js";

const providerMocks = vi.hoisted(() => ({
  calls: [] as string[],
  responses: new Map<string, unknown>()
}));

vi.mock("../src/extractor/providers/anthropic.js", () => ({
  extractFromAnthropic: vi.fn(async (_prompt: string, model: string) => {
    providerMocks.calls.push(`anthropic:${model}`);
    const key = `anthropic:${model}`;
    if (!providerMocks.responses.has(key)) {
      throw new Error(`missing mocked provider response for ${key}`);
    }
    return providerMocks.responses.get(key);
  })
}));

vi.mock("../src/extractor/providers/openai.js", () => ({
  extractFromOpenAI: vi.fn(async (_prompt: string, model: string) => {
    providerMocks.calls.push(`openai:${model}`);
    const key = `openai:${model}`;
    if (!providerMocks.responses.has(key)) {
      throw new Error(`missing mocked provider response for ${key}`);
    }
    return providerMocks.responses.get(key);
  })
}));

vi.mock("../src/extractor/providers/azure.js", () => ({
  extractFromAzureOpenAI: vi.fn(async (_prompt: string, model: string) => {
    providerMocks.calls.push(`openai-azure:${model}`);
    const key = `openai-azure:${model}`;
    if (!providerMocks.responses.has(key)) {
      throw new Error(`missing mocked provider response for ${key}`);
    }
    return providerMocks.responses.get(key);
  })
}));

vi.mock("../src/extractor/providers/gemini.js", () => ({
  extractFromGemini: vi.fn(async (_prompt: string, model: string) => {
    providerMocks.calls.push(`gemini:${model}`);
    const key = `gemini:${model}`;
    if (!providerMocks.responses.has(key)) {
      throw new Error(`missing mocked provider response for ${key}`);
    }
    return providerMocks.responses.get(key);
  })
}));

const envNames = [
  "ANTHROPIC_API_KEY",
  "OPENAI_API_KEY",
  "AZURE_OPENAI_ENDPOINT",
  "AZURE_OPENAI_API_KEY",
  "AZURE_OPENAI_DEPLOYMENT",
  "GOOGLE_API_KEY",
  "KELP_FINDEVIL_MODELS"
] as const;

type EnvSnapshot = Record<(typeof envNames)[number], string | undefined>;

describe("committee provider dispatch", () => {
  let previousEnv: EnvSnapshot;

  beforeEach(() => {
    previousEnv = snapshotEnv();
    clearProviderEnv();
    providerMocks.calls.length = 0;
    providerMocks.responses.clear();
  });

  afterEach(() => {
    restoreEnv(previousEnv);
    vi.clearAllMocks();
  });

  it("keeps three-provider agreement at confidence 1.0", async () => {
    configureCanonicalProviders();
    setCanonicalResponses(ledgerWith([claim()]), ledgerWith([claim()]), ledgerWith([claim()]));
    const outDir = await mkdtemp(join(tmpdir(), "findevil-provider-unanimous-"));

    const ledger = await extractClaimsCommittee("report", defaultCommitteeModels(), {
      cacheDir: join(outDir, "cache"),
      committeeVotePath: join(outDir, "committee-vote.jsonl"),
      now: () => "2026-05-30T00:00:00.000Z"
    });

    expect(ledger.claims).toHaveLength(1);
    expect(ledger.claims[0]?.confidence).toBe(1);
    expect(ledger.claims[0]?.status).toBe("confirmed");
    expect(providerMocks.calls.sort()).toEqual(
      [
        "anthropic:claude-opus-4-7",
        "gemini:gemini-2.5-pro",
        "openai-azure:gpt-5"
      ].sort()
    );
  });

  it("downgrades two-of-three agreement to confidence 0.67", async () => {
    configureCanonicalProviders();
    setCanonicalResponses(
      ledgerWith([claim()]),
      ledgerWith([claim()]),
      ledgerWith([claim({ severity: "medium" })])
    );
    const outDir = await mkdtemp(join(tmpdir(), "findevil-provider-disagree-"));

    const ledger = await extractClaimsCommittee("report", defaultCommitteeModels(), {
      cacheDir: join(outDir, "cache"),
      committeeVotePath: join(outDir, "committee-vote.jsonl"),
      now: () => "2026-05-30T00:00:00.000Z"
    });

    expect(ledger.claims).toHaveLength(1);
    expect(ledger.claims[0]?.confidence).toBeCloseTo(0.67, 2);
    expect(ledger.claims[0]?.status).toBe("inferred");
  });

  it("drops one-of-three claims", async () => {
    configureCanonicalProviders();
    setCanonicalResponses(ledgerWith([claim()]), ledgerWith([]), ledgerWith([]));
    const outDir = await mkdtemp(join(tmpdir(), "findevil-provider-drop-"));

    const ledger = await extractClaimsCommittee("report", defaultCommitteeModels(), {
      cacheDir: join(outDir, "cache"),
      committeeVotePath: join(outDir, "committee-vote.jsonl"),
      now: () => "2026-05-30T00:00:00.000Z"
    });

    expect(ledger.claims).toHaveLength(0);
  });

  it("throws an explanatory error when no provider is configured", async () => {
    const outDir = await mkdtemp(join(tmpdir(), "findevil-provider-none-"));

    await expect(
      extractClaimsCommittee("report", defaultCommitteeModels(), {
        cacheDir: join(outDir, "cache"),
        committeeVotePath: join(outDir, "committee-vote.jsonl")
      })
    ).rejects.toThrow("No Find Evil extractor providers configured");
  });

  it("falls back to one configured provider", async () => {
    process.env.GOOGLE_API_KEY = "gemini-key";
    providerMocks.responses.set("gemini:gemini-2.5-pro", ledgerWith([claim()]));
    const outDir = await mkdtemp(join(tmpdir(), "findevil-provider-single-"));

    const ledger = await extractClaims("report", {
      cacheDir: join(outDir, "cache"),
      committeeVotePath: join(outDir, "committee-vote.jsonl")
    });

    expect(ledger.claims).toHaveLength(1);
    expect(ledger.claims[0]?.confidence).toBe(1);
    expect(providerMocks.calls).toEqual(["gemini:gemini-2.5-pro"]);
  });
});

function configureCanonicalProviders(): void {
  process.env.ANTHROPIC_API_KEY = "anthropic-key";
  process.env.AZURE_OPENAI_ENDPOINT = "https://example.openai.azure.com";
  process.env.AZURE_OPENAI_API_KEY = "azure-key";
  process.env.AZURE_OPENAI_DEPLOYMENT = "gpt-5";
  process.env.GOOGLE_API_KEY = "gemini-key";
}

function setCanonicalResponses(
  anthropic: ClaimLedger,
  azure: ClaimLedger,
  gemini: ClaimLedger
): void {
  providerMocks.responses.set("anthropic:claude-opus-4-7", anthropic);
  providerMocks.responses.set("openai-azure:gpt-5", azure);
  providerMocks.responses.set("gemini:gemini-2.5-pro", gemini);
}

function ledgerWith(claims: readonly Claim[]): ClaimLedger {
  return {
    id: "claim-ledger-test",
    generatedAt: "2026-05-30T00:00:00.000Z",
    claims
  };
}

function claim(overrides: Partial<Claim> = {}): Claim {
  return {
    id: "claim-0001",
    text: "evil.exe executed from C:\\Users\\Public\\Downloads.",
    type: "program_execution",
    severity: "high",
    status: "confirmed",
    confidence: 1,
    evidenceRefs: [],
    missingEvidence: [],
    ...overrides
  };
}

function snapshotEnv(): EnvSnapshot {
  return Object.fromEntries(envNames.map((name) => [name, process.env[name]])) as EnvSnapshot;
}

function clearProviderEnv(): void {
  for (const name of envNames) {
    delete process.env[name];
  }
}

function restoreEnv(snapshot: EnvSnapshot): void {
  for (const name of envNames) {
    const value = snapshot[name];
    if (value === undefined) {
      delete process.env[name];
    } else {
      process.env[name] = value;
    }
  }
}
