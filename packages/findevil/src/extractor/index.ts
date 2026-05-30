import { emptyClaimLedger, type ClaimLedger } from "../types/claim.js";

// TODO: phase 2A extract atomic claims from Protocol SIFT reports.
export function extractClaims(report: string): ClaimLedger {
  return {
    ...emptyClaimLedger,
    id: report.length > 0 ? "claim-ledger-stub" : emptyClaimLedger.id
  };
}
