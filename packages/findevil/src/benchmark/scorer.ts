import type {
  BenchmarkLedger,
  BenchmarkScore,
  ExpectedFinding,
  GroundTruthMatch
} from "./types.js";

export const hallucinationDefinition =
  "A hallucinated finding is a confirmed claim with no ground-truth support; inferred claims are excluded.";

export function score(
  ledger: BenchmarkLedger,
  expected: readonly ExpectedFinding[]
): BenchmarkScore {
  const matches = matchGroundTruth(ledger, expected);
  const confirmedClaims = ledger.claims.filter((claim) => claim.status === "confirmed").length;
  // hallucination = confirmed assertion with no accepted ground-truth support; inferred claims do not count.
  const hallucinationCount = hallucinatedConfirmedClaims(ledger, expected).length;
  const truePositives = matches.filter((match) => match.truePositive).length;
  const falseNegatives = matches.filter((match) => match.falseNegative).length;
  const falsePositives =
    matches.filter((match) => match.falsePositive).length +
    unmatchedFalsePositiveClaims(ledger, expected).length;
  const hallucinationRate = ratio(hallucinationCount, confirmedClaims);
  const precision = ratio(truePositives, truePositives + falsePositives);
  const recall = ratio(truePositives, truePositives + falseNegatives);
  const f1 = precision + recall === 0 ? 0 : (2 * precision * recall) / (precision + recall);
  return {
    confirmedClaims,
    truePositives,
    falsePositives,
    falseNegatives,
    hallucinationCount,
    hallucinationRate,
    precision,
    recall,
    f1
  };
}

export function hallucinatedConfirmedClaims(
  ledger: BenchmarkLedger,
  expected: readonly ExpectedFinding[]
): BenchmarkLedger["claims"] {
  return ledger.claims
    .filter((claim) => claim.status === "confirmed")
    .filter((claim) => !hasGroundTruthSupport(claim, expected));
}

export function matchGroundTruth(
  ledger: BenchmarkLedger,
  expected: readonly ExpectedFinding[]
): GroundTruthMatch[] {
  const claimsById = new Map(ledger.claims.map((claim) => [claim.id, claim]));
  return expected.map((finding) => {
    const claim = claimsById.get(finding.claimId);
    const claimTechniqueIds = claim?.attackTechniques.map((technique) => technique.id) ?? [];
    const acceptedTechniqueIds = normalizedTechniqueIds(finding.acceptedTechniques);
    const matchedTechniqueIds = claimTechniqueIds.filter((id) => acceptedTechniqueIds.includes(id));
    const truePositive = claim?.status === "confirmed" && matchedTechniqueIds.length > 0;
    const falsePositive = claim?.status === "confirmed" && !truePositive;
    return {
      expectedFindingId: finding.id,
      expectedClaimId: finding.claimId,
      ...(claim ? { claimId: claim.id, claimStatus: claim.status } : {}),
      acceptedTechniqueIds,
      claimTechniqueIds,
      matchedTechniqueIds,
      truePositive,
      falsePositive,
      falseNegative: !truePositive
    };
  });
}

export function unmatchedFalsePositiveClaims(
  ledger: BenchmarkLedger,
  expected: readonly ExpectedFinding[]
): GroundTruthMatch[] {
  return ledger.claims
    .filter((claim) => claim.status === "confirmed")
    .filter(
      (claim) =>
        !expected.some((finding) => {
          const acceptedTechniqueIds = normalizedTechniqueIds(finding.acceptedTechniques);
          return (
            claim.id === finding.claimId &&
            claim.attackTechniques.some((technique) => acceptedTechniqueIds.includes(technique.id))
          );
        })
    )
    .filter((claim) => !expected.some((finding) => finding.claimId === claim.id))
    .map((claim) => ({
      expectedFindingId: "_extra_confirmed_claim",
      expectedClaimId: "_none_",
      claimId: claim.id,
      claimStatus: claim.status,
      acceptedTechniqueIds: [],
      claimTechniqueIds: claim.attackTechniques.map((technique) => technique.id),
      matchedTechniqueIds: [],
      truePositive: false,
      falsePositive: true,
      falseNegative: false
    }));
}

function normalizedTechniqueIds(ids: readonly string[]): string[] {
  return [...new Set(ids.map((id) => id.trim()).filter((id) => /^T\d{4}(\.\d{3})?$/u.test(id)))];
}

function hasGroundTruthSupport(
  claim: BenchmarkLedger["claims"][number],
  expected: readonly ExpectedFinding[]
): boolean {
  const claimTechniqueIds = claim.attackTechniques.map((technique) => technique.id);
  return expected.some((finding) => {
    if (finding.claimId !== claim.id) {
      return false;
    }
    const acceptedTechniqueIds = normalizedTechniqueIds(finding.acceptedTechniques);
    return claimTechniqueIds.some((id) => acceptedTechniqueIds.includes(id));
  });
}

function ratio(numerator: number, denominator: number): number {
  return denominator === 0 ? 0 : numerator / denominator;
}
