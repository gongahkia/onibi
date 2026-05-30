import type { Claim, ClaimStatus } from "../types/claim.js";

// TODO: phase 2A apply per-claim forensic verification rules.
export function verifyClaim(claim: Claim): ClaimStatus {
  return claim.status;
}
