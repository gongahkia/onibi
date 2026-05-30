import type { ServerResponse } from "node:http";
import type { FastifyInstance } from "fastify";
import type { AgentRunStore } from "./agent-run-store.js";
import type { ApiAuthContext } from "./auth.js";
import type { ApiPolicyEngine } from "./policy-engine.js";
interface AgentRunRouteOptions {
    readonly store: AgentRunStore;
    readonly policyEngine: ApiPolicyEngine;
    readonly auth: ApiAuthContext;
    readonly writeSseEvent: (response: ServerResponse, event: string, data: unknown) => void;
}
export declare function registerAgentRunRoutes(app: FastifyInstance, options: AgentRunRouteOptions): void;
export {};
//# sourceMappingURL=agent-run-routes.d.ts.map