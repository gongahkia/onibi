import { createHash, randomUUID } from "node:crypto";
import { execFileSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { stableJsonStringify } from "@kelpclaw/workflow-spec";
import type {
  AgentStepClassification,
  AgentStepSourceAgent,
  AgentStepStatus,
  JsonRecord,
  JsonValue
} from "@kelpclaw/workflow-spec";
import type { PolicyDecision } from "@kelpclaw/policy";

export type AgentRunStatus = "recording" | "stopped" | "failed";
export type AgentRunAuditAction =
  | "policy.denied"
  | "policy.approved"
  | "trajectory.promoted"
  | "audit.anchored";

export interface AgentRunRecord {
  readonly id: string;
  readonly sourceAgent: AgentStepSourceAgent;
  readonly sessionId: string;
  readonly title?: string | undefined;
  readonly status: AgentRunStatus;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly stoppedAt?: string | undefined;
  readonly events: readonly AgentStepEvent[];
  readonly auditEvents: readonly AgentRunAuditEvent[];
}

export interface AgentStepEvent {
  readonly id: string;
  readonly runId: string;
  readonly recordedAt: string;
  readonly sourceAgent: AgentStepSourceAgent;
  readonly sessionId: string;
  readonly hookEvent: string;
  readonly toolName: string;
  readonly toolUseId: string;
  readonly parentToolUseId?: string | undefined;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
  readonly status: AgentStepStatus;
  readonly contentHash: string;
  readonly prevEventHash: string;
  readonly chainIndex: number;
  readonly classification?: AgentStepClassification | undefined;
  readonly startedAt: string;
  readonly finishedAt?: string | undefined;
  readonly policyDecision?: PolicyDecision | undefined;
}

export interface AgentRunAuditEvent {
  readonly id: string;
  readonly runId: string;
  readonly action: AgentRunAuditAction;
  readonly createdAt: string;
  readonly summary: string;
  readonly eventId?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface StartAgentRunInput {
  readonly sourceAgent: AgentStepSourceAgent;
  readonly sessionId: string;
  readonly title?: string | undefined;
}

export type AppendAgentStepEventInput = Omit<
  AgentStepEvent,
  "id" | "runId" | "recordedAt" | "contentHash" | "prevEventHash" | "chainIndex"
>;

export interface StopAgentRunInput {
  readonly status: Extract<AgentRunStatus, "stopped" | "failed">;
}

export interface AgentRunStore {
  startRun(input: StartAgentRunInput): AgentRunRecord;
  getRun(id: string): AgentRunRecord | undefined;
  listRuns(): readonly AgentRunRecord[];
  appendEvent(runId: string, input: AppendAgentStepEventInput): AgentStepEvent;
  appendAuditEvent(
    runId: string,
    input: Omit<AgentRunAuditEvent, "id" | "runId" | "createdAt">
  ): AgentRunAuditEvent;
  stopRun(runId: string, input: StopAgentRunInput): AgentRunRecord;
  verifyAuditChain(runId: string): AgentRunAuditVerification;
}

export interface AgentRunAuditVerification {
  readonly valid: boolean;
  readonly brokenAt?: number | undefined;
}

export interface AgentRunAuditAnchor {
  readonly kelpclawAuditAnchorVersion: "1.0.0";
  readonly runId: string;
  readonly method: "local-file" | "external-http";
  readonly chainHead: string;
  readonly eventCount: number;
  readonly anchoredAt: string;
  readonly anchorId: string;
  readonly verification: AgentRunAuditVerification;
}

const genesisContentHash = `sha256:${"0".repeat(64)}`;

export class InMemoryAgentRunStore implements AgentRunStore {
  private readonly runs = new Map<string, AgentRunRecord>();

  public startRun(input: StartAgentRunInput): AgentRunRecord {
    const now = new Date().toISOString();
    const run: AgentRunRecord = {
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

  public getRun(id: string): AgentRunRecord | undefined {
    return this.runs.get(id);
  }

  public listRuns(): readonly AgentRunRecord[] {
    return [...this.runs.values()].sort((left, right) =>
      left.createdAt.localeCompare(right.createdAt)
    );
  }

  public appendEvent(runId: string, input: AppendAgentStepEventInput): AgentStepEvent {
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

  public appendAuditEvent(
    runId: string,
    input: Omit<AgentRunAuditEvent, "id" | "runId" | "createdAt">
  ): AgentRunAuditEvent {
    const run = this.requireRun(runId);
    const event = createAgentRunAuditEvent(runId, input);
    this.runs.set(runId, {
      ...run,
      updatedAt: event.createdAt,
      auditEvents: [...run.auditEvents, event]
    });
    return event;
  }

  public stopRun(runId: string, input: StopAgentRunInput): AgentRunRecord {
    const run = this.requireRun(runId);
    const now = new Date().toISOString();
    const updated: AgentRunRecord = {
      ...run,
      status: input.status,
      updatedAt: now,
      stoppedAt: now
    };
    this.runs.set(runId, updated);
    return updated;
  }

  public verifyAuditChain(runId: string): AgentRunAuditVerification {
    return verifyAgentRunAuditChain(this.requireRun(runId));
  }

  private requireRun(id: string): AgentRunRecord {
    const run = this.runs.get(id);
    if (!run) {
      throw new Error(`Agent run '${id}' was not found.`);
    }
    return run;
  }
}

export interface SqliteAgentRunStoreOptions {
  readonly databasePath: string;
  readonly sqliteBin?: string | undefined;
}

export class SqliteAgentRunStore implements AgentRunStore {
  private readonly databasePath: string;
  private readonly sqliteBin: string;

  public constructor(options: SqliteAgentRunStoreOptions) {
    this.databasePath = options.databasePath;
    this.sqliteBin = options.sqliteBin ?? process.env.KELPCLAW_SQLITE_BIN ?? "sqlite3";
    mkdirSync(dirname(this.databasePath), { recursive: true });
    this.runSql(sqliteAgentRunMigrations.join("\n"));
  }

  public startRun(input: StartAgentRunInput): AgentRunRecord {
    const run = new InMemoryAgentRunStore().startRun(input);
    this.runSql(`
      INSERT INTO agent_runs (id, source_agent, session_id, title, status, created_at, updated_at, stopped_at)
      VALUES (${sqlString(run.id)}, ${sqlString(run.sourceAgent)}, ${sqlString(run.sessionId)}, ${sqlNullable(run.title)}, ${sqlString(run.status)}, ${sqlString(run.createdAt)}, ${sqlString(run.updatedAt)}, NULL);
    `);
    return run;
  }

  public getRun(id: string): AgentRunRecord | undefined {
    const [row] = this.querySql<AgentRunRow>(
      `SELECT id, source_agent AS sourceAgent, session_id AS sessionId, title, status, created_at AS createdAt, updated_at AS updatedAt, stopped_at AS stoppedAt FROM agent_runs WHERE id = ${sqlString(id)};`
    );
    return row ? this.hydrateRun(row) : undefined;
  }

  public listRuns(): readonly AgentRunRecord[] {
    return this.querySql<AgentRunRow>(
      "SELECT id, source_agent AS sourceAgent, session_id AS sessionId, title, status, created_at AS createdAt, updated_at AS updatedAt, stopped_at AS stoppedAt FROM agent_runs ORDER BY created_at;"
    ).map((row) => this.hydrateRun(row));
  }

  public appendEvent(runId: string, input: AppendAgentStepEventInput): AgentStepEvent {
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

  public appendAuditEvent(
    runId: string,
    input: Omit<AgentRunAuditEvent, "id" | "runId" | "createdAt">
  ): AgentRunAuditEvent {
    this.requireRun(runId);
    const event = createAgentRunAuditEvent(runId, input);
    this.runSql(`
      INSERT INTO agent_run_audit_events (run_id, id, created_at, action, event_json)
      VALUES (${sqlString(runId)}, ${sqlString(event.id)}, ${sqlString(event.createdAt)}, ${sqlString(event.action)}, ${sqlString(JSON.stringify(event))});
      UPDATE agent_runs SET updated_at = ${sqlString(event.createdAt)} WHERE id = ${sqlString(runId)};
    `);
    return event;
  }

  public stopRun(runId: string, input: StopAgentRunInput): AgentRunRecord {
    this.requireRun(runId);
    const now = new Date().toISOString();
    this.runSql(`
      UPDATE agent_runs
      SET status = ${sqlString(input.status)}, updated_at = ${sqlString(now)}, stopped_at = ${sqlString(now)}
      WHERE id = ${sqlString(runId)};
    `);
    return this.requireRun(runId);
  }

  public verifyAuditChain(runId: string): AgentRunAuditVerification {
    return verifyAgentRunAuditChain(this.requireRun(runId));
  }

  private hydrateRun(row: AgentRunRow): AgentRunRecord {
    const events = this.querySql<AgentRunEventRow>(
      `SELECT event_json AS eventJson FROM agent_run_events WHERE run_id = ${sqlString(row.id)} ORDER BY chain_index;`
    ).map((eventRow) => JSON.parse(eventRow.eventJson) as AgentStepEvent);
    const auditEvents = this.querySql<AgentRunAuditEventRow>(
      `SELECT event_json AS eventJson FROM agent_run_audit_events WHERE run_id = ${sqlString(row.id)} ORDER BY created_at, id;`
    ).map((eventRow) => JSON.parse(eventRow.eventJson) as AgentRunAuditEvent);
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

  private requireRun(id: string): AgentRunRecord {
    const run = this.getRun(id);
    if (!run) {
      throw new Error(`Agent run '${id}' was not found.`);
    }
    return run;
  }

  private runSql(sql: string): void {
    execFileSync(this.sqliteBin, [this.databasePath], {
      input: sql,
      encoding: "utf8"
    });
  }

  private querySql<T>(sql: string): T[] {
    const output = execFileSync(this.sqliteBin, ["-json", this.databasePath, sql], {
      encoding: "utf8"
    });
    if (output.trim().length === 0) {
      return [];
    }
    return JSON.parse(output) as T[];
  }
}

export function verifyAgentRunAuditChain(run: AgentRunRecord): AgentRunAuditVerification {
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

export function agentRunAuditChainHead(run: AgentRunRecord): string {
  return run.events.at(-1)?.prevEventHash ?? genesisContentHash;
}

export function createAgentRunAuditAnchor(
  run: AgentRunRecord,
  method: AgentRunAuditAnchor["method"] = "local-file"
): AgentRunAuditAnchor {
  const anchorBase = {
    kelpclawAuditAnchorVersion: "1.0.0" as const,
    runId: run.id,
    method,
    chainHead: agentRunAuditChainHead(run),
    eventCount: run.events.length,
    anchoredAt: new Date().toISOString(),
    verification: verifyAgentRunAuditChain(run)
  };
  return {
    ...anchorBase,
    anchorId: hashJson(anchorBase as unknown as JsonValue)
  };
}

function createChainedAgentStepEvent(
  run: AgentRunRecord,
  input: AppendAgentStepEventInput
): AgentStepEvent {
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

function createAgentRunAuditEvent(
  runId: string,
  input: Omit<AgentRunAuditEvent, "id" | "runId" | "createdAt">
): AgentRunAuditEvent {
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

function hashAgentStepEventContent(event: AgentStepEvent): string {
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

function hashAgentStepEventLink(
  previous: AgentStepEvent | undefined,
  contentHash: string,
  chainIndex: number
): string {
  return hashJson({
    chainIndex,
    previousContentHash: previous?.contentHash ?? genesisContentHash,
    contentHash
  });
}

function hashJson(value: unknown): string {
  return `sha256:${createHash("sha256")
    .update(stableJsonStringify(value as JsonValue), "utf8")
    .digest("hex")}`;
}

function sqlString(value: string): string {
  return `'${value.replace(/'/gu, "''")}'`;
}

function sqlNullable(value: string | undefined): string {
  return value === undefined ? "NULL" : sqlString(value);
}

interface AgentRunRow {
  readonly id: string;
  readonly sourceAgent: AgentStepSourceAgent;
  readonly sessionId: string;
  readonly title?: string | undefined;
  readonly status: AgentRunStatus;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly stoppedAt?: string | undefined;
}

interface AgentRunEventRow {
  readonly eventJson: string;
}

interface AgentRunAuditEventRow {
  readonly eventJson: string;
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
] as const;
