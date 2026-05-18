import type { JsonRecord, WorkflowDockerSpec, WorkflowNodeType } from "@kelpclaw/workflow-spec";

export type SkillCapability =
  | "brief-ingestion"
  | "workflow-validation"
  | "typescript-codegen"
  | "approval-routing"
  | "adapter-dispatch";

export interface SkillMetadata {
  readonly id: string;
  readonly name: string;
  readonly version: string;
  readonly summary: string;
  readonly deterministic: true;
  readonly nodeTypes: readonly WorkflowNodeType[];
  readonly capabilities: readonly SkillCapability[];
  readonly inputContract: JsonRecord;
  readonly outputContract: JsonRecord;
  readonly metaprompt: string;
  readonly docker: WorkflowDockerSpec;
}

export interface SkillLookupQuery {
  readonly skillId?: string;
  readonly nodeType?: WorkflowNodeType;
  readonly capability?: SkillCapability;
}
