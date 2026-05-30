import { createHash, randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { stableJsonStringify } from "@kelpclaw/workflow-spec";
const genesisContentHash = `sha256:${"0".repeat(64)}`;
export class InMemoryAgentRunStore {
    runs = new Map();
    startRun(input) {
        const now = new Date().toISOString();
        const run = {
            id: `agent-run.${randomUUID()}`,
            sourceAgent: input.sourceAgent,
            sessionId: input.sessionId,
            ...(input.title ? { title: input.title } : {}),
            status: "recording",
            createdAt: now,
            updatedAt: now,
            events: [],
            auditEvents: []
        };
        this.runs.set(run.id, run);
        return run;
    }
    getRun(id) {
        return this.runs.get(id);
    }
    listRuns() {
        return [...this.runs.values()].sort((left, right) => left.createdAt.localeCompare(right.createdAt));
    }
    appendEvent(runId, input) {
        const run = this.requireRun(runId);
        if (run.status !== "recording") {
            throw new Error(`Agent run '${runId}' is not recording.`);
        }
        const event = createChainedAgentStepEvent(run, input);
        this.runs.set(runId, {
            ...run,
            updatedAt: event.recordedAt,
            events: [...run.events, event]
        });
        return event;
    }
    appendAuditEvent(runId, input) {
        const run = this.requireRun(runId);
        const event = createAgentRunAuditEvent(runId, input);
        this.runs.set(runId, {
            ...run,
            updatedAt: event.createdAt,
            auditEvents: [...run.auditEvents, event]
        });
        return event;
    }
    stopRun(runId, input) {
        const run = this.requireRun(runId);
        const now = new Date().toISOString();
        const updated = {
            ...run,
            status: input.status,
            updatedAt: now,
            stoppedAt: now
        };
        this.runs.set(runId, updated);
        return updated;
    }
    verifyAuditChain(runId) {
        return verifyAgentRunAuditChain(this.requireRun(runId));
    }
    requireRun(id) {
        const run = this.runs.get(id);
        if (!run) {
            throw new Error(`Agent run '${id}' was not found.`);
        }
        return run;
    }
}
export class SqliteAgentRunStore {
    databasePath;
    sqliteBin;
    constructor(options) {
        this.databasePath = options.databasePath;
        this.sqliteBin = options.sqliteBin ?? process.env.KELPCLAW_SQLITE_BIN ?? "sqlite3";
        mkdirSync(dirname(this.databasePath), { recursive: true });
        this.runSql(sqliteAgentRunMigrations.join("\n"));
    }
    startRun(input) {
        const run = new InMemoryAgentRunStore().startRun(input);
        this.runSql(`
      INSERT INTO agent_runs (id, source_agent, session_id, title, status, created_at, updated_at, stopped_at)
      VALUES (${sqlString(run.id)}, ${sqlString(run.sourceAgent)}, ${sqlString(run.sessionId)}, ${sqlNullable(run.title)}, ${sqlString(run.status)}, ${sqlString(run.createdAt)}, ${sqlString(run.updatedAt)}, NULL);
    `);
        return run;
    }
    getRun(id) {
        const [row] = this.querySql(`SELECT id, source_agent AS sourceAgent, session_id AS sessionId, title, status, created_at AS createdAt, updated_at AS updatedAt, stopped_at AS stoppedAt FROM agent_runs WHERE id = ${sqlString(id)};`);
        return row ? this.hydrateRun(row) : undefined;
    }
    listRuns() {
        return this.querySql("SELECT id, source_agent AS sourceAgent, session_id AS sessionId, title, status, created_at AS createdAt, updated_at AS updatedAt, stopped_at AS stoppedAt FROM agent_runs ORDER BY created_at;").map((row) => this.hydrateRun(row));
    }
    appendEvent(runId, input) {
        const run = this.requireRun(runId);
        if (run.status !== "recording") {
            throw new Error(`Agent run '${runId}' is not recording.`);
        }
        const event = createChainedAgentStepEvent(run, input);
        this.runSql(`
      INSERT INTO agent_run_events (run_id, chain_index, event_json)
      VALUES (${sqlString(runId)}, ${event.chainIndex}, ${sqlString(JSON.stringify(event))});
      UPDATE agent_runs SET updated_at = ${sqlString(event.recordedAt)} WHERE id = ${sqlString(runId)};
    `);
        return event;
    }
    appendAuditEvent(runId, input) {
        this.requireRun(runId);
        const event = createAgentRunAuditEvent(runId, input);
        this.runSql(`
      INSERT INTO agent_run_audit_events (run_id, id, created_at, action, event_json)
      VALUES (${sqlString(runId)}, ${sqlString(event.id)}, ${sqlString(event.createdAt)}, ${sqlString(event.action)}, ${sqlString(JSON.stringify(event))});
      UPDATE agent_runs SET updated_at = ${sqlString(event.createdAt)} WHERE id = ${sqlString(runId)};
    `);
        return event;
    }
    stopRun(runId, input) {
        this.requireRun(runId);
        const now = new Date().toISOString();
        this.runSql(`
      UPDATE agent_runs
      SET status = ${sqlString(input.status)}, updated_at = ${sqlString(now)}, stopped_at = ${sqlString(now)}
      WHERE id = ${sqlString(runId)};
    `);
        return this.requireRun(runId);
    }
    verifyAuditChain(runId) {
        return verifyAgentRunAuditChain(this.requireRun(runId));
    }
    hydrateRun(row) {
        const events = this.querySql(`SELECT event_json AS eventJson FROM agent_run_events WHERE run_id = ${sqlString(row.id)} ORDER BY chain_index;`).map((eventRow) => JSON.parse(eventRow.eventJson));
        const auditEvents = this.querySql(`SELECT event_json AS eventJson FROM agent_run_audit_events WHERE run_id = ${sqlString(row.id)} ORDER BY created_at, id;`).map((eventRow) => JSON.parse(eventRow.eventJson));
        return {
            id: row.id,
            sourceAgent: row.sourceAgent,
            sessionId: row.sessionId,
            ...(row.title ? { title: row.title } : {}),
            status: row.status,
            createdAt: row.createdAt,
            updatedAt: row.updatedAt,
            ...(row.stoppedAt ? { stoppedAt: row.stoppedAt } : {}),
            events,
            auditEvents
        };
    }
    requireRun(id) {
        const run = this.getRun(id);
        if (!run) {
            throw new Error(`Agent run '${id}' was not found.`);
        }
        return run;
    }
    runSql(sql) {
        execFileSync(this.sqliteBin, [this.databasePath], {
            input: sql,
            encoding: "utf8"
        });
    }
    querySql(sql) {
        const output = execFileSync(this.sqliteBin, ["-json", this.databasePath, sql], {
            encoding: "utf8"
        });
        if (output.trim().length === 0) {
            return [];
        }
        return JSON.parse(output);
    }
}
export function verifyAgentRunAuditChain(run) {
    for (let index = 0; index < run.events.length; index += 1) {
        const event = run.events[index];
        const previous = index === 0 ? undefined : run.events[index - 1];
        if (!event || event.chainIndex !== index) {
            return { valid: false, brokenAt: index };
        }
        const contentHash = hashAgentStepEventContent(event);
        if (event.contentHash !== contentHash) {
            return { valid: false, brokenAt: index };
        }
        const prevEventHash = hashAgentStepEventLink(previous, event.contentHash, index);
        if (event.prevEventHash !== prevEventHash) {
            return { valid: false, brokenAt: index };
        }
    }
    return { valid: true };
}
export function agentRunAuditChainHead(run) {
    return run.events.at(-1)?.prevEventHash ?? genesisContentHash;
}
export function createAgentRunAuditAnchor(run, method = "local-file") {
    const anchorBase = {
        kelpclawAuditAnchorVersion: "1.0.0",
        runId: run.id,
        method,
        chainHead: agentRunAuditChainHead(run),
        eventCount: run.events.length,
        anchoredAt: new Date().toISOString(),
        verification: verifyAgentRunAuditChain(run)
    };
    return {
        ...anchorBase,
        anchorId: hashJson(anchorBase)
    };
}
function createChainedAgentStepEvent(run, input) {
    const previous = run.events.at(-1);
    const chainIndex = run.events.length;
    const recordedAt = new Date().toISOString();
    const base = {
        id: `agent-step.${randomUUID()}`,
        runId: run.id,
        recordedAt,
        sourceAgent: input.sourceAgent,
        sessionId: input.sessionId,
        hookEvent: input.hookEvent,
        toolName: input.toolName,
        toolUseId: input.toolUseId,
        ...(input.parentToolUseId ? { parentToolUseId: input.parentToolUseId } : {}),
        args: input.args,
        ...(input.result !== undefined ? { result: input.result } : {}),
        status: input.status,
        ...(input.classification ? { classification: input.classification } : {}),
        startedAt: input.startedAt,
        ...(input.finishedAt ? { finishedAt: input.finishedAt } : {}),
        ...(input.policyDecision ? { policyDecision: input.policyDecision } : {})
    };
    const contentHash = hashJson(base);
    return {
        ...base,
        contentHash,
        prevEventHash: hashAgentStepEventLink(previous, contentHash, chainIndex),
        chainIndex
    };
}
function createAgentRunAuditEvent(runId, input) {
    return {
        id: `agent-run-audit.${randomUUID()}`,
        runId,
        action: input.action,
        createdAt: new Date().toISOString(),
        summary: input.summary,
        ...(input.eventId ? { eventId: input.eventId } : {}),
        ...(input.metadata ? { metadata: input.metadata } : {})
    };
}
function hashAgentStepEventContent(event) {
    const content = {
        id: event.id,
        runId: event.runId,
        recordedAt: event.recordedAt,
        sourceAgent: event.sourceAgent,
        sessionId: event.sessionId,
        hookEvent: event.hookEvent,
        toolName: event.toolName,
        toolUseId: event.toolUseId,
        ...(event.parentToolUseId ? { parentToolUseId: event.parentToolUseId } : {}),
        args: event.args,
        ...(event.result !== undefined ? { result: event.result } : {}),
        status: event.status,
        ...(event.classification ? { classification: event.classification } : {}),
        startedAt: event.startedAt,
        ...(event.finishedAt ? { finishedAt: event.finishedAt } : {}),
        ...(event.policyDecision ? { policyDecision: event.policyDecision } : {})
    };
    return hashJson(content);
}
function hashAgentStepEventLink(previous, contentHash, chainIndex) {
    return hashJson({
        chainIndex,
        previousContentHash: previous?.contentHash ?? genesisContentHash,
        contentHash
    });
}
function hashJson(value) {
    return `sha256:${createHash("sha256")
        .update(stableJsonStringify(value), "utf8")
        .digest("hex")}`;
}
function sqlString(value) {
    return `'${value.replace(/'/gu, "''")}'`;
}
function sqlNullable(value) {
    return value === undefined ? "NULL" : sqlString(value);
}
const sqliteAgentRunMigrations = [
    "PRAGMA journal_mode=WAL;",
    `CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    source_agent TEXT NOT NULL,
    session_id TEXT NOT NULL,
    title TEXT,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    stopped_at TEXT
  );`,
    `CREATE TABLE IF NOT EXISTS agent_run_events (
    run_id TEXT NOT NULL,
    chain_index INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    PRIMARY KEY (run_id, chain_index),
    FOREIGN KEY (run_id) REFERENCES agent_runs(id)
  );`,
    `CREATE TABLE IF NOT EXISTS agent_run_audit_events (
    run_id TEXT NOT NULL,
    id TEXT PRIMARY KEY,
    created_at TEXT NOT NULL,
    action TEXT NOT NULL,
    event_json TEXT NOT NULL,
    FOREIGN KEY (run_id) REFERENCES agent_runs(id)
  );`
];
//# sourceMappingURL=agent-run-store.js.map