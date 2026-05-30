import type { JsonRecord } from "@kelpclaw/workflow-spec";
import type { TaintLedgerEntry } from "../types/taint.js";
import type { FirewallEvent } from "../types/firewall.js";

// TODO: phase 2C classify tool calls against taint ledger and policy packs.
export function classifyToolCall(
  args: JsonRecord,
  taintLedger: readonly TaintLedgerEntry[]
): FirewallEvent | undefined {
  void args;
  void taintLedger;
  return undefined;
}
