import type { Claim, ClaimStatus } from "../../types/claim.js";

export function verifyDefaultClaim(claim: Claim): ClaimStatus {
  if (hasContradiction(claim)) {
    return "contradicted";
  }
  return claim.evidenceRefs.length > 0 ? "inferred" : "unverifiable";
}

function hasContradiction(claim: Claim): boolean {
  return claim.evidenceRefs.some((ref) => ref.supports.toLowerCase().includes("contradict"));
}
