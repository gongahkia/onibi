import { evaluatePolicy, parsePolicyYaml } from "@kelpclaw/policy";
export class ApiPolicyEngine {
    ruleset = { rules: [] };
    evaluateStep(input) {
        return evaluatePolicy({
            tool: input.toolName,
            args: input.args,
            ...(input.sourceAgent ? { sourceAgent: input.sourceAgent } : {}),
            ...(input.classification ? { classification: input.classification } : {})
        }, this.ruleset);
    }
    replaceRuleset(ruleset) {
        this.ruleset = ruleset;
        return this.ruleset;
    }
    replaceYaml(yaml) {
        return this.replaceRuleset(parsePolicyYaml(yaml));
    }
    currentRuleset() {
        return this.ruleset;
    }
}
//# sourceMappingURL=policy-engine.js.map