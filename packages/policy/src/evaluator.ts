import { evaluatePolicyExpression } from "./expressions.js";
import type { PolicyAction, PolicyContext, PolicyDecision, PolicyRuleSet } from "./types.js";

const actionRank: Record<PolicyAction, number> = {
  allow: 0,
  "log-only": 1,
  "require-approval": 2,
  deny: 3
};

export function evaluatePolicy(context: PolicyContext, ruleset: PolicyRuleSet): PolicyDecision {
  const matches = ruleset.rules.filter((rule) => evaluatePolicyExpression(rule.when, context));
  if (matches.length === 0) {
    return {
      action: "allow",
      matchedRuleIds: [],
      reason: "no policy rules matched"
    };
  }

  const selected = [...matches].sort((left, right) => {
    const rankDelta = actionRank[right.action] - actionRank[left.action];
    return rankDelta || left.id.localeCompare(right.id);
  })[0];
  if (!selected) {
    return {
      action: "allow",
      matchedRuleIds: [],
      reason: "no policy rules matched"
    };
  }

  return {
    action: selected.action,
    matchedRuleIds: matches.map((rule) => rule.id).sort(),
    reason: `matched policy rule '${selected.id}'`,
    ...(selected.approverRole ? { approverRole: selected.approverRole } : {})
  };
}
