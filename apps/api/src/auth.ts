import type { FastifyReply, FastifyRequest } from "fastify";

export const apiRoles = ["admin", "operator", "reviewer", "auditor"] as const;
export type ApiRole = (typeof apiRoles)[number];

export interface ApiPrincipal {
  readonly subject: string;
  readonly roles: readonly ApiRole[];
  readonly tokenKind: "legacy-admin" | "role-token";
}

export interface ApiAuthOptions {
  readonly adminToken?: string | null | undefined;
  readonly roleTokens?: Readonly<Record<string, readonly ApiRole[]>> | undefined;
}

export interface ApiAuthContext {
  readonly enabled: boolean;
  authenticate(request: FastifyRequest): ApiPrincipal | null;
  requireRole(role: ApiRole): (request: FastifyRequest, reply: FastifyReply) => Promise<void>;
}

const roleSet = new Set<string>(apiRoles);

export function createApiAuthContext(options: ApiAuthOptions): ApiAuthContext {
  const configuredTokens = {
    ...parseRoleTokensEnv(process.env.KELPCLAW_ROLE_TOKENS),
    ...(options.roleTokens ?? {})
  };
  const enabled = Boolean(options.adminToken) || Object.keys(configuredTokens).length > 0;

  return {
    enabled,
    authenticate(request) {
      if (!enabled) {
        return {
          subject: "dev",
          roles: ["admin"],
          tokenKind: "role-token"
        };
      }
      return authenticateBearer(request.headers.authorization, {
        adminToken: options.adminToken,
        roleTokens: configuredTokens
      });
    },
    requireRole(role) {
      return async (request, reply) => {
        if (!enabled) {
          return;
        }
        const principal = authPrincipalForRequest(request);
        if (!principal || !principalHasRole(principal, role)) {
          await reply.code(403).send({
            ok: false,
            error: "FORBIDDEN",
            message: `Role '${role}' is required.`
          });
        }
      };
    }
  };
}

export function attachAuthPrincipal(
  request: FastifyRequest,
  principal: ApiPrincipal | null
): void {
  (request as FastifyRequest & { kelpAuth?: ApiPrincipal | null }).kelpAuth = principal;
}

export function authPrincipalForRequest(request: FastifyRequest): ApiPrincipal | null {
  return (request as FastifyRequest & { kelpAuth?: ApiPrincipal | null }).kelpAuth ?? null;
}

export function createRoleToken(input: {
  readonly roles: readonly ApiRole[];
  readonly subject?: string | undefined;
  readonly expiresAt?: string | undefined;
}): string {
  return `kelp.${Buffer.from(
    JSON.stringify({
      sub: input.subject ?? "api",
      roles: input.roles,
      ...(input.expiresAt ? { exp: input.expiresAt } : {})
    }),
    "utf8"
  ).toString("base64url")}`;
}

export function principalHasRole(principal: ApiPrincipal, role: ApiRole): boolean {
  return principal.roles.includes("admin") || principal.roles.includes(role);
}

function authenticateBearer(
  authorization: string | string[] | undefined,
  options: Required<Pick<ApiAuthOptions, "roleTokens">> & Pick<ApiAuthOptions, "adminToken">
): ApiPrincipal | null {
  const header = Array.isArray(authorization) ? authorization[0] : authorization;
  const token = header?.startsWith("Bearer ") ? header.slice("Bearer ".length) : undefined;
  if (!token) {
    return null;
  }
  if (options.adminToken && token === options.adminToken) {
    return {
      subject: "legacy-admin",
      roles: ["admin"],
      tokenKind: "legacy-admin"
    };
  }
  const configuredRoles = options.roleTokens[token];
  if (configuredRoles) {
    return {
      subject: "configured-token",
      roles: configuredRoles,
      tokenKind: "role-token"
    };
  }
  if (!token.startsWith("kelp.")) {
    return null;
  }
  return parseClaimToken(token);
}

function parseClaimToken(token: string): ApiPrincipal | null {
  try {
    const claim = JSON.parse(Buffer.from(token.slice("kelp.".length), "base64url").toString("utf8"));
    if (!claim || typeof claim !== "object" || !Array.isArray(claim.roles)) {
      return null;
    }
    if (typeof claim.exp === "string" && Date.parse(claim.exp) <= Date.now()) {
      return null;
    }
    const roles = claim.roles.filter((role: unknown): role is ApiRole =>
      typeof role === "string" && roleSet.has(role)
    );
    if (roles.length === 0) {
      return null;
    }
    return {
      subject: typeof claim.sub === "string" ? claim.sub : "api",
      roles,
      tokenKind: "role-token"
    };
  } catch {
    return null;
  }
}

function parseRoleTokensEnv(value: string | undefined): Readonly<Record<string, readonly ApiRole[]>> {
  if (!value) {
    return {};
  }
  const parsed = JSON.parse(value) as unknown;
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("KELPCLAW_ROLE_TOKENS must be a JSON object.");
  }
  return Object.fromEntries(
    Object.entries(parsed).map(([token, roles]) => {
      if (!Array.isArray(roles)) {
        throw new Error("KELPCLAW_ROLE_TOKENS values must be role arrays.");
      }
      const validRoles = roles.filter(
        (role): role is ApiRole => typeof role === "string" && roleSet.has(role)
      );
      if (validRoles.length === 0) {
        throw new Error("KELPCLAW_ROLE_TOKENS entries must include at least one valid role.");
      }
      return [token, validRoles];
    })
  );
}
