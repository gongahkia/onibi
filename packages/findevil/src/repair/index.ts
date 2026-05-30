import type { Claim } from "../types/claim.js";

export interface RepairPrompt {
  readonly prompt: string;
  readonly targetTools: readonly string[];
}

const targetToolsByType: Partial<Record<Claim["type"], readonly string[]>> = {
  program_execution: ["Prefetch", "Amcache", "ShimCache", "Sysmon", "timeline"],
  persistence: ["registry", "scheduled-tasks", "services", "event-log"],
  network_connection: ["pcap", "netflow", "dns", "firewall-log"]
};

export function generateRepairPrompt(claim: Claim): RepairPrompt {
  const targetTools = targetToolsByType[claim.type] ?? ["timeline", "case-artifacts"];
  const prompt =
    claim.repairAction?.prompt ??
    [
      `Prove, retract, or downgrade claim ${claim.id}: ${claim.text}`,
      `Claim type: ${claim.type}`,
      `Current status: ${claim.status}`,
      `Missing evidence: ${claim.missingEvidence.length > 0 ? claim.missingEvidence.join(", ") : "none recorded"}`,
      `Target tools/artifacts: ${targetTools.join(", ")}`,
      "Return only evidence-backed corrections. Do not keep high-severity claims without direct proof."
    ].join("\n");
  return { prompt, targetTools };
}

export { runRepairLoop } from "./loop.js";
export type {
  RepairAgentRequest,
  RepairAgentResult,
  RepairAgentRunner,
  RepairLoopOptions,
  RepairLoopResult,
  RepairTraceRow
} from "./loop.js";
