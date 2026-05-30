import type { PolicyPackMetadata } from "../packs.js";
import type { PolicyRuleSet } from "../types.js";

interface PolicyPack {
  readonly id: "tainted-instruction-block";
  readonly description: string;
  readonly metadata: PolicyPackMetadata;
  readonly ruleset: PolicyRuleSet;
}

// TODO: phase 2C replace stub matcher with tainted imperative-text detection.
export const taintedInstructionBlockPolicyPack = {
  id: "tainted-instruction-block",
  description: "Blocks case-derived text from becoming operational instructions.",
  metadata: {
    version: "0.1.0",
    region: "global",
    maturity: "experimental",
    controlMappings: ["dfir:taint-containment", "dfir:instruction-firewall"],
    changelog: ["Added Phase 1 stub policy pack for Find Evil tainted instruction checks."]
  },
  ruleset: {
    rules: [
      {
        id: "block-tainted-instruction-text",
        when: 'tool == "__phase_2_tainted_instruction_stub__"',
        action: "deny"
      }
    ]
  }
} satisfies PolicyPack;
