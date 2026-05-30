import type { FastifyReply, FastifyRequest } from "fastify";
export declare const apiRoles: readonly ["admin", "operator", "reviewer", "auditor"];
export type ApiRole = (typeof apiRoles)[number];
export interface ApiPrincipal {
    readonly subject: string;
    readonly roles: readonly ApiRole[];
    readonly tokenKind: "legacy-admin" | "server-role-token" | "signed-role-token";
}
export interface ApiAuthOptions {
    readonly adminToken?: string | null | undefined;
    readonly roleTokens?: Readonly<Record<string, readonly ApiRole[]>> | undefined;
    readonly signingSecret?: string | null | undefined;
}
export interface ApiAuthContext {
    readonly enabled: boolean;
    authenticate(request: FastifyRequest): ApiPrincipal | null;
    requireRole(role: ApiRole): (request: FastifyRequest, reply: FastifyReply) => Promise<void>;
}
export declare function createApiAuthContext(options: ApiAuthOptions): ApiAuthContext;
export declare function attachAuthPrincipal(request: FastifyRequest, principal: ApiPrincipal | null): void;
export declare function authPrincipalForRequest(request: FastifyRequest): ApiPrincipal | null;
export declare function createRoleToken(input: {
    readonly roles: readonly ApiRole[];
    readonly subject?: string | undefined;
    readonly expiresAt?: string | undefined;
    readonly signingSecret?: string | undefined;
}): string;
export declare function inspectApiToken(token: string, options: ApiAuthOptions): ApiPrincipal | null;
export declare function isApiRole(value: unknown): value is ApiRole;
export declare function principalHasRole(principal: ApiPrincipal, role: ApiRole): boolean;
//# sourceMappingURL=auth.d.ts.map