import type { PolicyRuleSet } from "./types.js";

export const policyPackNames = [
  "baseline",
  "finance-sg",
  "pii-strict",
  "no-destructive-shell",
  "github-pr-safe",
  "sg-agentic-ai-baseline",
  "sg-pdpa-strict",
  "sg-financial-ai",
  "asean-genai-baseline"
] as const;

export type PolicyPackName = (typeof policyPackNames)[number];

export interface PolicyPack {
  readonly name: PolicyPackName;
  readonly description: string;
  readonly ruleset: PolicyRuleSet;
}

const packs: readonly PolicyPack[] = [
  {
    name: "baseline",
    description: "General local development defaults for auditable skill runs.",
    ruleset: {
      rules: [
        {
          id: "baseline-deny-destructive-shell",
          when: 'tool == "Bash" && args.command =~ "(rm -rf|sudo rm|mkfs|diskutil erase)"',
          action: "deny"
        },
        {
          id: "baseline-log-shell",
          when: 'tool == "Bash"',
          action: "log-only"
        }
      ]
    }
  },
  {
    name: "finance-sg",
    description: "Singapore finance workflow guardrails for payments, banking, tax, and CPF data.",
    ruleset: {
      rules: [
        {
          id: "finance-sg-review-financial-shell",
          when: 'tool == "Bash" && args.command =~ "(paynow|cpf|iras|mas|bank|payment|transaction|invoice)"',
          action: "require-approval",
          approverRole: "finance-reviewer"
        },
        {
          id: "finance-sg-review-financial-skill",
          when: 'skill.tags includes "finance-sg"',
          action: "require-approval",
          approverRole: "finance-reviewer"
        },
        {
          id: "finance-sg-deny-secret-print",
          when: 'tool == "Bash" && args.command =~ "(printenv|env|cat).*(_KEY|TOKEN|SECRET|PASSWORD)"',
          action: "deny"
        }
      ]
    }
  },
  {
    name: "pii-strict",
    description: "Strict handling rules for personal data and credential-like material.",
    ruleset: {
      rules: [
        {
          id: "pii-strict-review-pii-commands",
          when: 'tool == "Bash" && args.command =~ "(email|phone|passport|nric|ssn|dob|address|customer|user)"',
          action: "require-approval",
          approverRole: "privacy-reviewer"
        },
        {
          id: "pii-strict-review-file-writes",
          when: 'tool == "Write" || tool == "Edit" || tool == "MultiEdit"',
          action: "require-approval",
          approverRole: "privacy-reviewer"
        },
        {
          id: "pii-strict-deny-secret-exfil",
          when: 'tool == "Bash" && args.command =~ "(curl|wget|nc|scp).*(TOKEN|SECRET|PASSWORD|PRIVATE_KEY)"',
          action: "deny"
        }
      ]
    }
  },
  {
    name: "no-destructive-shell",
    description: "Blocks common destructive shell and git cleanup commands.",
    ruleset: {
      rules: [
        {
          id: "no-destructive-shell-deny-rm-rf",
          when: 'tool == "Bash" && args.command =~ "rm -rf"',
          action: "deny"
        },
        {
          id: "no-destructive-shell-deny-git-reset",
          when: 'tool == "Bash" && args.command =~ "git reset --hard|git clean -fd"',
          action: "deny"
        },
        {
          id: "no-destructive-shell-deny-system-erase",
          when: 'tool == "Bash" && args.command =~ "(mkfs|diskutil erase|sudo rm)"',
          action: "deny"
        }
      ]
    }
  },
  {
    name: "github-pr-safe",
    description: "Safe defaults for PR-oriented GitHub automation.",
    ruleset: {
      rules: [
        {
          id: "github-pr-safe-deny-merge",
          when: 'tool == "Bash" && args.command =~ "gh pr (merge|close)|git push --force"',
          action: "deny"
        },
        {
          id: "github-pr-safe-review-mutating-gh",
          when: 'tool == "Bash" && args.command =~ "gh (issue|pr|label|release) (create|edit|delete|reopen|comment)"',
          action: "require-approval",
          approverRole: "reviewer"
        },
        {
          id: "github-pr-safe-log-readonly-gh",
          when: 'tool == "Bash" && args.command =~ "gh (pr|issue|run) (view|list|checks|status)"',
          action: "log-only"
        }
      ]
    }
  },
  {
    name: "sg-agentic-ai-baseline",
    description:
      "Singapore agentic AI defaults for bounded autonomy, approvals, and fail-closed tool governance.",
    ruleset: {
      rules: [
        {
          id: "sg-agentic-deny-destructive-shell",
          when: 'tool == "Bash" && args.command =~ "(rm -rf|sudo rm|mkfs|diskutil erase|git reset --hard|git clean -fd)"',
          action: "deny"
        },
        {
          id: "sg-agentic-deny-unclassified-tool",
          when: 'tool == "Unknown"',
          action: "deny"
        },
        {
          id: "sg-agentic-deny-secret-exfil",
          when: 'tool == "Bash" && args.command =~ "(curl|wget|nc|scp).*(TOKEN|SECRET|PASSWORD|PRIVATE_KEY)"',
          action: "deny"
        },
        {
          id: "sg-agentic-review-file-mutation",
          when: 'tool == "Write" || tool == "Edit" || tool == "MultiEdit"',
          action: "require-approval",
          approverRole: "agentic-ai-reviewer"
        },
        {
          id: "sg-agentic-review-mutating-github",
          when: 'tool == "Bash" && args.command =~ "gh (issue|pr|label|release) (create|edit|delete|reopen|comment|merge|close)|git push"',
          action: "require-approval",
          approverRole: "agentic-ai-reviewer"
        },
        {
          id: "sg-agentic-review-networked-shell",
          when: 'tool == "Bash" && args.command =~ "(curl|wget|http|https)"',
          action: "require-approval",
          approverRole: "agentic-ai-reviewer"
        }
      ]
    }
  },
  {
    name: "sg-pdpa-strict",
    description:
      "Singapore PDPA-oriented personal-data guardrails for agent skills and audit-first runs.",
    ruleset: {
      rules: [
        {
          id: "sg-pdpa-review-personal-data-shell",
          when: 'tool == "Bash" && args.command =~ "(email|phone|passport|nric|ssn|dob|address|customer|user|personal data)"',
          action: "require-approval",
          approverRole: "privacy-reviewer"
        },
        {
          id: "sg-pdpa-review-personal-data-write",
          when: 'tool == "Write" || tool == "Edit" || tool == "MultiEdit"',
          action: "require-approval",
          approverRole: "privacy-reviewer"
        },
        {
          id: "sg-pdpa-deny-secret-exfil",
          when: 'tool == "Bash" && args.command =~ "(curl|wget|nc|scp).*(TOKEN|SECRET|PASSWORD|PRIVATE_KEY)"',
          action: "deny"
        },
        {
          id: "sg-pdpa-deny-env-dump",
          when: 'tool == "Bash" && args.command =~ "(printenv|env|cat).*(_KEY|TOKEN|SECRET|PASSWORD)"',
          action: "deny"
        }
      ]
    }
  },
  {
    name: "sg-financial-ai",
    description:
      "Singapore financial AI guardrails for payments, banking, tax, customer data, and regulated workflows.",
    ruleset: {
      rules: [
        {
          id: "sg-financial-ai-review-financial-shell",
          when: 'tool == "Bash" && args.command =~ "(paynow|cpf|iras|mas|bank|payment|transaction|invoice|customer|account)"',
          action: "require-approval",
          approverRole: "finance-reviewer"
        },
        {
          id: "sg-financial-ai-review-financial-skill",
          when: 'skill.tags includes "finance-sg"',
          action: "require-approval",
          approverRole: "finance-reviewer"
        },
        {
          id: "sg-financial-ai-review-financial-write",
          when: 'tool == "Write" || tool == "Edit" || tool == "MultiEdit"',
          action: "require-approval",
          approverRole: "finance-reviewer"
        },
        {
          id: "sg-financial-ai-deny-secret-exfil",
          when: 'tool == "Bash" && args.command =~ "(curl|wget|nc|scp|printenv|env|cat).*(TOKEN|SECRET|PASSWORD|PRIVATE_KEY)"',
          action: "deny"
        }
      ]
    }
  },
  {
    name: "asean-genai-baseline",
    description:
      "Region-neutral ASEAN/APAC generative and agentic AI defaults for safe local automation.",
    ruleset: {
      rules: [
        {
          id: "asean-genai-deny-destructive-shell",
          when: 'tool == "Bash" && args.command =~ "(rm -rf|sudo rm|mkfs|diskutil erase|git reset --hard|git clean -fd)"',
          action: "deny"
        },
        {
          id: "asean-genai-deny-unclassified-tool",
          when: 'tool == "Unknown"',
          action: "deny"
        },
        {
          id: "asean-genai-review-file-mutation",
          when: 'tool == "Write" || tool == "Edit" || tool == "MultiEdit"',
          action: "require-approval",
          approverRole: "ai-governance-reviewer"
        },
        {
          id: "asean-genai-review-mutating-shell",
          when: 'tool == "Bash" && args.command =~ "(gh (issue|pr|label|release) (create|edit|delete|reopen|comment|merge|close)|git push|curl -X (POST|PUT|PATCH|DELETE))"',
          action: "require-approval",
          approverRole: "ai-governance-reviewer"
        }
      ]
    }
  }
];

export function listPolicyPacks(): readonly PolicyPack[] {
  return packs;
}

export function getPolicyPack(name: string): PolicyPack | undefined {
  return packs.find((pack) => pack.name === name);
}

export function requirePolicyPack(name: string): PolicyPack {
  const pack = getPolicyPack(name);
  if (!pack) {
    throw new Error(
      `Unknown policy pack '${name}'. Available packs: ${policyPackNames.join(", ")}.`
    );
  }
  return pack;
}

export function policyPackToYaml(pack: PolicyPack): string {
  return [
    "rules:",
    ...pack.ruleset.rules.flatMap((rule) => [
      `  - id: ${rule.id}`,
      `    when: ${quoteYamlString(rule.when)}`,
      `    action: ${rule.action}`,
      ...(rule.approverRole ? [`    approverRole: ${rule.approverRole}`] : [])
    ])
  ].join("\n");
}

function quoteYamlString(value: string): string {
  return `"${value.replace(/\\/gu, "\\\\").replace(/"/gu, '\\"')}"`;
}
