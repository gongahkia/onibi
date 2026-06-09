import {
  hallucinationDefinition,
  matchGroundTruth,
  score,
  unmatchedFalsePositiveClaims
} from "./scorer.js";
import type {
  BenchmarkCaseManifest,
  BenchmarkLedger,
  BenchmarkReport,
  ExpectedFinding
} from "./types.js";

export function runBenchmark(
  caseManifest: BenchmarkCaseManifest,
  ledger: BenchmarkLedger
): BenchmarkReport {
  const expectedFindings = expectedFindingsFromCaseManifest(caseManifest);
  const totals = score(ledger, expectedFindings);
  const matches = matchGroundTruth(ledger, expectedFindings);
  const unmatchedFalsePositiveRows = unmatchedFalsePositiveClaims(ledger, expectedFindings);
  return {
    ...totals,
    hallucinationDefinition,
    expectedFindings: expectedFindings.length,
    evaluatedClaims: ledger.claims.length,
    matches,
    unmatchedFalsePositiveClaims: unmatchedFalsePositiveRows
  };
}

export function expectedFindingsFromCaseManifest(
  caseManifest: BenchmarkCaseManifest
): ExpectedFinding[] {
  if (typeof caseManifest === "string") {
    return parseExpectedFindingsYaml(caseManifest);
  }
  const rawFindings: readonly unknown[] = isReadonlyArray(caseManifest)
    ? caseManifest
    : (caseManifest.expectedFindings ?? []);
  return rawFindings.map((finding, index) => normalizeExpectedFinding(finding, index));
}

function normalizeExpectedFinding(input: unknown, index: number): ExpectedFinding {
  if (!isRecord(input)) {
    throw new Error(`expectedFindings[${index}] must be an object.`);
  }
  const id = stringField(input.id);
  const claimId = stringField(input.claimId);
  const acceptedTechniques = stringArrayField(input.acceptedTechniques);
  if (!id) {
    throw new Error(`expectedFindings[${index}].id is required.`);
  }
  if (!claimId) {
    throw new Error(`expectedFindings[${index}].claimId is required.`);
  }
  if (acceptedTechniques.length === 0) {
    throw new Error(`expectedFindings[${index}].acceptedTechniques must contain ATT&CK IDs.`);
  }
  return {
    id,
    claimId,
    ...(stringField(input.type) ? { type: stringField(input.type) } : {}),
    ...(stringField(input.description) ? { description: stringField(input.description) } : {}),
    acceptedTechniques
  };
}

function parseExpectedFindingsYaml(input: string): ExpectedFinding[] {
  const findings: Record<string, unknown>[] = [];
  let inExpectedFindings = false;
  let current: Record<string, unknown> | undefined;
  let currentList: "acceptedTechniques" | undefined;

  for (const line of input.split(/\r?\n/u)) {
    if (/^expectedFindings:\s*$/u.test(line)) {
      inExpectedFindings = true;
      continue;
    }
    if (!inExpectedFindings) {
      continue;
    }
    if (/^\S/u.test(line)) {
      break;
    }

    const itemMatch = /^  -\s+([A-Za-z][A-Za-z0-9_-]*):\s*(.*?)\s*$/u.exec(line);
    if (itemMatch?.[1]) {
      current = {};
      findings.push(current);
      currentList = undefined;
      setYamlField(current, itemMatch[1], itemMatch[2] ?? "");
      continue;
    }
    if (!current) {
      continue;
    }

    const fieldMatch = /^    ([A-Za-z][A-Za-z0-9_-]*):\s*(.*?)\s*$/u.exec(line);
    if (fieldMatch?.[1]) {
      const key = fieldMatch[1];
      const value = fieldMatch[2] ?? "";
      currentList =
        key === "acceptedTechniques" && value.length === 0 ? "acceptedTechniques" : undefined;
      setYamlField(current, key, value);
      continue;
    }

    const listMatch = /^      -\s*(.*?)\s*$/u.exec(line);
    if (listMatch?.[1] && currentList === "acceptedTechniques") {
      const list = Array.isArray(current.acceptedTechniques) ? current.acceptedTechniques : [];
      current.acceptedTechniques = [...list, scalarValue(listMatch[1])];
    }
  }

  return findings.map((finding, index) => normalizeExpectedFinding(finding, index));
}

function setYamlField(target: Record<string, unknown>, key: string, rawValue: string): void {
  if (
    key !== "id" &&
    key !== "claimId" &&
    key !== "type" &&
    key !== "description" &&
    key !== "acceptedTechniques"
  ) {
    return;
  }
  if (key === "acceptedTechniques") {
    target[key] = parseInlineList(rawValue);
    return;
  }
  if (rawValue.length > 0 && rawValue !== ">") {
    target[key] = scalarValue(rawValue);
  }
}

function parseInlineList(input: string): string[] {
  const match = /^\[(.*)\]$/u.exec(input.trim());
  if (!match?.[1]) {
    return [];
  }
  return match[1]
    .split(",")
    .map((value) => scalarValue(value))
    .filter(Boolean);
}

function scalarValue(input: string): string {
  return input.trim().replace(/^["']|["']$/gu, "");
}

function stringField(input: unknown): string | undefined {
  return typeof input === "string" && input.trim().length > 0 ? input.trim() : undefined;
}

function stringArrayField(input: unknown): string[] {
  if (!Array.isArray(input)) {
    return [];
  }
  return input.flatMap((value) =>
    typeof value === "string" && value.trim() ? [value.trim()] : []
  );
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}

function isReadonlyArray(input: unknown): input is readonly unknown[] {
  return Array.isArray(input);
}
