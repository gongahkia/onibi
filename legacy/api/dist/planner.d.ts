import type { CodegenArtifactStore, CodeGenerator } from "@kelpclaw/codegen";
import type { WorkflowPlanRequest, WorkflowRepromptNodeRequest, WorkflowRepromptNodeResponse, WorkflowSpec } from "@kelpclaw/workflow-spec";
export interface WorkflowPlannerBackend {
    plan(request: WorkflowPlanRequest): Promise<WorkflowSpec>;
}
export interface RegistryPlannerBackendOptions {
    readonly codeGenerator: CodeGenerator;
    readonly artifactStore: CodegenArtifactStore;
}
export type PlannerBackendMode = "deterministic" | "live";
export type PlannerBackendProvider = "anthropic" | "openai" | "openweight";
export interface LivePlannerBackendOptions extends Partial<RegistryPlannerBackendOptions> {
    readonly apiKey?: string | undefined;
    readonly baseUrl?: string | undefined;
    readonly model?: string | undefined;
    readonly provider?: PlannerBackendProvider | undefined;
}
export declare function createLivePlannerBackend(options?: LivePlannerBackendOptions): WorkflowPlannerBackend;
export declare function createDeterministicPlannerBackend(options?: Partial<RegistryPlannerBackendOptions>): WorkflowPlannerBackend;
export declare function createPlannerBackendFromEnv(options?: Partial<RegistryPlannerBackendOptions>): WorkflowPlannerBackend;
export declare function planWorkflowDraft(request: WorkflowPlanRequest, planner?: WorkflowPlannerBackend): Promise<WorkflowSpec>;
export declare function planMockWorkflowDraft(request: WorkflowPlanRequest): WorkflowSpec;
export declare function repromptWorkflowNode(workflow: WorkflowSpec, request: WorkflowRepromptNodeRequest): WorkflowRepromptNodeResponse["after"];
export declare function repromptWorkflow(workflow: WorkflowSpec, request: WorkflowRepromptNodeRequest): {
    readonly workflow: WorkflowSpec;
    readonly before: WorkflowRepromptNodeResponse["before"];
    readonly after: WorkflowRepromptNodeResponse["after"];
    readonly diff: WorkflowRepromptNodeResponse["diff"];
};
//# sourceMappingURL=planner.d.ts.map