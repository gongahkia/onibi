import type { AttackTechnique, Claim, ClaimLedger, ClaimStatus } from "../types/claim.js";
import type { FirewallEvent } from "../types/firewall.js";
import type { RepairTraceRow } from "../repair/loop.js";

export interface AccuracyReportInput {
  readonly baselineLedger: ClaimLedger;
  readonly repairedLedger: ClaimLedger;
  readonly repairTrace: readonly RepairTraceRow[];
  readonly firewallEvents: readonly FirewallEvent[];
}

export function renderAccuracyReport(input: AccuracyReportInput): string {
  const repairedById = new Map(input.repairedLedger.claims.map((claim) => [claim.id, claim]));
  const rows = input.baselineLedger.claims.map((baseline) => {
    const repaired = repairedById.get(baseline.id);
    return {
      id: baseline.id,
      type: baseline.type,
      severity: baseline.severity,
      baseline: baseline.status,
      repaired: repaired?.status ?? "unverifiable",
      evidence: repaired?.evidenceRefs.length ?? 0,
      outcome: outcomeFor(baseline, repaired)
    };
  });
  const baselineCounts = statusCounts(input.baselineLedger.claims);
  const repairedCounts = statusCounts(input.repairedLedger.claims);
  const repairPrompts = input.repairTrace.filter((row) => row.event === "repair_prompt").length;
  const repairResults = input.repairTrace.filter((row) => row.event === "repair_result").length;
  const successfulRepairs = rows.filter((row) => row.outcome !== "unchanged").length;
  const attackCoverage = attackCoverageRows(input.repairedLedger.claims);

  return [
    "# KelpClaw Find Evil Accuracy Report",
    "",
    "## Summary",
    "",
    `- Baseline claims: ${input.baselineLedger.claims.length}`,
    `- Repaired claims: ${input.repairedLedger.claims.length}`,
    `- Repair prompts: ${repairPrompts}`,
    `- Repair results: ${repairResults}`,
    `- Successful status changes: ${successfulRepairs}`,
    `- Firewall blocks: ${input.firewallEvents.length}`,
    "",
    "## Status Counts",
    "",
    "| Status | Baseline | Repaired |",
    "|---|---:|---:|",
    ...claimStatusOrder.map(
      (status) =>
        `| ${status} | ${baselineCounts.get(status) ?? 0} | ${repairedCounts.get(status) ?? 0} |`
    ),
    "",
    "## Claim Diff",
    "",
    "| Claim | Type | Severity | Baseline | Repaired | Evidence refs | Outcome |",
    "|---|---|---|---|---|---:|---|",
    ...rows.map(
      (row) =>
        `| ${escapeMarkdown(row.id)} | ${row.type} | ${row.severity} | ${row.baseline} | ${row.repaired} | ${row.evidence} | ${row.outcome} |`
    ),
    "",
    "## MITRE ATT&CK Coverage",
    "",
    "| Technique | Name | Tactic | Confirmed claims |",
    "|---|---|---|---:|",
    ...(attackCoverage.length > 0
      ? attackCoverage.map(
          (row) =>
            `| ${row.technique.id} | ${escapeMarkdown(row.technique.name)} | ${escapeMarkdown(row.technique.tactic)} | ${row.confirmedClaims} |`
        )
      : ["| _none_ | _none_ | _none_ | 0 |"]),
    ""
  ].join("\n");
}

const claimStatusOrder: readonly ClaimStatus[] = [
  "confirmed",
  "inferred",
  "unsupported",
  "contradicted",
  "unverifiable"
];

function statusCounts(claims: readonly Claim[]): Map<ClaimStatus, number> {
  const counts = new Map<ClaimStatus, number>();
  for (const claim of claims) {
    counts.set(claim.status, (counts.get(claim.status) ?? 0) + 1);
  }
  return counts;
}

interface AttackCoverageRow {
  readonly technique: AttackTechnique;
  readonly confirmedClaims: number;
}

function attackCoverageRows(claims: readonly Claim[]): AttackCoverageRow[] {
  const rows = new Map<string, { technique: AttackTechnique; confirmedClaimIds: Set<string> }>();
  for (const claim of claims) {
    const seenInClaim = new Set<string>();
    for (const technique of claim.attackTechniques) {
      if (seenInClaim.has(technique.id)) {
        continue;
      }
      seenInClaim.add(technique.id);
      const row = rows.get(technique.id) ?? {
        technique,
        confirmedClaimIds: new Set<string>()
      };
      if (claim.status === "confirmed") {
        row.confirmedClaimIds.add(claim.id);
      }
      rows.set(technique.id, row);
    }
  }
  return [...rows.values()]
    .map((row) => ({
      technique: row.technique,
      confirmedClaims: row.confirmedClaimIds.size
    }))
    .sort((left, right) => left.technique.id.localeCompare(right.technique.id));
}

function outcomeFor(baseline: Claim, repaired: Claim | undefined): string {
  if (!repaired) {
    return "missing-after-repair";
  }
  if (baseline.status === repaired.status) {
    return "unchanged";
  }
  if (repaired.status === "confirmed") {
    return "confirmed";
  }
  if (baseline.status === "unsupported" || baseline.status === "contradicted") {
    return "downgraded-or-retracted";
  }
  return "changed";
}

function escapeMarkdown(value: string): string {
  return value.replace(/\|/gu, "\\|");
}
