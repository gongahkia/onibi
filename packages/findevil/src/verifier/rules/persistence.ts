import type { Claim, ClaimStatus } from "../../types/claim.js";

const directPersistenceEvidence = new Set(["registry-run-key", "scheduled-task", "service-create"]);

export function verifyPersistenceClaim(claim: Claim): ClaimStatus {
  const supports = claim.evidenceRefs.map((ref) => ref.supports.toLowerCase());
  if (supports.some((support) => support.includes("contradict"))) {
    return "contradicted";
  }
  if (supports.some((support) => directPersistenceEvidence.has(support))) {
    return "confirmed";
  }
  if (supports.length > 0) {
    return "inferred";
  }
  return "unsupported";
}
