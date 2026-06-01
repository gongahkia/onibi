import type { ClaimLedger } from "../types/claim.js";
import type { FirewallEvent } from "../types/firewall.js";
import type { SpoliationCheck } from "../types/spoliation.js";
import type { TaintLedgerEntry } from "../types/taint.js";
import type { RepairTraceRow } from "../repair/loop.js";

export type SentinelMode = "sentinel" | "verify" | "firewall";
export type SentinelStatus = "succeeded" | "policy_denied";
export type TimestampMode = "live" | "skip";

export interface SentinelOptions {
  readonly casePath: string;
  readonly evidenceRoot: string;
  readonly outDir: string;
  readonly maxIterations: number;
  readonly siftCommand?: string | undefined;
  readonly tracePath?: string | undefined;
  readonly mode?: SentinelMode | undefined;
  readonly skipFirewall?: boolean | undefined;
  readonly skipSpoliation?: boolean | undefined;
  readonly skipClaimExtraction?: boolean | undefined;
  readonly timestampMode?: TimestampMode | undefined;
}

export interface SentinelOutputPaths {
  readonly agentExecution: string;
  readonly claimLedger: string;
  readonly repairTrace: string;
  readonly taintLedger: string;
  readonly firewallEvents: string;
  readonly spoliationCheck: string;
  readonly evidenceManifest: string;
  readonly accuracyReport: string;
  readonly auditBundle: string;
}

export interface SentinelResult {
  readonly ok: boolean;
  readonly status: SentinelStatus;
  readonly runId: string;
  readonly mode: SentinelMode;
  readonly outDir: string;
  readonly outputs: SentinelOutputPaths;
  readonly baselineLedger?: ClaimLedger | undefined;
  readonly claimLedger?: ClaimLedger | undefined;
  readonly repairTrace: readonly RepairTraceRow[];
  readonly firewallEvents: readonly FirewallEvent[];
  readonly taintLedger: readonly TaintLedgerEntry[];
  readonly spoliationCheck?: SpoliationCheck | undefined;
  readonly policyDenials: number;
  readonly uncorrectedPolicyDenials: number;
}
