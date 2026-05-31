import type { Claim, ClaimStatus } from "../../types/claim.js";

const directExecutionEvidence = new Set([
  "prefetch_entry",
  "amcache_execution_record",
  "shimcache_indicator",
  "srum_network_activity",
  "sysmon_process_create",
  "security_4688_process_create",
  "volatility-pslist",
  "volatility-malfind"
]);
const corroboratingOnlyExecutionEvidence = new Set(["mft-file-create"]);

export function verifyProgramExecutionClaim(claim: Claim): ClaimStatus {
  const supports = claim.evidenceRefs.map((ref) => ref.supports.toLowerCase());
  if (supports.some((support) => support.includes("contradict"))) {
    return "contradicted";
  }
  if (supports.some((support) => directExecutionEvidence.has(support))) {
    return "confirmed";
  }
  if (supports.some((support) => corroboratingOnlyExecutionEvidence.has(support))) {
    return "unsupported";
  }
  return "unsupported";
}
