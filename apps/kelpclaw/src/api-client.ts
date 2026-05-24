import type {
  JsonRecord,
  JsonValue,
  WorkflowAcceptPlanRequest,
  WorkflowAcceptPlanResponse,
  WorkflowAgentMemoryRecord,
  WorkflowAgentTimelineEvent,
  WorkflowApproveRequest,
  WorkflowApproveResponse,
  WorkflowBudgetLedger,
  WorkflowBudgetPolicy,
  WorkflowBranchMergePreviewRequest,
  WorkflowBranchMergePreviewResponse,
  WorkflowBranchMergeRequest,
  WorkflowBranchMergeResponse,
  WorkflowBranchPlanRequest,
  WorkflowBranchPlanResponse,
  WorkflowBranchRepromptNodeRequest,
  WorkflowBranchRepromptNodeResponse,
  WorkflowCreateBranchRequest,
  WorkflowCreateBranchResponse,
  WorkflowConnectorListResponse,
  WorkflowConnectorResponse,
  WorkflowDraftEvaluation,
  WorkflowFeedbackRequest,
  WorkflowFeedbackResponse,
  WorkflowGetBranchResponse,
  WorkflowListBranchesResponse,
  WorkflowListRunsResponse,
  WorkflowListSchedulesResponse,
  WorkflowFetchRunResponse,
  WorkflowOpsHealth,
  WorkflowPlanRequest,
  WorkflowPlanSuccessResponse,
  WorkflowPlanResponse,
  WorkflowPlannerSuggestionDecisionRequest,
  WorkflowPlannerSuggestionDecisionResponse,
  WorkflowRepromptNodeRequest,
  WorkflowRepromptNodeResponse,
  WorkflowJob,
  WorkflowJobEvent,
  WorkflowNodeDecisionTrace,
  WorkflowNodeDecisionTraceExport,
  WorkflowWorkspace,
  WorkflowDeploymentKind,
  WorkflowDeploymentRecord,
  WorkflowDeploymentRollbackTarget,
  WorkflowProviderRuntimeConfig,
  WorkflowReuseCandidatesResponse,
  WorkflowRuntimeTruthSnapshot,
  WorkflowRouterEvalCase,
  WorkflowRouterEvalRun,
  WorkflowScheduleRecord,
  WorkflowStartRunRequest,
  WorkflowStartRunResponse,
  WorkflowAuditExportRecord,
  WorkflowUpdateBranchRequest,
  WorkflowUpdateBranchResponse,
  WorkflowValidateRequest,
  WorkflowValidateResponse
} from "@kelpclaw/workflow-spec";

export interface DeploymentActivationSummaryResponse {
  readonly ok: true;
  readonly activeDeployments: readonly WorkflowDeploymentRecord[];
  readonly activeSchedules: readonly JsonRecord[];
  readonly runnerConfigurations: readonly JsonRecord[];
  readonly skillPublications: readonly JsonRecord[];
  readonly integrationBindings: readonly JsonRecord[];
  readonly bundles: readonly JsonRecord[];
  readonly generatedServices: readonly JsonRecord[];
}

export interface RuntimeProviderStatusResponse {
  readonly ok: true;
  readonly providers: readonly WorkflowProviderRuntimeConfig[];
}

export interface RuntimeTruthResponse {
  readonly ok: true;
  readonly truth: WorkflowRuntimeTruthSnapshot;
}

export interface OpsHealthResponse {
  readonly ok: true;
  readonly health: WorkflowOpsHealth;
}

export interface RouterEvalListResponse {
  readonly ok: true;
  readonly classifierVersion: string;
  readonly cases: readonly WorkflowRouterEvalCase[];
  readonly latestRun?: WorkflowRouterEvalRun | undefined;
}

export interface RouterEvalRunResponse {
  readonly ok: true;
  readonly run: WorkflowRouterEvalRun;
}

export interface AgentMemoryListResponse {
  readonly ok: true;
  readonly memories: readonly WorkflowAgentMemoryRecord[];
}

export interface BudgetResponse {
  readonly ok: true;
  readonly policy: WorkflowBudgetPolicy;
  readonly ledgers: readonly WorkflowBudgetLedger[];
}

export interface AgentTimelineResponse {
  readonly ok: true;
  readonly events: readonly WorkflowAgentTimelineEvent[];
}

export interface DecisionTraceResponse {
  readonly ok: true;
  readonly traces: readonly WorkflowNodeDecisionTrace[];
}

export interface DecisionTraceExportResponse {
  readonly ok: true;
  readonly export: WorkflowNodeDecisionTraceExport;
  readonly jsonl: string;
}

export interface AuditExportResponse {
  readonly ok: true;
  readonly export: WorkflowAuditExportRecord;
  readonly jsonl: string;
}

export interface CodegenReviewRequest {
  readonly status: "approved" | "rejected";
  readonly reviewedBy: string;
  readonly notes?: string | undefined;
  readonly branchId?: string | undefined;
}

export interface CodegenReviewResponse {
  readonly ok: true;
  readonly workflow: WorkflowPlanSuccessResponse["workflow"];
  readonly draftRevision: WorkflowPlanSuccessResponse["draftRevision"];
  readonly validation: WorkflowValidateResponse["validation"];
  readonly node: unknown;
}

export interface CodegenPromotionResponse {
  readonly ok: true;
  readonly skill: {
    readonly id: string;
    readonly name: string;
  };
  readonly artifact: {
    readonly path: string;
    readonly checksum: string;
    readonly contentType: string;
  };
}

export interface CodegenBuildResponse {
  readonly ok: true;
  readonly workflow: WorkflowPlanSuccessResponse["workflow"];
  readonly draftRevision: WorkflowPlanSuccessResponse["draftRevision"];
  readonly validation: WorkflowValidateResponse["validation"];
  readonly job: WorkflowJob;
  readonly workspace: WorkflowWorkspace;
  readonly agentRuns: readonly unknown[];
  readonly artifacts: readonly unknown[];
  readonly testReport: unknown;
  readonly evalReport: unknown;
}

export interface CodegenEvalsResponse {
  readonly ok: true;
  readonly agentRuns: readonly unknown[];
  readonly agentArtifacts: readonly unknown[];
  readonly testReports: readonly unknown[];
  readonly evalReports: readonly unknown[];
}

export interface AgentStepEvent {
  readonly id: string;
  readonly runId: string;
  readonly recordedAt: string;
  readonly sourceAgent: string;
  readonly sessionId: string;
  readonly hookEvent: string;
  readonly toolName: string;
  readonly toolUseId: string;
  readonly args: JsonRecord;
  readonly result?: JsonValue | undefined;
  readonly status: string;
  readonly contentHash: string;
  readonly prevEventHash: string;
  readonly chainIndex: number;
  readonly classification?: string | undefined;
  readonly startedAt: string;
  readonly finishedAt?: string | undefined;
  readonly policyDecision?: JsonRecord | undefined;
}

export interface AgentRunAuditEvent {
  readonly id: string;
  readonly runId: string;
  readonly action: string;
  readonly createdAt: string;
  readonly summary: string;
  readonly eventId?: string | undefined;
  readonly metadata?: JsonRecord | undefined;
}

export interface AgentRunRecord {
  readonly id: string;
  readonly sourceAgent: string;
  readonly sessionId: string;
  readonly title?: string | undefined;
  readonly status: string;
  readonly createdAt: string;
  readonly updatedAt: string;
  readonly events: readonly AgentStepEvent[];
  readonly auditEvents: readonly AgentRunAuditEvent[];
}

export interface AgentRunListResponse {
  readonly ok: true;
  readonly runs: readonly AgentRunRecord[];
}

export interface AgentRunPolicyApprovalResponse {
  readonly ok: true;
  readonly approval: {
    readonly eventId: string;
    readonly status: "approved" | "denied";
  };
  readonly auditEvent: AgentRunAuditEvent;
  readonly run: AgentRunRecord;
}

export interface AgentRunAuditAnchor {
  readonly kelpclawAuditAnchorVersion: "1.0.0";
  readonly runId: string;
  readonly method: "local-file" | "external-http";
  readonly chainHead: string;
  readonly eventCount: number;
  readonly anchoredAt: string;
  readonly anchorId: string;
  readonly verification: {
    readonly valid: boolean;
    readonly brokenAt?: number | undefined;
  };
}

export interface AgentRunAuditAnchorResponse {
  readonly ok: true;
  readonly anchor: AgentRunAuditAnchor;
  readonly anchorPath: string;
  readonly externalAnchor: {
    readonly enabled: boolean;
    readonly status: "skipped" | "succeeded" | "failed";
    readonly endpoint?: string | undefined;
    readonly remoteStatus?: number | undefined;
    readonly message?: string | undefined;
  };
  readonly run: AgentRunRecord;
}

export interface PolicyRulesResponse {
  readonly ok: true;
  readonly ruleset: JsonRecord;
}

export interface SecretMetadata {
  readonly name: string;
  readonly createdAt: string;
  readonly updatedAt: string;
}

export interface IntegrationReadiness {
  readonly id: string;
  readonly ready: boolean;
  readonly requiredSecrets: readonly string[];
}

export interface SecretListResponse {
  readonly ok: true;
  readonly secrets: readonly SecretMetadata[];
  readonly integrations: readonly IntegrationReadiness[];
}

export interface GoogleIntegrationStatusResponse {
  readonly ok: true;
  readonly connected: boolean;
}

export interface GoogleConnectResponse {
  readonly ok: true;
  readonly url: string;
  readonly state: string;
}

export class KelpClawApiError extends Error {
  public readonly status: number;

  public constructor(status: number, message: string) {
    super(message);
    this.name = "KelpClawApiError";
    this.status = status;
  }
}

export const kelpClawApi = {
  fetchRuntimeProviders(): Promise<RuntimeProviderStatusResponse> {
    return getJson("/api/runtime/providers");
  },

  fetchRuntimeTruth(
    workflowId: string,
    branchId?: string | undefined
  ): Promise<RuntimeTruthResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/runtime-truth${queryString({ branchId })}`
    );
  },

  fetchOpsHealth(): Promise<OpsHealthResponse> {
    return getJson("/api/ops/health");
  },

  fetchRouterEvals(): Promise<RouterEvalListResponse> {
    return getJson("/api/router/evals");
  },

  runRouterEvals(): Promise<RouterEvalRunResponse> {
    return postJson("/api/router/evals/run", {});
  },

  fetchAgentMemory(workflowId: string): Promise<AgentMemoryListResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/memory`);
  },

  fetchBudget(workflowId: string, branchId?: string | undefined): Promise<BudgetResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/budget${queryString({ branchId })}`
    );
  },

  updateBudget(
    workflowId: string,
    request: Partial<WorkflowBudgetPolicy> & {
      readonly branchId?: string | undefined;
      readonly updatedBy?: string | undefined;
    }
  ): Promise<BudgetResponse> {
    return patchJson(`/api/workflows/${encodeURIComponent(workflowId)}/budget`, request);
  },

  fetchAgentTimeline(workflowId: string): Promise<AgentTimelineResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/agent-timeline`);
  },

  fetchAgentRuns(): Promise<AgentRunListResponse> {
    return getJson("/api/agent-runs");
  },

  fetchPolicies(): Promise<PolicyRulesResponse> {
    return getJson("/api/policies");
  },

  updatePolicyYaml(yaml: string): Promise<PolicyRulesResponse> {
    return putJson("/api/policies", { yaml });
  },

  approveAgentRunEvent(
    runId: string,
    eventId: string,
    body: { readonly reviewedBy?: string; readonly reason?: string } = {}
  ): Promise<AgentRunPolicyApprovalResponse> {
    return postJson(
      `/api/agent-runs/${encodeURIComponent(runId)}/events/${encodeURIComponent(eventId)}/approve`,
      body
    );
  },

  denyAgentRunEvent(
    runId: string,
    eventId: string,
    body: { readonly reviewedBy?: string; readonly reason?: string } = {}
  ): Promise<AgentRunPolicyApprovalResponse> {
    return postJson(
      `/api/agent-runs/${encodeURIComponent(runId)}/events/${encodeURIComponent(eventId)}/deny`,
      body
    );
  },

  anchorAgentRun(runId: string): Promise<AgentRunAuditAnchorResponse> {
    return postJson(`/api/agent-runs/${encodeURIComponent(runId)}/audit/anchor`, {});
  },

  plan(request: WorkflowPlanRequest, jobId?: string | undefined): Promise<WorkflowPlanResponse> {
    return postJson(
      "/api/workflows/plan",
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  validate(
    workflowId: string,
    request: WorkflowValidateRequest
  ): Promise<WorkflowValidateResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/validate`, request);
  },

  repromptNode(
    workflowId: string,
    request: WorkflowRepromptNodeRequest
  ): Promise<WorkflowRepromptNodeResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/reprompt-node`, request);
  },

  feedback(
    workflowId: string,
    request: WorkflowFeedbackRequest,
    jobId?: string | undefined
  ): Promise<WorkflowFeedbackResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/feedback`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  decideSuggestion(
    workflowId: string,
    feedbackId: string,
    suggestionId: string,
    request: WorkflowPlannerSuggestionDecisionRequest
  ): Promise<WorkflowPlannerSuggestionDecisionResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/feedback/${encodeURIComponent(feedbackId)}/suggestions/${encodeURIComponent(suggestionId)}/decision`,
      request
    );
  },

  evaluateDraft(
    workflowId: string,
    request: {
      readonly workflow: WorkflowPlanSuccessResponse["workflow"];
      readonly mockOnly: true;
      readonly branchId?: string | undefined;
    },
    jobId?: string | undefined
  ): Promise<{ readonly ok: true; readonly evaluation: WorkflowDraftEvaluation }> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/evaluate-draft`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  approve(workflowId: string, request: WorkflowApproveRequest): Promise<WorkflowApproveResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/approve`, request);
  },

  acceptPlan(
    workflowId: string,
    request: WorkflowAcceptPlanRequest
  ): Promise<WorkflowAcceptPlanResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/accept-plan`, request);
  },

  createBranch(
    workflowId: string,
    request: WorkflowCreateBranchRequest
  ): Promise<WorkflowCreateBranchResponse> {
    return postJson(`/api/workflows/${encodeURIComponent(workflowId)}/branches`, request);
  },

  listBranches(workflowId: string): Promise<WorkflowListBranchesResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/branches`);
  },

  fetchBranch(workflowId: string, branchId: string): Promise<WorkflowGetBranchResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}`
    );
  },

  updateBranch(
    workflowId: string,
    branchId: string,
    request: WorkflowUpdateBranchRequest
  ): Promise<WorkflowUpdateBranchResponse> {
    return patchJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}`,
      request
    );
  },

  planBranch(
    workflowId: string,
    branchId: string,
    request: WorkflowBranchPlanRequest,
    jobId?: string | undefined
  ): Promise<WorkflowBranchPlanResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}/plan`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  repromptBranchNode(
    workflowId: string,
    branchId: string,
    request: WorkflowBranchRepromptNodeRequest
  ): Promise<WorkflowBranchRepromptNodeResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}/reprompt-node`,
      request
    );
  },

  acceptBranchPlan(
    workflowId: string,
    branchId: string,
    request: WorkflowAcceptPlanRequest
  ): Promise<WorkflowAcceptPlanResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}/accept-plan`,
      request
    );
  },

  previewBranchMerge(
    workflowId: string,
    sourceBranchId: string,
    request: WorkflowBranchMergePreviewRequest
  ): Promise<WorkflowBranchMergePreviewResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(sourceBranchId)}/merge-preview`,
      request
    );
  },

  mergeBranch(
    workflowId: string,
    sourceBranchId: string,
    request: WorkflowBranchMergeRequest
  ): Promise<WorkflowBranchMergeResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(sourceBranchId)}/merge`,
      request
    );
  },

  fetchReuseCandidates(
    workflowId: string,
    branchId: string
  ): Promise<WorkflowReuseCandidatesResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/branches/${encodeURIComponent(branchId)}/reuse-candidates`
    );
  },

  reviewCodegen(
    workflowId: string,
    nodeId: string,
    request: CodegenReviewRequest
  ): Promise<CodegenReviewResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/codegen/${encodeURIComponent(nodeId)}/review`,
      request
    );
  },

  promoteCodegen(workflowId: string, nodeId: string): Promise<CodegenPromotionResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/codegen/${encodeURIComponent(nodeId)}/promote`,
      {}
    );
  },

  buildCodegen(
    workflowId: string,
    nodeId: string,
    request: {
      readonly maxIterations?: number;
      readonly maxReimplementationAttempts?: number;
      readonly maxWallClockSeconds?: number;
      readonly maxModelCostUsd?: number;
      readonly runTestsInDocker?: boolean;
      readonly branchId?: string | undefined;
    },
    jobId?: string | undefined
  ): Promise<CodegenBuildResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/codegen/${encodeURIComponent(nodeId)}/build`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  fetchCodegenEvals(workflowId: string, nodeId: string): Promise<CodegenEvalsResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/codegen/${encodeURIComponent(nodeId)}/evals`
    );
  },

  fetchDecisionTraces(workflowId: string): Promise<DecisionTraceResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/decision-traces`);
  },

  fetchNodeDecisionTraces(workflowId: string, nodeId: string): Promise<DecisionTraceResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/nodes/${encodeURIComponent(nodeId)}/decision-traces`
    );
  },

  exportDecisionTraces(workflowId: string): Promise<DecisionTraceExportResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/decision-traces/export`);
  },

  exportAudit(workflowId: string): Promise<AuditExportResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/audit/export`);
  },

  startRun(
    workflowId: string,
    request: WorkflowStartRunRequest,
    jobId?: string | undefined
  ): Promise<WorkflowStartRunResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/runs`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  fetchRun(workflowId: string, runId: string): Promise<WorkflowFetchRunResponse> {
    return getJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}`
    );
  },

  fetchRuns(workflowId: string): Promise<WorkflowListRunsResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/runs`);
  },

  replayRun(workflowId: string, runId: string): Promise<WorkflowStartRunResponse> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/runs/${encodeURIComponent(runId)}/replay`,
      {}
    );
  },

  fetchSchedules(workflowId: string): Promise<WorkflowListSchedulesResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/schedules`);
  },

  pauseSchedule(
    workflowId: string,
    scheduleId: string
  ): Promise<{ readonly ok: true; readonly schedule: WorkflowScheduleRecord }> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/schedules/${encodeURIComponent(scheduleId)}/pause`,
      {}
    );
  },

  resumeSchedule(
    workflowId: string,
    scheduleId: string
  ): Promise<{ readonly ok: true; readonly schedule: WorkflowScheduleRecord }> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/schedules/${encodeURIComponent(scheduleId)}/resume`,
      {}
    );
  },

  fetchConnectors(): Promise<WorkflowConnectorListResponse> {
    return getJson("/api/connectors");
  },

  importOpenApiConnector(request: {
    readonly name?: string | undefined;
    readonly sourceUrl?: string | undefined;
    readonly document?: string | JsonRecord | undefined;
    readonly secretRefs?: Readonly<Record<string, string>> | undefined;
  }): Promise<WorkflowConnectorResponse> {
    return postJson("/api/connectors/openapi/import", request);
  },

  registerMcpConnector(request: {
    readonly name?: string | undefined;
    readonly endpointUrl: string;
    readonly secretRefs?: Readonly<Record<string, string>> | undefined;
  }): Promise<WorkflowConnectorResponse> {
    return postJson("/api/connectors/mcp", request);
  },

  testConnector(connectorId: string): Promise<WorkflowConnectorResponse> {
    return postJson(`/api/connectors/${encodeURIComponent(connectorId)}/test`, {});
  },

  deleteConnector(connectorId: string): Promise<{ readonly ok: true; readonly deleted: boolean }> {
    return deleteJson(`/api/connectors/${encodeURIComponent(connectorId)}`);
  },

  listSecrets(): Promise<SecretListResponse> {
    return getJson("/api/secrets");
  },

  upsertSecret(
    name: string,
    value: string
  ): Promise<{ readonly ok: true; readonly secret: SecretMetadata }> {
    return putJson("/api/secrets", { name, value });
  },

  deleteSecret(name: string): Promise<{ readonly ok: true; readonly deleted: boolean }> {
    return deleteJson(`/api/secrets/${encodeURIComponent(name)}`);
  },

  googleStatus(): Promise<GoogleIntegrationStatusResponse> {
    return getJson("/api/integrations/google/status");
  },

  googleConnect(): Promise<GoogleConnectResponse> {
    return getJson("/api/integrations/google/connect");
  },

  googleRevoke(): Promise<{ readonly ok: true; readonly deleted: boolean }> {
    return postJson("/api/integrations/google/revoke", {});
  },

  createJob(request: {
    readonly type: WorkflowJob["type"];
    readonly workflowId?: string;
    readonly revisionId?: string;
    readonly nodeId?: string;
    readonly maxAttempts?: number;
  }): Promise<{ readonly ok: true; readonly job: WorkflowJob }> {
    return postJson("/api/jobs", request);
  },

  fetchJob(jobId: string): Promise<{ readonly ok: true; readonly job: WorkflowJob }> {
    return getJson(`/api/jobs/${encodeURIComponent(jobId)}`);
  },

  cancelJob(
    jobId: string,
    reason: string
  ): Promise<{ readonly ok: true; readonly job: WorkflowJob }> {
    return postJson(`/api/jobs/${encodeURIComponent(jobId)}/cancel`, { reason });
  },

  fetchWorkspace(
    workspaceId: string
  ): Promise<{ readonly ok: true; readonly workspace: WorkflowWorkspace }> {
    return getJson(`/api/workspaces/${encodeURIComponent(workspaceId)}`);
  },

  fetchDeployments(
    workflowId: string
  ): Promise<{ readonly ok: true; readonly deployments: readonly WorkflowDeploymentRecord[] }> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/deployments`);
  },

  fetchActiveDeployments(workflowId: string): Promise<DeploymentActivationSummaryResponse> {
    return getJson(`/api/workflows/${encodeURIComponent(workflowId)}/deployments/active`);
  },

  deployWorkflow(
    workflowId: string,
    request: {
      readonly approvedRevisionId: string;
      readonly kind: WorkflowDeploymentKind;
      readonly createdBy: string;
      readonly rollbackPlan: string;
      readonly branchId?: string | undefined;
      readonly metadata?: Record<string, unknown>;
    },
    jobId?: string | undefined
  ): Promise<{ readonly ok: true; readonly deployment: WorkflowDeploymentRecord }> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/deployments`,
      request,
      jobId ? { "x-kelpclaw-job-id": jobId } : undefined
    );
  },

  undeployDeployment(
    workflowId: string,
    deploymentId: string
  ): Promise<{
    readonly ok: true;
    readonly deployment: WorkflowDeploymentRecord;
    readonly active: DeploymentActivationSummaryResponse;
  }> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/deployments/${encodeURIComponent(deploymentId)}/undeploy`,
      {}
    );
  },

  rollbackDeployment(
    workflowId: string,
    deploymentId: string
  ): Promise<{
    readonly ok: true;
    readonly deployment: WorkflowDeploymentRecord;
    readonly rollbackTarget: WorkflowDeploymentRollbackTarget;
    readonly active: DeploymentActivationSummaryResponse;
  }> {
    return postJson(
      `/api/workflows/${encodeURIComponent(workflowId)}/deployments/${encodeURIComponent(deploymentId)}/rollback`,
      {}
    );
  },

  async streamJobEvents(
    jobId: string,
    onEvent: (event: WorkflowJobEvent | WorkflowJob) => void
  ): Promise<void> {
    const response = await fetch(`/api/jobs/${encodeURIComponent(jobId)}/events`, {
      headers: authHeader()
    });
    if (!response.ok || !response.body) {
      await parseJsonResponse(response);
      return;
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      const chunks = buffer.split("\n\n");
      buffer = chunks.pop() ?? "";
      for (const chunk of chunks) {
        const line = chunk.split("\n").find((candidate) => candidate.startsWith("data: "));
        if (line) {
          onEvent(JSON.parse(line.slice("data: ".length)) as WorkflowJobEvent | WorkflowJob);
        }
      }
    }
  },

  async streamAgentRunEvents(
    runId: string,
    onEvent: (event: AgentStepEvent | AgentRunRecord) => void
  ): Promise<void> {
    const response = await fetch(`/api/agent-runs/${encodeURIComponent(runId)}/events`, {
      headers: authHeader()
    });
    if (!response.ok || !response.body) {
      await parseJsonResponse(response);
      return;
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      buffer += decoder.decode(value, { stream: true });
      const chunks = buffer.split("\n\n");
      buffer = chunks.pop() ?? "";
      for (const chunk of chunks) {
        const line = chunk.split("\n").find((candidate) => candidate.startsWith("data: "));
        if (line) {
          onEvent(JSON.parse(line.slice("data: ".length)) as AgentStepEvent | AgentRunRecord);
        }
      }
    }
  }
};

function queryString(values: Readonly<Record<string, string | undefined>>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) {
    if (value) {
      params.set(key, value);
    }
  }
  const encoded = params.toString();
  return encoded ? `?${encoded}` : "";
}

async function postJson<TResponse>(
  url: string,
  body: unknown,
  extraHeaders: Record<string, string> | undefined = undefined
): Promise<TResponse> {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "content-type": "application/json",
      ...authHeader(),
      ...(extraHeaders ?? {})
    },
    body: JSON.stringify(body)
  });

  return parseJsonResponse<TResponse>(response);
}

async function putJson<TResponse>(url: string, body: unknown): Promise<TResponse> {
  const response = await fetch(url, {
    method: "PUT",
    headers: {
      "content-type": "application/json",
      ...authHeader()
    },
    body: JSON.stringify(body)
  });

  return parseJsonResponse<TResponse>(response);
}

async function patchJson<TResponse>(url: string, body: unknown): Promise<TResponse> {
  const response = await fetch(url, {
    method: "PATCH",
    headers: {
      "content-type": "application/json",
      ...authHeader()
    },
    body: JSON.stringify(body)
  });

  return parseJsonResponse<TResponse>(response);
}

async function deleteJson<TResponse>(url: string): Promise<TResponse> {
  const response = await fetch(url, {
    method: "DELETE",
    headers: authHeader()
  });

  return parseJsonResponse<TResponse>(response);
}

async function getJson<TResponse>(url: string): Promise<TResponse> {
  const response = await fetch(url, {
    headers: authHeader()
  });
  return parseJsonResponse<TResponse>(response);
}

async function parseJsonResponse<TResponse>(response: Response): Promise<TResponse> {
  const payload = (await response.json()) as { readonly message?: string; readonly error?: string };
  if (!response.ok) {
    throw new KelpClawApiError(
      response.status,
      payload.message ?? payload.error ?? `KelpClaw API request failed with ${response.status}.`
    );
  }

  return payload as TResponse;
}

export function readKelpClawAdminToken(): string {
  const stored = readLocalStorage("kelpclaw.adminToken");
  const env = (import.meta as ImportMeta & { readonly env?: Record<string, string | undefined> })
    .env;
  return stored || env?.VITE_KELPCLAW_ADMIN_TOKEN || "";
}

export function saveKelpClawAdminToken(token: string): void {
  writeLocalStorage("kelpclaw.adminToken", token.trim());
}

function authHeader(): Record<string, string> {
  const token = readKelpClawAdminToken();
  return token ? { authorization: `Bearer ${token}` } : {};
}

function readLocalStorage(key: string): string {
  try {
    return globalThis.localStorage?.getItem(key) ?? "";
  } catch {
    return "";
  }
}

function writeLocalStorage(key: string, value: string): void {
  try {
    if (value.length === 0) {
      globalThis.localStorage?.removeItem(key);
    } else {
      globalThis.localStorage?.setItem(key, value);
    }
  } catch {
    // The token remains in component state when storage is unavailable.
  }
}
