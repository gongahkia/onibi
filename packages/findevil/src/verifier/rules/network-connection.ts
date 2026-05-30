import type { Claim, ClaimStatus } from "../../types/claim.js";

export function verifyNetworkConnectionClaim(claim: Claim): ClaimStatus {
  const supports = claim.evidenceRefs.map((ref) => ref.supports.toLowerCase());
  if (supports.some((support) => support.includes("contradict"))) {
    return "contradicted";
  }
  if (supports.includes("netflow-or-pcap")) {
    return "confirmed";
  }
  if (supports.includes("dns_lookup") || supports.includes("dns-lookup") || supports.length > 0) {
    return "inferred";
  }
  return "unsupported";
}
