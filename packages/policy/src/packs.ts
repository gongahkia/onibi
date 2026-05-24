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
  "asean-genai-baseline",
  "web-search-safe",
  "sg-web-research",
  "browser-automation-strict"
] as const;

export type PolicyPackName = (typeof policyPackNames)[number];

export interface PolicyPack {
  readonly name: PolicyPackName;
  readonly description: string;
  readonly metadata: PolicyPackMetadata;
  readonly ruleset: PolicyRuleSet;
}

export interface PolicyPackMetadata {
  readonly version: string;
  readonly region: "global" | "sg" | "asean";
  readonly maturity: "baseline" | "strict" | "regulated" | "experimental";
  readonly controlMappings: readonly string[];
  readonly changelog: readonly string[];
}

const packs: readonly PolicyPack[] = [
  {
    name: "baseline",
    description: "General local development defaults for auditable skill runs.",
    metadata: {
      version: "1.0.0",
      region: "global",
      maturity: "baseline",
      controlMappings: ["tool-risk", "destructive-action-prevention", "audit-logging"],
      changelog: ["Initial local-development baseline policy pack."]
    },
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
    metadata: {
      version: "1.0.0",
      region: "sg",
      maturity: "regulated",
      controlMappings: ["financial-review", "secret-exfiltration-prevention", "human-approval"],
      changelog: ["Initial Singapore finance guardrails for agent skills."]
    },
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
    metadata: {
      version: "1.0.0",
      region: "global",
      maturity: "strict",
      controlMappings: ["privacy-review", "secret-exfiltration-prevention", "data-minimisation"],
      changelog: ["Initial strict PII and credential handling policy pack."]
    },
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
    metadata: {
      version: "1.0.0",
      region: "global",
      maturity: "strict",
      controlMappings: ["destructive-action-prevention", "reversibility", "change-control"],
      changelog: ["Initial destructive shell prevention policy pack."]
    },
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
    metadata: {
      version: "1.0.0",
      region: "global",
      maturity: "baseline",
      controlMappings: ["source-control-change-control", "human-approval", "audit-logging"],
      changelog: ["Initial GitHub pull-request safety policy pack."]
    },
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
    metadata: {
      version: "1.1.0",
      region: "sg",
      maturity: "regulated",
      controlMappings: [
        "imda-agentic-ai:human-accountability",
        "imda-agentic-ai:bounded-autonomy",
        "imda-agentic-ai:traceability",
        "imda-agentic-ai:distributed-safeguards",
        "imda-agentic-ai:incident-readiness"
      ],
      changelog: [
        "Mapped policy metadata to Singapore IMDA Agentic AI governance control areas.",
        "Added explicit delegation, tool access, network, and irreversible-action review rules."
      ]
    },
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
        },
        {
          id: "sg-agentic-review-agent-delegation",
          when: 'tool == "Task" || tool == "TodoWrite"',
          action: "require-approval",
          approverRole: "agentic-ai-reviewer"
        },
        {
          id: "sg-agentic-review-irreversible-actions",
          when: 'tool == "Bash" && args.command =~ "(deploy|release|publish|terraform apply|kubectl apply|docker push)"',
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
    metadata: {
      version: "1.0.0",
      region: "sg",
      maturity: "strict",
      controlMappings: ["sg-pdpa:personal-data-review", "data-minimisation", "privacy-approval"],
      changelog: ["Initial Singapore PDPA-oriented strict policy pack."]
    },
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
    metadata: {
      version: "1.0.0",
      region: "sg",
      maturity: "regulated",
      controlMappings: [
        "financial-review",
        "customer-data-protection",
        "regulated-workflow-approval"
      ],
      changelog: ["Initial Singapore financial AI policy pack."]
    },
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
    metadata: {
      version: "1.0.0",
      region: "asean",
      maturity: "baseline",
      controlMappings: ["bounded-autonomy", "human-approval", "destructive-action-prevention"],
      changelog: ["Initial ASEAN/APAC baseline policy pack."]
    },
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
  },
  {
    name: "web-search-safe",
    description:
      "Safe defaults for governed web search, answer, and fetch operations with approval gates for stored content and browser automation.",
    metadata: {
      version: "1.0.0",
      region: "global",
      maturity: "baseline",
      controlMappings: ["web-evidence-review", "content-retention", "browser-automation-approval"],
      changelog: ["Initial governed web intelligence policy pack."]
    },
    ruleset: {
      rules: [
        {
          id: "web-search-safe-review-full-content-storage",
          when: 'args.storeFullContent == "true"',
          action: "require-approval",
          approverRole: "web-research-reviewer"
        },
        {
          id: "web-search-safe-review-browser-session",
          when: 'tool startsWith "tinyfish.browser"',
          action: "require-approval",
          approverRole: "web-automation-reviewer"
        },
        {
          id: "web-search-safe-review-web-agent",
          when: 'tool == "tinyfish.agent.run"',
          action: "require-approval",
          approverRole: "web-automation-reviewer"
        },
        {
          id: "web-search-safe-deny-sensitive-browser-goals",
          when: 'tool startsWith "tinyfish.browser" && args.goal =~ "(login|password|checkout|payment|bank|credential|delete account)"',
          action: "deny"
        },
        {
          id: "web-search-safe-deny-secret-harvest",
          when: 'args.query =~ "(TOKEN|SECRET|PASSWORD|PRIVATE_KEY|API_KEY)" || args.goal =~ "(TOKEN|SECRET|PASSWORD|PRIVATE_KEY|API_KEY)"',
          action: "deny"
        }
      ]
    }
  },
  {
    name: "sg-web-research",
    description:
      "Singapore-oriented web research guardrails for PDPA, financial services, government, and regulated-domain evidence collection.",
    metadata: {
      version: "1.0.0",
      region: "sg",
      maturity: "regulated",
      controlMappings: [
        "sg-pdpa:query-review",
        "financial-research-review",
        "web-evidence-retention"
      ],
      changelog: ["Initial Singapore governed web research policy pack."]
    },
    ruleset: {
      rules: [
        {
          id: "sg-web-research-review-personal-data-query",
          when: 'args.query =~ "(nric|passport|phone|email|customer|personal data|pdpa|cpf|iras)" || args.question =~ "(nric|passport|phone|email|customer|personal data|pdpa|cpf|iras)"',
          action: "require-approval",
          approverRole: "privacy-reviewer"
        },
        {
          id: "sg-web-research-review-financial-regulatory-query",
          when: 'args.query =~ "(mas|bank|payment|paynow|transaction|investment|insurance|financial advice)" || args.question =~ "(mas|bank|payment|paynow|transaction|investment|insurance|financial advice)"',
          action: "require-approval",
          approverRole: "finance-reviewer"
        },
        {
          id: "sg-web-research-review-full-content-storage",
          when: 'args.storeFullContent == "true"',
          action: "require-approval",
          approverRole: "privacy-reviewer"
        },
        {
          id: "sg-web-research-review-browser-or-agent",
          when: 'tool startsWith "tinyfish.browser" || tool == "tinyfish.agent.run"',
          action: "require-approval",
          approverRole: "ai-governance-reviewer"
        },
        {
          id: "sg-web-research-deny-secret-harvest",
          when: 'args.query =~ "(TOKEN|SECRET|PASSWORD|PRIVATE_KEY|API_KEY)" || args.goal =~ "(TOKEN|SECRET|PASSWORD|PRIVATE_KEY|API_KEY)"',
          action: "deny"
        }
      ]
    }
  },
  {
    name: "browser-automation-strict",
    description:
      "Strict web automation rules that force human approval for browser and web-agent actions and block account, payment, and credential flows.",
    metadata: {
      version: "1.0.0",
      region: "global",
      maturity: "strict",
      controlMappings: [
        "browser-automation-approval",
        "credential-flow-prevention",
        "account-safety"
      ],
      changelog: ["Initial strict browser automation policy pack."]
    },
    ruleset: {
      rules: [
        {
          id: "browser-automation-strict-deny-login-payment",
          when: 'tool startsWith "tinyfish.browser" && args.goal =~ "(login|password|checkout|payment|bank|credential|account settings|delete account)"',
          action: "deny"
        },
        {
          id: "browser-automation-strict-deny-agent-login-payment",
          when: 'tool == "tinyfish.agent.run" && args.goal =~ "(login|password|checkout|payment|bank|credential|account settings|delete account)"',
          action: "deny"
        },
        {
          id: "browser-automation-strict-review-browser",
          when: 'tool startsWith "tinyfish.browser"',
          action: "require-approval",
          approverRole: "web-automation-reviewer"
        },
        {
          id: "browser-automation-strict-review-agent",
          when: 'tool == "tinyfish.agent.run"',
          action: "require-approval",
          approverRole: "web-automation-reviewer"
        },
        {
          id: "browser-automation-strict-review-content-storage",
          when: 'args.storeFullContent == "true"',
          action: "require-approval",
          approverRole: "web-automation-reviewer"
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
