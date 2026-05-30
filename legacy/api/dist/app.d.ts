import type { FastifyInstance } from "fastify";
import type { CodegenArtifactStore } from "@kelpclaw/codegen";
import type { NodeRunner } from "@kelpclaw/nanoclaw";
import type { WorkflowSpec } from "@kelpclaw/workflow-spec";
import { ApiPolicyEngine } from "./policy-engine.js";
import type { SecretStore } from "./secrets.js";
import type { AgentRunStore } from "./agent-run-store.js";
import type { ApiRole } from "./auth.js";
import type { ApiOtlpExporter } from "./otlp-exporter.js";
import type { WorkflowStore } from "./store.js";
import type { WorkflowPlannerBackend } from "./planner.js";
export interface ApiAppOptions {
    readonly store?: WorkflowStore | undefined;
    readonly planner?: WorkflowPlannerBackend | undefined;
    readonly artifactStore?: CodegenArtifactStore | undefined;
    readonly secretStore?: SecretStore | undefined;
    readonly agentRunStore?: AgentRunStore | undefined;
    readonly policyEngine?: ApiPolicyEngine | undefined;
    readonly otlpExporter?: ApiOtlpExporter | undefined;
    readonly roleTokens?: Readonly<Record<string, readonly ApiRole[]>> | undefined;
    readonly authSigningSecret?: string | null | undefined;
    readonly adminToken?: string | null | undefined;
    readonly rehydratePromotedSkills?: boolean | undefined;
    readonly runner?: NodeRunner | undefined;
}
export declare function createConfiguredWorkflowStore(): WorkflowStore;
export declare function createConfiguredSecretStore(): SecretStore;
export declare function createConfiguredAgentRunStore(): AgentRunStore;
export declare function buildApiApp(options?: ApiAppOptions): FastifyInstance;
export type { WorkflowSpec };
//# sourceMappingURL=app.d.ts.map