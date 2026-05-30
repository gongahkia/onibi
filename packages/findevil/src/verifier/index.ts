import type { Claim, ClaimStatus } from "../types/claim.js";
import { verifyDefaultClaim } from "./rules/default.js";
import { verifyNetworkConnectionClaim } from "./rules/network-connection.js";
import { verifyPersistenceClaim } from "./rules/persistence.js";
import { verifyProgramExecutionClaim } from "./rules/program-execution.js";

export { verifyDefaultClaim } from "./rules/default.js";
export { verifyNetworkConnectionClaim } from "./rules/network-connection.js";
export { verifyPersistenceClaim } from "./rules/persistence.js";
export { verifyProgramExecutionClaim } from "./rules/program-execution.js";

export function verifyClaim(claim: Claim): ClaimStatus {
  switch (claim.type) {
    case "program_execution":
      return verifyProgramExecutionClaim(claim);
    case "persistence":
      return verifyPersistenceClaim(claim);
    case "network_connection":
      return verifyNetworkConnectionClaim(claim);
    default:
      return verifyDefaultClaim(claim);
  }
}
