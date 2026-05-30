import { randomUUID } from "node:crypto";
import { agentStepClassifications, agentStepSourceAgents, agentStepStatuses } from "@kelpclaw/workflow-spec";
const sourceAgents = new Set(agentStepSourceAgents);
const classifications = new Set(agentStepClassifications);
const statuses = new Set(agentStepStatuses);
export function registerAgentRunRoutes(app, options) {
    app.post("/api/agent-runs", { preHandler: options.auth.requireRole("operator") }, async (request, reply) => {
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
    });
    app.get("/api/agent-runs", { preHandler: options.auth.requireRole("auditor") }, async () => ({
        ok: true,
        runs: options.store.listRuns()
    }));
    app.get("/api/agent-runs/:id", { preHandler: options.auth.requireRole("auditor") }, async (request, reply) => {
        const run = options.store.getRun(request.params.id);
        if (!run) {
            return agentRunNotFound(reply, request.params.id);
        }
        return { ok: true, run };
    });
    app.post("/api/agent-runs/:id/events", { preHandler: options.auth.requireRole("operator") }, async (request, reply) => {
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
        const policyInput = {
            ...input,
            policyDecision: decision
        };
        if (decision.action === "deny") {
            const denied = options.store.appendEvent(request.params.id, {
                ...policyInput,
                status: "denied",
                result: policyInput.result ??
                    {
                        denied: true,
                        policyDecision: decision
                    }
            });
            options.store.appendAuditEvent(request.params.id, {
                action: "policy.denied",
                eventId: denied.id,
                summary: `Policy denied '${denied.toolName}'.`,
                metadata: { decision: decision }
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
    });
    app.get("/api/agent-runs/:id/events", { preHandler: options.auth.requireRole("auditor") }, async (request, reply) => {
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
    });
    app.post("/api/agent-runs/:id/stop", { preHandler: options.auth.requireRole("operator") }, async (request, reply) => {
        const status = request.body.status === "failed" ? "failed" : "stopped";
        const input = { status };
        try {
            return { ok: true, run: options.store.stopRun(request.params.id, input) };
        }
        catch {
            return agentRunNotFound(reply, request.params.id);
        }
    });
    app.post("/api/agent-runs/:id/events/:eventId/approve", { preHandler: options.auth.requireRole("reviewer") }, async (request, reply) => reviewPolicyApproval(options, request.params.id, request.params.eventId, "approved", request.body, reply));
    app.post("/api/agent-runs/:id/events/:eventId/deny", { preHandler: options.auth.requireRole("reviewer") }, async (request, reply) => reviewPolicyApproval(options, request.params.id, request.params.eventId, "denied", request.body, reply));
    app.get("/api/agent-runs/:id/audit/verify", { preHandler: options.auth.requireRole("auditor") }, async (request, reply) => {
        try {
            return { ok: true, verification: options.store.verifyAuditChain(request.params.id) };
        }
        catch {
            return agentRunNotFound(reply, request.params.id);
        }
    });
    app.get("/api/policies", { preHandler: options.auth.requireRole("auditor") }, async () => ({
        ok: true,
        ruleset: options.policyEngine.currentRuleset()
    }));
    app.put("/api/policies", { preHandler: options.auth.requireRole("admin") }, async (request, reply) => {
        try {
            const ruleset = typeof request.body.yaml === "string"
                ? options.policyEngine.replaceYaml(request.body.yaml)
                : options.policyEngine.replaceRuleset({ rules: parseRules(request.body.rules) });
            return { ok: true, ruleset };
        }
        catch (error) {
            return reply.code(422).send({
                ok: false,
                error: "POLICY_INVALID",
                message: error instanceof Error ? error.message : "Policy configuration is invalid."
            });
        }
    });
    app.post("/api/policies/check", { preHandler: options.auth.requireRole("operator") }, async (request, reply) => {
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
    });
}
function reviewPolicyApproval(options, runId, eventId, status, body, reply) {
    const run = options.store.getRun(runId);
    if (!run) {
        return agentRunNotFound(reply, runId);
    }
    const event = run.events.find((candidate) => candidate.id === eventId);
    if (!event) {
        return reply.code(404).send({
            ok: false,
            error: "AGENT_STEP_NOT_FOUND",
            message: `Agent-step event '${eventId}' was not found.`
        });
    }
    if (event.status !== "pending" || event.policyDecision?.action !== "require-approval") {
        return reply.code(409).send({
            ok: false,
            error: "POLICY_APPROVAL_NOT_REQUIRED",
            message: `Agent-step event '${eventId}' does not require reviewer approval.`
        });
    }
    const currentStatus = policyApprovalStatus(run, eventId);
    if (currentStatus) {
        return reply.code(409).send({
            ok: false,
            error: "POLICY_APPROVAL_ALREADY_REVIEWED",
            message: `Agent-step event '${eventId}' was already ${currentStatus}.`
        });
    }
    const reason = stringValue(body.reason);
    const metadata = {
        approvalStatus: status,
        reviewedBy: stringValue(body.reviewedBy) ?? "reviewer",
        decision: event.policyDecision
    };
    if (reason) {
        metadata.reason = reason;
    }
    const auditEvent = options.store.appendAuditEvent(runId, {
        action: status === "approved" ? "policy.approved" : "policy.denied",
        eventId,
        summary: `Reviewer ${status} '${event.toolName}' policy gate.`,
        metadata
    });
    return {
        ok: true,
        approval: {
            eventId,
            status
        },
        auditEvent,
        run: options.store.getRun(runId)
    };
}
function policyApprovalStatus(run, eventId) {
    for (const auditEvent of run.auditEvents) {
        if (auditEvent.eventId !== eventId) {
            continue;
        }
        if (auditEvent.action === "policy.approved") {
            return "approved";
        }
        if (auditEvent.metadata?.approvalStatus === "denied") {
            return "denied";
        }
    }
    return undefined;
}
function appendInputFromBody(runSourceAgent, runSessionId, body) {
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
        ...(body.result !== undefined ? { result: body.result } : {}),
        status,
        ...(classification ? { classification } : {}),
        startedAt,
        ...(finishedAt ? { finishedAt } : {})
    };
}
function parseRules(input) {
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
function isPolicyAction(value) {
    return (value === "allow" || value === "require-approval" || value === "deny" || value === "log-only");
}
function parseSourceAgent(input) {
    return typeof input === "string" && sourceAgents.has(input)
        ? input
        : undefined;
}
function parseClassification(input) {
    return typeof input === "string" && classifications.has(input)
        ? input
        : undefined;
}
function parseStatus(input) {
    return typeof input === "string" && statuses.has(input) ? input : undefined;
}
function stringValue(input) {
    return typeof input === "string" && input.trim().length > 0 ? input.trim() : undefined;
}
function isJsonRecord(input) {
    return typeof input === "object" && input !== null && !Array.isArray(input);
}
function agentRunNotFound(reply, id) {
    return reply.code(404).send({
        ok: false,
        error: "AGENT_RUN_NOT_FOUND",
        message: `Agent run '${id}' was not found.`
    });
}
//# sourceMappingURL=agent-run-routes.js.map