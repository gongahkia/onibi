import { readFile } from "node:fs/promises";
import type { JsonRecord } from "@kelpclaw/workflow-spec";
import type { PolicyPackMetadata } from "../packs.js";
import type { PolicyContext } from "../types.js";
import type { PolicyRuleSet } from "../types.js";

interface PolicyPack {
  readonly id: "tainted-instruction-block";
  readonly description: string;
  readonly metadata: PolicyPackMetadata;
  readonly ruleset: PolicyRuleSet;
}

export interface TaintedInstructionFirewallDecision {
  readonly decision: "allow" | "block";
  readonly quoted: boolean;
  readonly matchedEntries?: readonly unknown[] | undefined;
  readonly matchedTaint?: readonly unknown[] | undefined;
  readonly matchedPatternId?: string | undefined;
}

export interface TaintedInstructionPolicyContextOptions<TLedgerEntry> {
  readonly taintLedgerPath: string;
  readonly classifyToolCall: (
    args: JsonRecord,
    taintLedger: readonly TLedgerEntry[]
  ) => TaintedInstructionFirewallDecision;
}

export const taintedInstructionBlockReason =
  "Case-derived text cannot become an operational instruction.";

export const taintedInstructionBlockPolicyPack = {
  id: "tainted-instruction-block",
  description: "Blocks case-derived text from becoming operational instructions.",
  metadata: {
    version: "0.1.0",
    region: "global",
    maturity: "experimental",
    controlMappings: ["dfir:taint-containment", "dfir:instruction-firewall"],
    changelog: [
      "Added Phase 1 stub policy pack for Find Evil tainted instruction checks.",
      "Replaced the stub with evaluator-compatible firewall decision rules."
    ]
  },
  ruleset: {
    rules: [
      {
        id: "block-tainted-instruction-text",
        when: 'args.firewall.decision == "block"',
        action: "deny"
      },
      {
        id: "log-tainted-quote",
        when: 'args.firewall.decision == "allow" && args.firewall.quoted == "true" && args.firewall.matchedTaint == "true"',
        action: "log-only"
      }
    ]
  }
} satisfies PolicyPack;

export async function createTaintedInstructionPolicyContext<TLedgerEntry = unknown>(
  tool: string,
  args: JsonRecord,
  options: TaintedInstructionPolicyContextOptions<TLedgerEntry>
): Promise<PolicyContext> {
  const taintLedger = await readTaintLedgerJsonl<TLedgerEntry>(options.taintLedgerPath);
  return createTaintedInstructionPolicyContextFromDecision(
    tool,
    args,
    options.classifyToolCall(args, taintLedger)
  );
}

export function createTaintedInstructionPolicyContextFromDecision(
  tool: string,
  args: JsonRecord,
  decision: TaintedInstructionFirewallDecision
): PolicyContext {
  return {
    tool,
    args: {
      ...args,
      firewall: {
        decision: decision.decision,
        quoted: String(decision.quoted),
        matchedTaint: String(hasMatchedTaint(decision)),
        matchedPatternId: decision.matchedPatternId ?? ""
      }
    }
  };
}

export async function readTaintLedgerJsonl<TLedgerEntry = unknown>(
  path: string
): Promise<readonly TLedgerEntry[]> {
  const content = await readFile(path, "utf8");
  return content
    .split(/\r?\n/u)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => JSON.parse(line) as TLedgerEntry);
}

function hasMatchedTaint(decision: TaintedInstructionFirewallDecision): boolean {
  return Boolean(
    (decision.matchedEntries && decision.matchedEntries.length > 0) ||
    (decision.matchedTaint && decision.matchedTaint.length > 0)
  );
}
