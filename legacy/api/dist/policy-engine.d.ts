import type { PolicyDecision, PolicyRuleSet } from "@kelpclaw/policy";
import type { AppendAgentStepEventInput } from "./agent-run-store.js";
export declare class ApiPolicyEngine {
    private ruleset;
    evaluateStep(input: AppendAgentStepEventInput): PolicyDecision;
    replaceRuleset(ruleset: PolicyRuleSet): PolicyRuleSet;
    replaceYaml(yaml: string): PolicyRuleSet;
    currentRuleset(): PolicyRuleSet;
}
//# sourceMappingURL=policy-engine.d.ts.map