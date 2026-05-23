import type {
  AgentStepClassification,
  AgentStepSourceAgent,
  JsonRecord
} from "@kelpclaw/workflow-spec";

export type PolicyAction = "allow" | "require-approval" | "deny" | "log-only";

export interface PolicyRule {
  readonly id: string;
  readonly when: string;
  readonly action: PolicyAction;
  readonly approverRole?: string | undefined;
}

export interface PolicyRuleSet {
  readonly rules: readonly PolicyRule[];
}

export interface PolicyContext {
  readonly tool: string;
  readonly args: JsonRecord;
  readonly sourceAgent?: AgentStepSourceAgent | undefined;
  readonly classification?: AgentStepClassification | undefined;
  readonly skill?:
    | {
        readonly id?: string | undefined;
        readonly tags?: readonly string[] | undefined;
      }
    | undefined;
}

export interface PolicyDecision {
  readonly action: PolicyAction;
  readonly matchedRuleIds: readonly string[];
  readonly reason: string;
  readonly approverRole?: string | undefined;
}

export class PolicyExpressionError extends Error {
  public constructor(message: string) {
    super(message);
    this.name = "PolicyExpressionError";
  }
}
