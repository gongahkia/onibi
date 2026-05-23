import type { PolicyAction, PolicyRule, PolicyRuleSet } from "./types.js";
import { validatePolicyExpression } from "./expressions.js";

const policyActions = new Set<PolicyAction>(["allow", "require-approval", "deny", "log-only"]);

interface MutablePolicyRule {
  id?: string | undefined;
  when?: string | undefined;
  action?: PolicyAction | undefined;
  approverRole?: string | undefined;
}

export function parsePolicyYaml(input: string): PolicyRuleSet {
  const rules: PolicyRule[] = [];
  let current: MutablePolicyRule | undefined;

  for (const rawLine of input.split(/\r?\n/u)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#") || line === "rules:") {
      continue;
    }
    if (line.startsWith("- ")) {
      if (current) {
        rules.push(requireRule(current));
      }
      current = {};
      applyPair(current, line.slice(2));
      continue;
    }
    if (!current) {
      throw new Error(`Policy YAML property appears before a rule: ${line}`);
    }
    applyPair(current, line);
  }

  if (current) {
    rules.push(requireRule(current));
  }

  return { rules };
}

function applyPair(rule: MutablePolicyRule, line: string): void {
  const separator = line.indexOf(":");
  if (separator < 0) {
    throw new Error(`Policy YAML line is missing ':': ${line}`);
  }
  const key = line.slice(0, separator).trim();
  const value = stripQuotes(line.slice(separator + 1).trim());
  switch (key) {
    case "id":
      rule.id = value;
      return;
    case "when":
      rule.when = value;
      return;
    case "action":
      if (!policyActions.has(value as PolicyAction)) {
        throw new Error(`Unsupported policy action '${value}'.`);
      }
      rule.action = value as PolicyAction;
      return;
    case "approverRole":
      rule.approverRole = value;
      return;
    default:
      throw new Error(`Unsupported policy key '${key}'.`);
  }
}

function requireRule(input: MutablePolicyRule): PolicyRule {
  if (!input.id || !input.when || !input.action) {
    throw new Error("Policy rules require id, when, and action.");
  }
  validatePolicyExpression(input.when);
  return {
    id: input.id,
    when: input.when,
    action: input.action,
    ...(input.approverRole ? { approverRole: input.approverRole } : {})
  };
}

function stripQuotes(value: string): string {
  if (
    (value.startsWith('"') && value.endsWith('"')) ||
    (value.startsWith("'") && value.endsWith("'"))
  ) {
    return value.slice(1, -1);
  }
  return value;
}
