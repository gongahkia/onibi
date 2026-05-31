import type { Claim, ClaimStatus } from "../../types/claim.js";

const malwareIdentificationEvidence = new Set(["yara_hit"]);

export function verifyDefaultClaim(claim: Claim): ClaimStatus {
  if (hasContradiction(claim)) {
    return "contradicted";
  }
  if (claim.type === "malware_identification") {
    return verifyMalwareIdentificationClaim(claim);
  }
  return claim.evidenceRefs.length > 0 ? "inferred" : "unverifiable";
}

function hasContradiction(claim: Claim): boolean {
  return claim.evidenceRefs.some((ref) => ref.supports.toLowerCase().includes("contradict"));
}

function verifyMalwareIdentificationClaim(claim: Claim): ClaimStatus {
  const supports = claim.evidenceRefs.map((ref) => ref.supports.toLowerCase());
  if (supports.some((support) => malwareIdentificationEvidence.has(support))) {
    return "confirmed";
  }
  return supports.length > 0 ? "inferred" : "unverifiable";
}
