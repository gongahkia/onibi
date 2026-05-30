import type { WorkflowNodeKind, WorkflowPlanRequest, WorkflowTaskRoute, WorkflowTaskRouteKind } from "@kelpclaw/workflow-spec";
export declare const routerClassifierVersion = "kelpclaw.router.scored-v1";
export interface RouteWorkflowTaskOptions {
    readonly correlationId: string;
    readonly provider?: string | undefined;
    readonly model?: string | undefined;
    readonly now?: string | undefined;
}
export declare function routeWorkflowTask(request: WorkflowPlanRequest, options: RouteWorkflowTaskOptions): WorkflowTaskRoute;
export declare function expectedNodeKindsForRoute(route: WorkflowTaskRouteKind): readonly WorkflowNodeKind[];
//# sourceMappingURL=router.d.ts.map