import type { WorkflowRouterEvalCase, WorkflowRouterEvalRun, WorkflowTaskRouteKind } from "@kelpclaw/workflow-spec";
export declare const routerEvalCases: readonly WorkflowRouterEvalCase[];
export declare function runRouterEvalCases(input?: {
    readonly cases?: readonly WorkflowRouterEvalCase[] | undefined;
    readonly now?: string | undefined;
    readonly provider?: string | undefined;
    readonly model?: string | undefined;
}): WorkflowRouterEvalRun;
export declare function routerEvalSummary(run: WorkflowRouterEvalRun): string;
export declare function routeKindFromString(value: string): WorkflowTaskRouteKind | undefined;
//# sourceMappingURL=router-evals.d.ts.map