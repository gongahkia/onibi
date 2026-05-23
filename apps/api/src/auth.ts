import { createHmac, timingSafeEqual } from "node:crypto";
import type { FastifyReply, FastifyRequest } from "fastify";

export const apiRoles = ["admin", "operator", "reviewer", "auditor"] as const;
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

const roleSet = new Set<string>(apiRoles);

export function createApiAuthContext(options: ApiAuthOptions): ApiAuthContext {
  const configuredTokens = {
    ...parseRoleTokensEnv(process.env.KELPCLAW_ROLE_TOKENS),
    ...(options.roleTokens ?? {})
  };
  const signingSecret =
    options.signingSecret === undefined
      ? process.env.KELPCLAW_AUTH_SIGNING_SECRET
      : options.signingSecret;
  const enabled =
    Boolean(options.adminToken) ||
    Object.keys(configuredTokens).length > 0 ||
    Boolean(signingSecret);

  return {
    enabled,
    authenticate(request) {
      if (!enabled) {
        return {
          subject: "dev",
          roles: ["admin"],
          tokenKind: "signed-role-token"
        };
      }
      return authenticateBearer(request.headers.authorization, {
        adminToken: options.adminToken,
        roleTokens: configuredTokens,
        signingSecret
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

export function attachAuthPrincipal(request: FastifyRequest, principal: ApiPrincipal | null): void {
  (request as FastifyRequest & { kelpAuth?: ApiPrincipal | null }).kelpAuth = principal;
}

export function authPrincipalForRequest(request: FastifyRequest): ApiPrincipal | null {
  return (request as FastifyRequest & { kelpAuth?: ApiPrincipal | null }).kelpAuth ?? null;
}

export function createRoleToken(input: {
  readonly roles: readonly ApiRole[];
  readonly subject?: string | undefined;
  readonly expiresAt?: string | undefined;
  readonly signingSecret?: string | undefined;
}): string {
  const signingSecret = input.signingSecret ?? process.env.KELPCLAW_AUTH_SIGNING_SECRET;
  if (!signingSecret) {
    throw new Error("createRoleToken requires signingSecret or KELPCLAW_AUTH_SIGNING_SECRET.");
  }
  const payload = Buffer.from(
    JSON.stringify({
      sub: input.subject ?? "api",
      roles: input.roles,
      iat: new Date().toISOString(),
      ...(input.expiresAt ? { exp: input.expiresAt } : {})
    }),
    "utf8"
  ).toString("base64url");
  return `kelp.v1.${payload}.${signTokenPayload(payload, signingSecret)}`;
}

export function principalHasRole(principal: ApiPrincipal, role: ApiRole): boolean {
  return principal.roles.includes("admin") || principal.roles.includes(role);
}

function authenticateBearer(
  authorization: string | string[] | undefined,
  options: {
    readonly adminToken?: string | null | undefined;
    readonly roleTokens: Readonly<Record<string, readonly ApiRole[]>>;
    readonly signingSecret?: string | null | undefined;
  }
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
      tokenKind: "server-role-token"
    };
  }
  if (!token.startsWith("kelp.v1.") || !options.signingSecret) {
    return null;
  }
  return parseSignedClaimToken(token, options.signingSecret);
}

function parseSignedClaimToken(token: string, signingSecret: string): ApiPrincipal | null {
  try {
    const [, version, payload, signature] = token.split(".");
    if (version !== "v1" || !payload || !signature) {
      return null;
    }
    if (!constantTimeEqual(signature, signTokenPayload(payload, signingSecret))) {
      return null;
    }
    const claim = JSON.parse(Buffer.from(payload, "base64url").toString("utf8"));
    if (!claim || typeof claim !== "object" || !Array.isArray(claim.roles)) {
      return null;
    }
    if (typeof claim.exp === "string" && Date.parse(claim.exp) <= Date.now()) {
      return null;
    }
    const roles = claim.roles.filter(
      (role: unknown): role is ApiRole => typeof role === "string" && roleSet.has(role)
    );
    if (roles.length === 0) {
      return null;
    }
    return {
      subject: typeof claim.sub === "string" ? claim.sub : "api",
      roles,
      tokenKind: "signed-role-token"
    };
  } catch {
    return null;
  }
}

function signTokenPayload(payload: string, signingSecret: string): string {
  return createHmac("sha256", signingSecret).update(payload, "utf8").digest("base64url");
}

function constantTimeEqual(left: string, right: string): boolean {
  const leftBuffer = Buffer.from(left, "utf8");
  const rightBuffer = Buffer.from(right, "utf8");
  return leftBuffer.length === rightBuffer.length && timingSafeEqual(leftBuffer, rightBuffer);
}

function parseRoleTokensEnv(
  value: string | undefined
): Readonly<Record<string, readonly ApiRole[]>> {
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
