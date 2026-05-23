import { randomUUID } from "node:crypto";
import type { ServerResponse } from "node:http";
import type { FastifyInstance } from "fastify";
import {
  agentStepClassifications,
  agentStepSourceAgents,
  agentStepStatuses
} from "@kelpclaw/workflow-spec";
import type {
  AgentStepClassification,
  AgentStepSourceAgent,
  AgentStepStatus,
  JsonRecord,
  JsonValue
} from "@kelpclaw/workflow-spec";
import type {
  AgentRunStore,
  AppendAgentStepEventInput,
  StopAgentRunInput
} from "./agent-run-store.js";
import type { ApiAuthContext } from "./auth.js";
import type { ApiPolicyEngine } from "./policy-engine.js";

interface AgentRunRouteOptions {
  readonly store: AgentRunStore;
  readonly policyEngine: ApiPolicyEngine;
  readonly auth: ApiAuthContext;
  readonly writeSseEvent: (response: ServerResponse, event: string, data: unknown) => void;
}

interface AgentRunParams {
  readonly id: string;
}

interface StartAgentRunBody {
  readonly sourceAgent?: unknown;
  readonly sessionId?: unknown;
  readonly title?: unknown;
}

interface AppendAgentStepBody {
  readonly sourceAgent?: unknown;
  readonly sessionId?: unknown;
  readonly hookEvent?: unknown;
  readonly toolName?: unknown;
  readonly toolUseId?: unknown;
  readonly parentToolUseId?: unknown;
  readonly args?: unknown;
  readonly result?: unknown;
  readonly status?: unknown;
  readonly classification?: unknown;
  readonly startedAt?: unknown;
  readonly finishedAt?: unknown;
}

interface StopAgentRunBody {
  readonly status?: unknown;
}

interface PolicyRequestBody {
  readonly yaml?: unknown;
  readonly rules?: unknown;
}

const sourceAgents = new Set<string>(agentStepSourceAgents);
const classifications = new Set<string>(agentStepClassifications);
const statuses = new Set<string>(agentStepStatuses);

export function registerAgentRunRoutes(app: FastifyInstance, options: AgentRunRouteOptions): void {
  app.post<{ Body: StartAgentRunBody }>(
    "/api/agent-runs",
    { preHandler: options.auth.requireRole("operator") },
    async (request, reply) => {
    const sourceAgent = parseSourceAgent(request.body.sourceAgent);
    const sessionId = stringValue(request.body.sessionId);
    if (!sourceAgent || !sessionId) {
      return reply.code(422).send({
        ok: false,
        error: "AGENT_RUN_INVALID",
        message: "Agent run creation requires sourceAgent and sessionId."
      });
    }

    const run = options.store.startRun({
      sourceAgent,
      sessionId,
      ...(typeof request.body.title === "string" && request.body.title.trim()
        ? { title: request.body.title.trim() }
        : {})
    });
      return reply.code(201).send({ ok: true, run });
    }
  );

  app.get(
    "/api/agent-runs",
    { preHandler: options.auth.requireRole("auditor") },
    async () => ({
      ok: true,
      runs: options.store.listRuns()
    })
  );

  app.get<{ Params: AgentRunParams }>(
    "/api/agent-runs/:id",
    { preHandler: options.auth.requireRole("auditor") },
    async (request, reply) => {
      const run = options.store.getRun(request.params.id);
      if (!run) {
        return agentRunNotFound(reply, request.params.id);
      }
      return { ok: true, run };
    }
  );

  app.post<{ Params: AgentRunParams; Body: AppendAgentStepBody }>(
    "/api/agent-runs/:id/events",
    { preHandler: options.auth.requireRole("operator") },
    async (request, reply) => {
      const run = options.store.getRun(request.params.id);
      if (!run) {
        return agentRunNotFound(reply, request.params.id);
      }

      const input = appendInputFromBody(run.sourceAgent, run.sessionId, request.body);
      if (!input) {
        return reply.code(422).send({
          ok: false,
          error: "AGENT_STEP_INVALID",
          message: "Agent-step events require hookEvent, toolName, and object args."
        });
      }

      const decision = options.policyEngine.evaluateStep(input);
      const policyInput: AppendAgentStepEventInput = {
        ...input,
        policyDecision: decision
      };
      if (decision.action === "deny") {
        const denied = options.store.appendEvent(request.params.id, {
          ...policyInput,
          status: "denied",
          result:
            policyInput.result ??
            ({
              denied: true,
              policyDecision: decision as unknown as JsonRecord
            } satisfies JsonRecord)
        });
        options.store.appendAuditEvent(request.params.id, {
          action: "policy.denied",
          eventId: denied.id,
          summary: `Policy denied '${denied.toolName}'.`,
          metadata: { decision: decision as unknown as JsonValue }
        });
        return reply.code(403).send({
          ok: false,
          error: "POLICY_DENIED",
          message: decision.reason,
          decision,
          event: denied
        });
      }

      const event = options.store.appendEvent(request.params.id, {
        ...policyInput,
        status: decision.action === "require-approval" ? "pending" : policyInput.status
      });
      return reply.code(decision.action === "require-approval" ? 202 : 201).send({
        ok: true,
        event,
        decision
      });
    }
  );

  app.get<{ Params: AgentRunParams }>(
    "/api/agent-runs/:id/events",
    { preHandler: options.auth.requireRole("auditor") },
    async (request, reply) => {
      const run = options.store.getRun(request.params.id);
      if (!run) {
        return agentRunNotFound(reply, request.params.id);
      }

    reply.raw.writeHead(200, {
      "content-type": "text/event-stream; charset=utf-8",
      "cache-control": "no-cache, no-transform",
      connection: "keep-alive"
    });
    let sent = 0;
    const writeAvailableEvents = () => {
      const current = options.store.getRun(request.params.id);
      if (!current) {
        options.writeSseEvent(reply.raw, "error", {
          message: `Agent run '${request.params.id}' was not found.`
        });
        reply.raw.end();
        return true;
      }
      for (const event of current.events.slice(sent)) {
        options.writeSseEvent(reply.raw, "agent-step", event);
        sent += 1;
      }
      if (current.status !== "recording") {
        options.writeSseEvent(reply.raw, "agent-run-complete", current);
        reply.raw.end();
        return true;
      }
      return false;
    };

    if (writeAvailableEvents()) {
      return reply;
    }

    const interval = setInterval(() => {
      if (writeAvailableEvents()) {
        clearInterval(interval);
      }
    }, 250);
    request.raw.on("close", () => clearInterval(interval));
      return reply;
    }
  );

  app.post<{ Params: AgentRunParams; Body: StopAgentRunBody }>(
    "/api/agent-runs/:id/stop",
    { preHandler: options.auth.requireRole("operator") },
    async (request, reply) => {
      const status = request.body.status === "failed" ? "failed" : "stopped";
      const input: StopAgentRunInput = { status };
      try {
        return { ok: true, run: options.store.stopRun(request.params.id, input) };
      } catch {
        return agentRunNotFound(reply, request.params.id);
      }
    }
  );

  app.get<{ Params: AgentRunParams }>(
    "/api/agent-runs/:id/audit/verify",
    { preHandler: options.auth.requireRole("auditor") },
    async (request, reply) => {
      try {
        return { ok: true, verification: options.store.verifyAuditChain(request.params.id) };
      } catch {
        return agentRunNotFound(reply, request.params.id);
      }
    }
  );

  app.get(
    "/api/policies",
    { preHandler: options.auth.requireRole("auditor") },
    async () => ({
      ok: true,
      ruleset: options.policyEngine.currentRuleset()
    })
  );

  app.put<{ Body: PolicyRequestBody }>(
    "/api/policies",
    { preHandler: options.auth.requireRole("admin") },
    async (request, reply) => {
    try {
      const ruleset =
        typeof request.body.yaml === "string"
          ? options.policyEngine.replaceYaml(request.body.yaml)
          : options.policyEngine.replaceRuleset({ rules: parseRules(request.body.rules) });
      return { ok: true, ruleset };
    } catch (error) {
      return reply.code(422).send({
        ok: false,
        error: "POLICY_INVALID",
        message: error instanceof Error ? error.message : "Policy configuration is invalid."
      });
    }
    }
  );

  app.post<{ Body: AppendAgentStepBody }>(
    "/api/policies/check",
    { preHandler: options.auth.requireRole("operator") },
    async (request, reply) => {
      const input = appendInputFromBody("custom", `policy-check.${Date.now()}`, request.body);
      if (!input) {
        return reply.code(422).send({
          ok: false,
          error: "POLICY_CHECK_INVALID",
          message: "Policy checks require hookEvent, toolName, and object args."
        });
      }
      return {
        ok: true,
        decision: options.policyEngine.evaluateStep(input)
      };
    }
  );
}

function appendInputFromBody(
  runSourceAgent: AgentStepSourceAgent,
  runSessionId: string,
  body: AppendAgentStepBody
): AppendAgentStepEventInput | null {
  const sourceAgent = parseSourceAgent(body.sourceAgent) ?? runSourceAgent;
  const sessionId = stringValue(body.sessionId) ?? runSessionId;
  const hookEvent = stringValue(body.hookEvent);
  const toolName = stringValue(body.toolName);
  const args = isJsonRecord(body.args) ? body.args : null;
  if (!hookEvent || !toolName || !args) {
    return null;
  }
  const status = parseStatus(body.status) ?? "succeeded";
  const classification = parseClassification(body.classification);
  const startedAt = stringValue(body.startedAt) ?? new Date().toISOString();
  const finishedAt = stringValue(body.finishedAt);
  return {
    sourceAgent,
    sessionId,
    hookEvent,
    toolName,
    toolUseId: stringValue(body.toolUseId) ?? `tool-use.${randomUUID()}`,
    ...(stringValue(body.parentToolUseId)
      ? { parentToolUseId: stringValue(body.parentToolUseId) }
      : {}),
    args,
    ...(body.result !== undefined ? { result: body.result as JsonValue } : {}),
    status,
    ...(classification ? { classification } : {}),
    startedAt,
    ...(finishedAt ? { finishedAt } : {})
  };
}

function parseRules(input: unknown) {
  if (!Array.isArray(input)) {
    throw new Error("Policy update requires yaml or rules.");
  }
  return input.map((rule) => {
    if (!isJsonRecord(rule)) {
      throw new Error("Policy rules must be objects.");
    }
    const id = stringValue(rule.id);
    const when = stringValue(rule.when);
    const action = stringValue(rule.action);
    if (!id || !when || !isPolicyAction(action)) {
      throw new Error("Policy rules require id, when, and a supported action.");
    }
    return {
      id,
      when,
      action,
      ...(typeof rule.approverRole === "string" ? { approverRole: rule.approverRole } : {})
    };
  });
}

function isPolicyAction(
  value: string | undefined
): value is "allow" | "require-approval" | "deny" | "log-only" {
  return (
    value === "allow" || value === "require-approval" || value === "deny" || value === "log-only"
  );
}

function parseSourceAgent(input: unknown): AgentStepSourceAgent | undefined {
  return typeof input === "string" && sourceAgents.has(input)
    ? (input as AgentStepSourceAgent)
    : undefined;
}

function parseClassification(input: unknown): AgentStepClassification | undefined {
  return typeof input === "string" && classifications.has(input)
    ? (input as AgentStepClassification)
    : undefined;
}

function parseStatus(input: unknown): AgentStepStatus | undefined {
  return typeof input === "string" && statuses.has(input) ? (input as AgentStepStatus) : undefined;
}

function stringValue(input: unknown): string | undefined {
  return typeof input === "string" && input.trim().length > 0 ? input.trim() : undefined;
}

function isJsonRecord(input: unknown): input is JsonRecord {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}

function agentRunNotFound(
  reply: { code: (statusCode: number) => { send: (body: unknown) => unknown } },
  id: string
) {
  return reply.code(404).send({
    ok: false,
    error: "AGENT_RUN_NOT_FOUND",
    message: `Agent run '${id}' was not found.`
  });
}
