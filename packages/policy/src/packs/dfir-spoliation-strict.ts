import type { PolicyPackMetadata } from "../packs.js";
import type { PolicyRuleSet } from "../types.js";

interface PolicyPack {
  readonly id: "dfir-spoliation-strict";
  readonly description: string;
  readonly metadata: PolicyPackMetadata;
  readonly ruleset: PolicyRuleSet;
}

// TODO: phase 2B replace stub matcher with evidence-root write detection.
export const dfirSpoliationStrictPolicyPack = {
  id: "dfir-spoliation-strict",
  description: "Strict DFIR evidence spoliation guardrails for Protocol SIFT runs.",
  metadata: {
    version: "0.1.0",
    region: "global",
    maturity: "experimental",
    controlMappings: ["dfir:evidence-integrity", "dfir:spoliation-prevention"],
    changelog: ["Added Phase 1 stub policy pack for Find Evil spoliation checks."]
  },
  ruleset: {
    rules: [
      {
        id: "deny-write-into-evidence-root",
        when: 'tool == "__phase_2_spoliation_stub__"',
        action: "deny"
      }
    ]
  }
} satisfies PolicyPack;
