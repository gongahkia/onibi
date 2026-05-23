import { evaluatePolicy, parsePolicyYaml } from "@kelpclaw/policy";
import type { PolicyDecision, PolicyRuleSet } from "@kelpclaw/policy";
import type { AppendAgentStepEventInput } from "./agent-run-store.js";

export class ApiPolicyEngine {
  private ruleset: PolicyRuleSet = { rules: [] };

  public evaluateStep(input: AppendAgentStepEventInput): PolicyDecision {
    return evaluatePolicy(
      {
        tool: input.toolName,
        args: input.args,
        ...(input.sourceAgent ? { sourceAgent: input.sourceAgent } : {}),
        ...(input.classification ? { classification: input.classification } : {})
      },
      this.ruleset
    );
  }

  public replaceRuleset(ruleset: PolicyRuleSet): PolicyRuleSet {
    this.ruleset = ruleset;
    return this.ruleset;
  }

  public replaceYaml(yaml: string): PolicyRuleSet {
    return this.replaceRuleset(parsePolicyYaml(yaml));
  }

  public currentRuleset(): PolicyRuleSet {
    return this.ruleset;
  }
}
