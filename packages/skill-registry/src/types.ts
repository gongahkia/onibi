import type { JsonSchemaShape, WorkflowNodeKind, WorkflowRuntime } from "@kelpclaw/workflow-spec";

export type SkillCapability =
  | "gmail-receipts-read"
  | "sheets-rows-append"
  | "alert-urgency-classification"
  | "workflow-validation"
  | "approval-routing"
  | "adapter-dispatch";

export interface SkillExampleFixture {
  readonly id: string;
  readonly description: string;
  readonly input: Readonly<Record<string, unknown>>;
  readonly output: Readonly<Record<string, unknown>>;
}

export interface SkillMetadata {
  readonly id: string;
  readonly name: string;
  readonly version: string;
  readonly description: string;
  readonly deterministic: true;
  readonly nodeKinds: readonly WorkflowNodeKind[];
  readonly capabilities: readonly SkillCapability[];
  readonly inputSchema: Readonly<Record<string, JsonSchemaShape>>;
  readonly outputSchema: Readonly<Record<string, JsonSchemaShape>>;
  readonly requiredSecrets: readonly string[];
  readonly adapterDependencies: readonly string[];
  readonly runtimeTemplate: WorkflowRuntime;
  readonly metaprompt: string;
  readonly validationRules: readonly string[];
  readonly examples: readonly SkillExampleFixture[];
}

export interface SkillLookupQuery {
  readonly skillId?: string | undefined;
  readonly nodeKind?: WorkflowNodeKind | undefined;
  readonly capability?: SkillCapability | undefined;
  readonly adapterDependencies?: readonly string[] | undefined;
  readonly prompt?: string | undefined;
}

export interface SkillMatch {
  readonly skill: SkillMetadata;
  readonly score: number;
  readonly reasons: readonly string[];
}

export type SkillSelection =
  | { readonly kind: "skill"; readonly match: SkillMatch }
  | { readonly kind: "codegen"; readonly score: number; readonly reasons: readonly string[] };
