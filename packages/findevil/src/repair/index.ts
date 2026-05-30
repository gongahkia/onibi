import type { Claim } from "../types/claim.js";

// TODO: phase 2A generate targeted Protocol SIFT repair prompts.
export function generateRepairPrompt(claim: Claim): string {
  return (
    claim.repairAction?.prompt ??
    `Prove, retract, or downgrade claim ${claim.id}: ${claim.text}`
  );
}
