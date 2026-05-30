import type { AgentStepClassification, AgentStepSourceAgent, AgentStepStatus, JsonRecord, JsonValue } from "@kelpclaw/workflow-spec";
import type { PolicyDecision } from "@kelpclaw/policy";
export type AgentRunStatus = "recording" | "stopped" | "failed";
export type AgentRunAuditAction = "policy.denied" | "policy.approved" | "trajectory.promoted" | "audit.anchored";
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
export type AppendAgentStepEventInput = Omit<AgentStepEvent, "id" | "runId" | "recordedAt" | "contentHash" | "prevEventHash" | "chainIndex">;
export interface StopAgentRunInput {
    readonly status: Extract<AgentRunStatus, "stopped" | "failed">;
}
export interface AgentRunStore {
    startRun(input: StartAgentRunInput): AgentRunRecord;
    getRun(id: string): AgentRunRecord | undefined;
    listRuns(): readonly AgentRunRecord[];
    appendEvent(runId: string, input: AppendAgentStepEventInput): AgentStepEvent;
    appendAuditEvent(runId: string, input: Omit<AgentRunAuditEvent, "id" | "runId" | "createdAt">): AgentRunAuditEvent;
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
export declare class InMemoryAgentRunStore implements AgentRunStore {
    private readonly runs;
    startRun(input: StartAgentRunInput): AgentRunRecord;
    getRun(id: string): AgentRunRecord | undefined;
    listRuns(): readonly AgentRunRecord[];
    appendEvent(runId: string, input: AppendAgentStepEventInput): AgentStepEvent;
    appendAuditEvent(runId: string, input: Omit<AgentRunAuditEvent, "id" | "runId" | "createdAt">): AgentRunAuditEvent;
    stopRun(runId: string, input: StopAgentRunInput): AgentRunRecord;
    verifyAuditChain(runId: string): AgentRunAuditVerification;
    private requireRun;
}
export interface SqliteAgentRunStoreOptions {
    readonly databasePath: string;
    readonly sqliteBin?: string | undefined;
}
export declare class SqliteAgentRunStore implements AgentRunStore {
    private readonly databasePath;
    private readonly sqliteBin;
    constructor(options: SqliteAgentRunStoreOptions);
    startRun(input: StartAgentRunInput): AgentRunRecord;
    getRun(id: string): AgentRunRecord | undefined;
    listRuns(): readonly AgentRunRecord[];
    appendEvent(runId: string, input: AppendAgentStepEventInput): AgentStepEvent;
    appendAuditEvent(runId: string, input: Omit<AgentRunAuditEvent, "id" | "runId" | "createdAt">): AgentRunAuditEvent;
    stopRun(runId: string, input: StopAgentRunInput): AgentRunRecord;
    verifyAuditChain(runId: string): AgentRunAuditVerification;
    private hydrateRun;
    private requireRun;
    private runSql;
    private querySql;
}
export declare function verifyAgentRunAuditChain(run: AgentRunRecord): AgentRunAuditVerification;
export declare function agentRunAuditChainHead(run: AgentRunRecord): string;
export declare function createAgentRunAuditAnchor(run: AgentRunRecord, method?: AgentRunAuditAnchor["method"]): AgentRunAuditAnchor;
//# sourceMappingURL=agent-run-store.d.ts.map